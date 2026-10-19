package server

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// activity lists each pane's activity without its seq, which tests of seq
// itself check separately.
func activity(t *testing.T, s *session) map[int]protocol.PaneActivity {
	t.Helper()
	result := make(map[int]protocol.PaneActivity)
	for _, a := range s.status().Activity {
		a.Seq = 0
		result[a.Pane] = a
	}
	return result
}

func TestAttentionRequestsOnlyMarkUnviewedPanes(t *testing.T) {
	s := editorNavigationSession(t) // pane 1 left, pane 2 right and focused
	left, right := s.panes[1], s.panes[2]
	s.setViewing(true)

	for _, tc := range []struct {
		name, output string
		want         bool
	}{
		{"bell", "\a", true},
		{"OSC 9 notification", "\x1b]9;Claude needs your permission\a", true},
		{"OSC 777 notification", "\x1b]777;notify;Codex;turn complete\x1b\\", true},
		{"OSC 9;4 progress", "\x1b]9;4;1;50\a", false},
		{"OSC 777 other", "\x1b]777;preexec\a", false},
		{"OSC terminated by BEL", "\x1b]2;title\a", false},
	} {
		left.mu.Lock()
		left.attention = false
		left.mu.Unlock()
		left.apply([]byte(tc.output))
		if got := activity(t, s)[1].Attention; got != tc.want {
			t.Fatalf("%s: attention = %v, want %v", tc.name, got, tc.want)
		}
	}

	right.apply([]byte("\a"))
	if _, ok := activity(t, s)[2]; ok {
		t.Fatal("bell in the pane being viewed asked for attention")
	}

	// Losing terminal focus means nobody is looking, even at the focused pane.
	s.setViewing(false)
	right.apply([]byte("\a"))
	if !activity(t, s)[2].Attention {
		t.Fatal("bell while the client was unfocused was lost")
	}
	s.setViewing(true)
	if _, ok := activity(t, s)[2]; ok {
		t.Fatal("viewing the focused pane did not clear its attention")
	}

	// Moving focus to a pane marks it seen.
	left.apply([]byte("\a"))
	if err := s.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ok := activity(t, s)[1]; ok {
		t.Fatal("focusing a pane did not clear its attention")
	}
}

func TestAgentReportsDriveStateAndAttention(t *testing.T) {
	s := editorNavigationSession(t)
	s.setViewing(true) // pane 2 is viewed; pane 1 is not

	report := func(pane int, state, label string) {
		t.Helper()
		if err := s.reportAgent(pane, state, label, nil); err != nil {
			t.Fatal(err)
		}
	}
	report(1, protocol.AgentWorking, "claude")
	if got := activity(t, s)[1]; got != (protocol.PaneActivity{Pane: 1, Agent: "claude", State: protocol.AgentWorking}) {
		t.Fatalf("working: %+v", got)
	}
	report(1, protocol.AgentIdle, "")
	if got := activity(t, s)[1]; got != (protocol.PaneActivity{Pane: 1, Agent: "claude", State: protocol.AgentIdle, Attention: true}) {
		t.Fatalf("finished turn: %+v", got)
	}
	report(1, protocol.AgentWorking, "")
	report(1, protocol.AgentBlocked, "")
	if got := activity(t, s)[1]; got.State != protocol.AgentBlocked || !got.Attention {
		t.Fatalf("blocked: %+v", got)
	}
	report(1, protocol.AgentWorking, "")
	if activity(t, s)[1].Attention {
		t.Fatal("new work kept a stale attention flag")
	}
	report(1, protocol.AgentIdle, "")
	report(1, protocol.AgentIdle, "")
	if !activity(t, s)[1].Attention {
		t.Fatal("repeated idle cleared attention")
	}

	// The viewed pane reports state but never needs attention.
	report(2, protocol.AgentBlocked, "codex")
	if got := activity(t, s)[2]; got.State != protocol.AgentBlocked || got.Attention || got.Agent != "codex" {
		t.Fatalf("viewed blocked pane: %+v", got)
	}

	report(1, "clear", "")
	if _, ok := activity(t, s)[1]; ok {
		t.Fatal("clear left agent activity behind")
	}
	// An idle report without a preceding turn is not a completion.
	report(1, protocol.AgentIdle, "pi")
	if activity(t, s)[1].Attention {
		t.Fatal("idle without a finished turn asked for attention")
	}
	for _, bad := range []struct{ state, label string }{{"done", ""}, {"", ""}, {protocol.AgentIdle, "bad\x1bname"}, {protocol.AgentIdle, string(make([]byte, 41))}} {
		if err := s.reportAgent(1, bad.state, bad.label, nil); err == nil {
			t.Fatalf("accepted state %q label %q", bad.state, bad.label)
		}
	}
	for _, bad := range [][]string{{"/usr/bin/claude"}, {""}, {"claude", "a\nb"}, make([]string, 65)} {
		if err := s.reportAgent(1, protocol.AgentIdle, "", bad); err == nil {
			t.Fatalf("accepted resume command %q", bad)
		}
	}
	if err := s.reportAgent(99, protocol.AgentIdle, "", nil); err == nil {
		t.Fatal("report for a missing pane was accepted")
	}
}

func TestAgentSeqChangesExactlyWhenStateDoes(t *testing.T) {
	s := editorNavigationSession(t)
	seq := func(pane int) uint64 {
		t.Helper()
		for _, a := range s.status().Activity {
			if a.Pane == pane {
				return a.Seq
			}
		}
		return 0
	}
	report := func(pane int, state string) uint64 {
		t.Helper()
		if err := s.reportAgent(pane, state, "omp", nil); err != nil {
			t.Fatal(err)
		}
		return seq(pane)
	}
	working := report(1, protocol.AgentWorking)
	if working == 0 {
		t.Fatal("an agent pane has no seq")
	}
	if again := report(1, protocol.AgentWorking); again != working {
		t.Fatalf("repeating a state changed seq %d -> %d", working, again)
	}
	blocked := report(1, protocol.AgentBlocked)
	if blocked <= working {
		t.Fatalf("blocking kept or lowered seq: %d -> %d", working, blocked)
	}
	// Seqs are never shared between panes, so a stale one can't match elsewhere.
	if other := report(2, protocol.AgentBlocked); other == blocked || other == 0 {
		t.Fatalf("pane 2 seq %d, pane 1 seq %d", other, blocked)
	}
	if err := s.reportAgent(1, "clear", "", nil); err != nil {
		t.Fatal(err)
	}
	if relabeled := report(1, protocol.AgentBlocked); relabeled == blocked {
		t.Fatal("an agent coming back after clear reused its old seq")
	}
}

func TestAgentReportRequestTargetsSessionIDWithoutTouchingRecency(t *testing.T) {
	srv, s := viewportSession(t)
	before := srv.request(protocol.Request{Op: "list"}).Sessions[0]
	if r := srv.request(protocol.Request{Op: "agent-report", Name: "view", SessionID: s.id, PaneID: 1, Kind: protocol.AgentBlocked, Text: "claude"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	after := srv.request(protocol.Request{Op: "list"}).Sessions[0]
	want := []protocol.PaneActivity{{Pane: 1, Agent: "claude", State: protocol.AgentBlocked, Attention: true}}
	if len(after.Activity) == 1 {
		after.Activity[0].Seq = 0
	}
	if !reflect.DeepEqual(after.Activity, want) || !after.Used.Equal(before.Used) {
		t.Fatalf("list after report: %+v", after)
	}
	// A stale ID must not fall back to a session that now has the old name.
	if r := srv.request(protocol.Request{Op: "agent-report", Name: "view", SessionID: s.id + 1000, PaneID: 1, Kind: protocol.AgentIdle}); r.Error == "" {
		t.Fatal("report with an unknown session ID was accepted")
	}
}

func TestDirectionalMoveWithoutNeighborIsPublishedAsEdge(t *testing.T) {
	s := editorNavigationSession(t) // pane 2 focused on the right
	edge := func() (uint64, string) {
		f, _ := s.snapshot(map[int]int{}, map[int]int{}, 0, true)
		return f.Edge, f.EdgeDirection
	}
	if n, _ := edge(); n != 0 {
		t.Fatalf("new session edge = %d", n)
	}
	// select-pane shares navigate-pane's geometry without foreground-process
	// inspection, which forwards the key while a starting shell is unreadable.
	if err := s.input(protocol.Request{Op: "select-pane", Direction: "left"}); err != nil {
		t.Fatal(err)
	}
	if focusedPaneID(s) != 1 {
		t.Fatal("navigation did not move left")
	}
	if n, _ := edge(); n != 0 {
		t.Fatal("a move that found a neighbor was reported as an edge")
	}
	if err := s.input(protocol.Request{Op: "select-pane", Direction: "left"}); err != nil {
		t.Fatal(err)
	}
	if n, direction := edge(); n != 1 || direction != "left" || focusedPaneID(s) != 1 {
		t.Fatalf("left edge: %d %q focus=%d", n, direction, focusedPaneID(s))
	}
	// Editor hand-off at the edge (bmux focus) reports through the same count.
	if err := s.input(protocol.Request{Op: "focus-pane", PaneID: 1, Direction: "up"}); err != nil {
		t.Fatal(err)
	}
	if n, direction := edge(); n != 2 || direction != "up" {
		t.Fatalf("focus-pane edge: %d %q", n, direction)
	}
}

func TestAttachedClientFocusControlsViewing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- srv.Serve(l) }()
	defer func() {
		srv.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	if r := srv.request(protocol.Request{Op: "create", Name: "agents", Dir: t.TempDir()}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s := srv.sessions["agents"]
	viewers := func(want int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			s.mu.Lock()
			got := s.viewers
			s.mu.Unlock()
			if got == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("viewers = %d, want %d", got, want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	var r protocol.Response
	if err := enc.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "agents", Width: 50, Height: 8}); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&r); err != nil || r.Error != "" {
		t.Fatalf("attach: %+v %v", r, err)
	}
	go func() { _, _ = io.Copy(io.Discard, c) }()
	viewers(0) // attached but not focused yet
	for _, focused := range []bool{true, true, false, true} {
		if err := enc.Encode(protocol.Request{Op: "focus", Focused: focused}); err != nil {
			t.Fatal(err)
		}
	}
	viewers(1)
	s.panes[1].apply([]byte("\a"))
	if _, ok := activity(t, s)[1]; ok {
		t.Fatal("bell in a pane on screen asked for attention")
	}
	c.Close()
	viewers(0)
	s.panes[1].apply([]byte("\a"))
	if !activity(t, s)[1].Attention {
		t.Fatal("bell after the client left was not kept for later")
	}
}

func TestAgentMatchRecognizesInstalledLayouts(t *testing.T) {
	for command, want := range map[string]string{
		"opencode": "opencode",
		"/opt/homebrew/Caskroom/codex/0.162.0/bin/codex": "codex",
		"/Users/me/.local/share/claude/versions/2.1.296": "claude",
		"/opt/homebrew/bin/pi":                           "pi",
		"2.1.296":                                        "",
		"/usr/local/bin/node":                            "",
		"/bin/bash":                                      "",
		"/opt/tool/versions/1.0":                         "",
	} {
		if got := agentMatch(command); got != want {
			t.Errorf("agentMatch(%q) = %q, want %q", command, got, want)
		}
	}
	if !isScriptInterpreter("/opt/homebrew/bin/node") || !isScriptInterpreter("python3.12") || isScriptInterpreter("/bin/sh") {
		t.Error("script interpreter classification")
	}
}

func TestAgentOutputTimingSeparatesWorkingFromWaiting(t *testing.T) {
	p := &pane{}
	sample := func(output bool) {
		if output {
			p.noteOutputLocked(time.Now())
		}
		p.observeAgentLocked("opencode")
	}
	state := func() protocol.PaneActivity {
		a, _ := p.activityLocked(1)
		return a
	}
	var last protocol.PaneActivity
	expect := func(step string, want protocol.PaneActivity) {
		t.Helper()
		want.Pane = 1
		if want.Agent == "" {
			want.Agent = "opencode"
		}
		got := state()
		if changed := got.State != last.State || got.Agent != last.Agent; changed == (got.Seq == last.Seq) {
			t.Fatalf("%s: seq %d after %d, though the state changed: %v", step, got.Seq, last.Seq, changed)
		}
		last = got
		got.Seq = 0
		if got != want {
			t.Fatalf("%s: %+v, want %+v", step, got, want)
		}
	}
	sample(false)
	expect("detected at rest", protocol.PaneActivity{State: protocol.AgentIdle})
	sample(true)
	sample(false)
	expect("one stray redraw", protocol.PaneActivity{State: protocol.AgentIdle})
	sample(true)
	sample(true)
	expect("sustained output", protocol.PaneActivity{State: protocol.AgentWorking})
	sample(false)
	expect("one quiet sample", protocol.PaneActivity{State: protocol.AgentWorking})
	sample(false)
	expect("went quiet unseen", protocol.PaneActivity{State: protocol.AgentIdle, Attention: true})
	sample(true)
	sample(true)
	expect("working again", protocol.PaneActivity{State: protocol.AgentWorking})

	p.viewed.Store(true)
	sample(false)
	sample(false)
	expect("went quiet while viewed", protocol.PaneActivity{State: protocol.AgentIdle})
	p.viewed.Store(false)

	// Echo and redraws right after input are not the agent working.
	p.lastInput = time.Now()
	sample(true)
	sample(true)
	expect("input echo", protocol.PaneActivity{State: protocol.AgentIdle})
	p.lastInput = time.Time{}

	// Reports take precedence over output timing.
	if err := p.reportAgent(protocol.AgentWorking, "reviewer", nil); err != nil {
		t.Fatal(err)
	}
	sample(true)
	sample(true)
	sample(false)
	sample(false)
	expect("reported working", protocol.PaneActivity{Agent: "reviewer", State: protocol.AgentWorking})

	// Leaving the foreground drops the stale report and the row.
	p.observeAgentLocked("")
	if a, ok := p.activityLocked(1); ok {
		t.Fatalf("agent exit left a row: %+v", a)
	}
}

func TestSampledForegroundAgentAppearsAndLeaves(t *testing.T) {
	s := editorNavigationSession(t)
	// A copy of this test binary named like an agent; system binaries such
	// as /bin/sleep refuse to run once copied on macOS.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	agentDir := realDir(t, t.TempDir())
	agent := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(agent, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	// The agent runs in its own directory; the pane's shell stays where it is.
	command := "(cd " + strconv.Quote(agentDir) + " && BMUX_TEST_EDITOR_HELPER=1 exec " + strconv.Quote(agent) + " -test.run '^TestEditorNavigationHelper$')\n"
	if err := s.input(protocol.Request{Op: "paste", PaneID: 1, Text: command}); err != nil {
		t.Fatal(err)
	}
	await := func(step string, done func(protocol.PaneActivity, bool) bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			s.sampleAgents()
			a, ok := activity(t, s)[1]
			if done(a, ok) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %+v present=%v", step, a, ok)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	await("agent detected", func(a protocol.PaneActivity, ok bool) bool { return ok && a.Agent == "opencode" })
	if _, ok := activity(t, s)[2]; ok {
		t.Fatal("detection leaked into another pane")
	}
	// A restart reopens the agent where it ran, not where the shell is.
	snap, _ := s.persistentState()
	if got := snap.Panes[0]; got.Dir != agentDir || got.Agent != "opencode" || !reflect.DeepEqual(got.Resume, []string{"opencode", "--continue"}) {
		t.Fatalf("saved agent pane: %+v, want dir %s", got, agentDir)
	}
	if err := s.input(protocol.Request{Op: "key", PaneID: 1, Code: 'q'}); err != nil {
		t.Fatal(err)
	}
	await("agent exit", func(a protocol.PaneActivity, ok bool) bool { return !ok })
}
