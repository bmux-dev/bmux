package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func persistentServer(t *testing.T, path string) *Server {
	t.Helper()
	s := New("/bin/sh")
	if err := s.EnablePersistence(path); err != nil {
		t.Fatal(err)
	}
	return s
}

func realDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func awaitCwd(t *testing.T, p *pane, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := processCwd(p.cmd.Process.Pid)
		if err == nil && got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane directory = %q (%v), want %q", got, err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readState(t *testing.T, path string) stateFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestSessionsSurviveRestartAsLayoutsDirectoriesAndResumedAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saved := agentContinue
	agentContinue = map[string][]string{"fake": {"printf", "AUTO_%s\\n", "RESUMED"}}
	t.Cleanup(func() { agentContinue = saved })
	path := filepath.Join(t.TempDir(), "state", "sessions.json")
	root, work := realDir(t, t.TempDir()), realDir(t, t.TempDir())

	first := persistentServer(t, path)
	for _, name := range []string{"api", "web"} {
		if r := first.request(protocol.Request{Op: "create", Name: name, Dir: root, Width: 120, Height: 40}); r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	api := first.sessions["api"]
	for _, direction := range []string{"horizontal", "vertical"} {
		if err := api.input(protocol.Request{Op: "split", Direction: direction}); err != nil {
			t.Fatal(err)
		}
	}
	// Pane 3 (focused) moves to another directory; panes 2 and 3 run agents.
	if err := api.input(protocol.Request{Op: "paste", PaneID: 3, Text: "cd " + work + "\n"}); err != nil {
		t.Fatal(err)
	}
	awaitCwd(t, api.panes[3], work)
	if err := api.reportAgent(3, protocol.AgentWorking, "reviewer", []string{"printf", "EXACT_%s\\n", "RESUMED"}); err != nil {
		t.Fatal(err)
	}
	api.panes[2].mu.Lock()
	api.panes[2].detected = "fake"
	api.panes[2].mu.Unlock()
	first.sessions["web"].panes[1].mu.Lock()
	first.sessions["web"].panes[1].detected = "fake" // same directory and command as api pane 2
	first.sessions["web"].panes[1].mu.Unlock()
	first.Close()

	state := readState(t, path)
	if len(state.Sessions) != 2 || state.Sessions[0].Name != "api" || state.Sessions[1].Name != "web" {
		t.Fatalf("saved sessions: %+v", state.Sessions)
	}
	want := []paneSnapshot{
		{ID: 1, Dir: root},
		{ID: 2, Dir: root, Agent: "fake", Resume: []string{"printf", "AUTO_%s\\n", "RESUMED"}},
		{ID: 3, Dir: work, Agent: "reviewer", Resume: []string{"printf", "EXACT_%s\\n", "RESUMED"}, Reported: true},
	}
	if got := state.Sessions[0].Panes; !reflect.DeepEqual(got, want) {
		t.Fatalf("saved panes:\n got %+v\nwant %+v", got, want)
	}
	if state.Sessions[0].Focused != 3 {
		t.Fatalf("saved focus = %d", state.Sessions[0].Focused)
	}

	second := persistentServer(t, path)
	defer second.Close()
	restored := second.sessions["api"]
	if restored == nil || second.sessions["web"] == nil {
		t.Fatal("sessions were not restored")
	}
	restored.mu.Lock()
	gotLayout, focused, nextID := snapshotLayout(restored.root), restored.focused, restored.nextID
	restored.mu.Unlock()
	if !reflect.DeepEqual(gotLayout, state.Sessions[0].Layout) || focused != 3 || nextID != 4 {
		t.Fatalf("restored layout %+v focus %d next %d", gotLayout, focused, nextID)
	}
	awaitCwd(t, restored.panes[3], work)
	awaitCwd(t, restored.panes[1], root)
	awaitFrame(t, restored.panes[3], func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "EXACT_RESUMED") })
	awaitFrame(t, restored.panes[2], func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "AUTO_RESUMED") })
	// The second built-in "continue latest" in the same directory would reopen
	// the same conversation, so that pane gets only a shell.
	time.Sleep(300 * time.Millisecond)
	if f, _ := second.sessions["web"].panes[1].frame(0); strings.Contains(frameText(f), "AUTO_") {
		t.Fatal("a duplicate built-in resume ran twice in one directory")
	}
}

func TestShutdownSavesBeforeStoppingAndKilledSessionsStayGone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "sessions.json")
	s := persistentServer(t, path)
	for _, name := range []string{"keep", "gone"} {
		if r := s.request(protocol.Request{Op: "create", Name: name, Dir: t.TempDir()}); r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	if r := s.request(protocol.Request{Op: "kill", Name: "gone"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s.Close()
	state := readState(t, path)
	if len(state.Sessions) != 1 || state.Sessions[0].Name != "keep" {
		t.Fatalf("saved after shutdown: %+v", state.Sessions)
	}
	// Stopping panes during Close must not overwrite the final save.
	if err := s.saveState(); err != nil {
		t.Fatal(err)
	}
	if again := readState(t, path); len(again.Sessions) != 1 {
		t.Fatal("a save after shutdown erased the sessions")
	}
}

func TestUnreadableOrInvalidStateNeverBlocksStartup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	s := persistentServer(t, path)
	if len(s.sessions) != 0 {
		t.Fatal("restored from an unreadable file")
	}
	if data, err := os.ReadFile(path + ".unreadable"); err != nil || string(data) != "{not json" {
		t.Fatal("unreadable state was not kept aside")
	}
	s.Close()

	// A broken layout restores the session as one shell; a bad name is skipped.
	state := stateFile{Version: stateVersion, Sessions: []sessionSnapshot{
		{Name: "broken", Dir: dir, Layout: &layoutSnapshot{Direction: "diagonal", Ratio: 5000, First: &layoutSnapshot{Pane: 1}, Second: &layoutSnapshot{Pane: 1}}, Panes: []paneSnapshot{{ID: 1, Dir: dir}}},
		{Name: "bad name", Dir: dir, Layout: &layoutSnapshot{Pane: 1}, Panes: []paneSnapshot{{ID: 1, Dir: dir}}},
		{Name: "missing-dirs", Dir: "/does/not/exist", Layout: &layoutSnapshot{Pane: 7}, Panes: []paneSnapshot{{ID: 7, Dir: "/nope"}}},
	}}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s = persistentServer(t, path)
	defer s.Close()
	if len(s.sessions) != 2 || s.sessions["broken"] == nil || s.sessions["missing-dirs"] == nil {
		t.Fatalf("restored: %v", s.sessions)
	}
	if b := s.sessions["broken"]; len(b.panes) != 1 || b.root.id != 1 {
		t.Fatal("broken layout did not fall back to one pane")
	}
	if m := s.sessions["missing-dirs"]; m.info.Dir != os.Getenv("HOME") || m.panes[7] == nil {
		t.Fatalf("missing directories: dir %q panes %v", m.info.Dir, m.panes)
	}
}
