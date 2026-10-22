package server

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestRelaunchSavesAllowlistedJobsAndStartsThemAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("/bin/sh")
	t.Cleanup(srv.Close)
	// bash as a job leader stands in for a program with children, like man.
	// Not sh: macOS's /bin/sh is a shim that runs bash, so its name is bash.
	useConfig(t, srv, "[restore]\nrelaunch = [\"tail\", \"bash\"]\n")
	dir := realDir(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("NOTE_LINE\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := srv.request(protocol.Request{Op: "create", Name: "r", Dir: dir, Width: 160, Height: 40}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s := srv.sessions["r"]
	for range 3 {
		mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"})
	}
	jobs := map[int]string{
		1: "tail -f notes.txt",
		2: "tail -f notes.txt | cat",       // a pipeline: tail's output went to cat
		3: "sleep 30",                      // not allowlisted
		4: "bash -c 'sleep 30; echo done'", // allowlisted, with a child
	}
	for id, job := range jobs {
		mustInput(t, s, protocol.Request{Op: "paste", PaneID: id, Text: job + "\n"})
	}
	// Wait for each job to be complete in front of its shell.
	ready := func(id, members int) {
		t.Helper()
		p := s.panes[id]
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if group, err := foregroundPTYGroup(p.pty); err == nil && group > 0 && group != p.cmd.Process.Pid {
				if got, err := groupMembers(group); err == nil && len(got) >= members {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("pane %d: %q never started", id, jobs[id])
			}
		}
	}
	ready(1, 1)
	ready(2, 2)
	ready(3, 1)
	ready(4, 2)

	snap, ok := s.persistentState()
	if !ok {
		t.Fatal("no snapshot")
	}
	want := map[int][]string{1: {"tail", "-f", "notes.txt"}, 2: nil, 3: nil, 4: {"bash", "-c", "sleep 30; echo done"}}
	for _, p := range snap.Panes {
		if !reflect.DeepEqual(p.Relaunch, want[p.ID]) {
			t.Errorf("pane %d (%s) saved relaunch %q, want %q", p.ID, jobs[p.ID], p.Relaunch, want[p.ID])
		}
	}

	// An agent's resume command wins over relaunching.
	saved := agentContinue
	agentContinue = map[string][]string{"fake": {"printf", "RESUMED\\n"}}
	t.Cleanup(func() { agentContinue = saved })
	s.panes[1].mu.Lock()
	s.panes[1].detected = "fake"
	s.panes[1].mu.Unlock()
	agentSnap, _ := s.persistentState()
	if p := agentSnap.Panes[0]; p.Relaunch != nil || len(p.Resume) == 0 {
		t.Fatalf("agent pane: relaunch %q, resume %q", p.Relaunch, p.Resume)
	}
	s.panes[1].mu.Lock()
	s.panes[1].detected = ""
	s.panes[1].mu.Unlock()

	// Restoring types allowlisted lines only, checked against the list in
	// effect then: a line edited into the file to run something else doesn't.
	snap.Name = "r2"
	snap.Panes[2].Relaunch = []string{"sh", "-c", "echo SHOULD_NOT_RUN"}
	useConfig(t, srv, "[restore]\nrelaunch = [\"tail\"]\n")
	restored, err := restoreSession(snap, "/bin/sh", 160, 40, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.stop)
	awaitPaneText(t, restored, 1, "NOTE_LINE") // tail -f runs again and prints the file
	time.Sleep(300 * time.Millisecond)
	for _, id := range []int{2, 3, 4} {
		_, text, _ := restored.capturePane(paneTarget{pane: id})
		for _, ran := range []string{"NOTE_LINE", "SHOULD_NOT_RUN", "sleep 30"} {
			if strings.Contains(text, ran) {
				t.Fatalf("pane %d ran something on restore:\n%s", id, text)
			}
		}
	}

	// An empty list turns it off.
	useConfig(t, srv, "[restore]\nrelaunch = []\n")
	if argv := s.panes[1].relaunchCommand(currentSettings().relaunch); argv != nil {
		t.Fatalf("relaunch = [] still saved %q", argv)
	}
}

func TestRelaunchLines(t *testing.T) {
	dir := t.TempDir()
	if got := relaunchArgv([]string{"nvim"}, dir); !reflect.DeepEqual(got, []string{"nvim"}) {
		t.Fatalf("nvim without Session.vim: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "Session.vim"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for argv, want := range map[string][]string{
		"nvim":             {"nvim", "-S"},
		"/usr/bin/vim":     {"/usr/bin/vim", "-S"},
		"nvim main.go":     {"nvim", "main.go"}, // files given: keep them
		"less Session.vim": {"less", "Session.vim"},
	} {
		if got := relaunchArgv(strings.Fields(argv), dir); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q, want %q", argv, got, want)
		}
	}
	allowed := map[string]bool{"nvim": true}
	for _, tc := range []struct {
		argv []string
		ok   bool
	}{
		{[]string{"nvim", "a.go"}, true},
		{[]string{"/opt/bin/nvim"}, true},
		{[]string{"vim"}, false},
		{[]string{"nvim", "a\nrm -rf ~"}, false}, // a newline would run a second command
		{nil, false},
	} {
		if got := relaunchable(tc.argv, allowed); got != tc.ok {
			t.Errorf("relaunchable(%q) = %v", tc.argv, got)
		}
	}
	// 10 leads; 11 is its child and 12 its grandchild (man → sh → less).
	if !descendsFromLeader(10, []groupMember{{10, 1}, {11, 10}, {12, 11}}) {
		t.Error("a leader's descendants were rejected")
	}
	// 11 is the shell's other child: the other side of a pipe.
	if descendsFromLeader(10, []groupMember{{10, 1}, {11, 1}}) {
		t.Error("a pipeline was accepted")
	}
	if descendsFromLeader(10, []groupMember{{11, 10}}) {
		t.Error("a group whose leader exited was accepted")
	}
}
