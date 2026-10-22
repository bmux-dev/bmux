package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// awaitLines waits until path holds a line for every want, in any order.
func awaitLines(t *testing.T, path string, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		missing := ""
		for _, w := range want {
			if !strings.Contains(string(data), w) {
				missing = w
				break
			}
		}
		if missing == "" {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never got %q:\n%s", path, missing, data)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHooksRunForEveryEventWithThePanesVariablesAndEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- srv.Serve(l) }()
	defer func() { srv.Close(); <-finished }()

	log := filepath.Join(t.TempDir(), "hooks.log")
	var config strings.Builder
	config.WriteString("[hooks]\n")
	for _, event := range []string{"session-created", "session-closed", "window-created", "pane-exited", "client-attached", "client-detached", "agent-blocked", "agent-idle"} {
		// The format variable, the environment and the directory, per event.
		config.WriteString(event + ` = ["echo ` + event + ` #{pane_target} hook=$BMUX_HOOK pane=$BMUX_PANE dir=$PWD >> ` + log + `"]` + "\n")
	}
	useConfig(t, srv, config.String())

	dir := realDir(t, t.TempDir())
	if r := srv.request(protocol.Request{Op: "create", Name: "h", Dir: dir, Width: 80, Height: 24}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s := srv.sessions["h"]
	awaitLines(t, log, "session-created h:1 hook=session-created pane=1 dir="+dir)

	mustInput(t, s, protocol.Request{Op: "new-window"})
	awaitLines(t, log, "window-created h:2 hook=window-created pane=2")

	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	if err := enc.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "h", Width: 80, Height: 24}); err != nil {
		t.Fatal(err)
	}
	var r protocol.Response
	if err := dec.Decode(&r); err != nil || r.Error != "" {
		t.Fatalf("attach: %+v %v", r, err)
	}
	awaitLines(t, log, "client-attached h:2")
	c.Close()
	awaitLines(t, log, "client-detached h:2")

	for _, state := range []string{protocol.AgentWorking, protocol.AgentBlocked, protocol.AgentWorking, protocol.AgentIdle} {
		if err := s.reportAgent(1, state, "claude", nil); err != nil {
			t.Fatal(err)
		}
	}
	awaitLines(t, log, "agent-blocked h:1 hook=agent-blocked pane=1", "agent-idle h:1 hook=agent-idle pane=1")

	if err := s.closePane(2); err != nil {
		t.Fatal(err)
	}
	awaitLines(t, log, "pane-exited h:2 hook=pane-exited pane=2")
	if err := s.closePane(1); err != nil {
		t.Fatal(err)
	}
	text := awaitLines(t, log, "pane-exited h:1", "session-closed h:1 hook=session-closed pane=1")

	// Each state change fires once: no second blocked or idle line.
	if strings.Count(text, "agent-blocked h:") != 1 || strings.Count(text, "agent-idle h:") != 1 {
		t.Fatalf("agent hooks fired more than once:\n%s", text)
	}
}

func TestHooksAreBoundedInTimeAndNumber(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ran := filepath.Join(t.TempDir(), "ran")
	useConfig(t, New("/bin/sh"), `[hooks]
session-closed = ["echo ran >> `+ran+`"]
`)
	saved := hookTimeout
	hookTimeout = 200 * time.Millisecond
	t.Cleanup(func() { hookTimeout = saved })
	start := time.Now()
	runHook("pane-exited", "sleep 30", protocol.PaneInfo{}, hookTimeout)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("a hook ran %v past its time limit", elapsed)
	}

	// With every slot taken, more hooks are dropped rather than queued.
	for range maxHooks {
		hookSlots <- struct{}{}
	}
	runHooks("session-closed", protocol.PaneInfo{})
	for range maxHooks {
		<-hookSlots
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a hook ran past the concurrency cap")
	}
	// With a slot free, it runs.
	runHooks("session-closed", protocol.PaneInfo{})
	awaitLines(t, ran, "ran")
}
