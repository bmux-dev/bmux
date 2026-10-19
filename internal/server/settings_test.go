package server

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

func useConfig(t *testing.T, s *Server, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { activeSettings.Store(settingsFrom(config.Default())) })
	if err := s.LoadSettings(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAgentSettingsChangeDetectionAndTiming(t *testing.T) {
	useConfig(t, New("/bin/sh"), `[agents]
extra = ["myagent"]
working_after = "3s"
waiting_after = "1s"
`)
	if agentMatch("/usr/local/bin/myagent") != "myagent" || agentMatch("claude") != "claude" {
		t.Fatal("extra agent names don't add to the built-in ones")
	}
	p := &pane{}
	sample := func(output bool) protocol.PaneActivity {
		if output {
			p.noteOutputLocked(p.lastInput.Add(inputEchoWindow * 2))
		}
		p.observeAgentLocked("myagent")
		a, _ := p.activityLocked(1)
		return a
	}
	sample(true)
	if sample(true).State == protocol.AgentWorking {
		t.Fatal("working before working_after")
	}
	if sample(true).State != protocol.AgentWorking {
		t.Fatal("not working after 3 samples of output")
	}
	if a := sample(false); a.State != protocol.AgentIdle || !a.Attention {
		t.Fatalf("waiting_after of 1s: %+v", a)
	}
}

func TestDetectionCanBeTurnedOff(t *testing.T) {
	s := editorNavigationSession(t)
	s.panes[1].mu.Lock()
	s.panes[1].detected = "codex"
	s.panes[1].mu.Unlock()
	srv := New("/bin/sh")
	srv.sessions["navigation"] = s
	useConfig(t, srv, "[agents]\ndetect = false\n")
	if err := srv.reloadSettings(); err != nil {
		t.Fatal(err)
	}
	if _, ok := activity(t, s)[1]; ok {
		t.Fatal("turning detection off kept a detected agent")
	}
	s.sampleAgents() // must not detect anything now
	if _, ok := activity(t, s)[1]; ok {
		t.Fatal("sampling still detects agents")
	}
}

func TestResumeOverridesAndRestoreOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("/bin/sh")
	useConfig(t, srv, `[restore.resume]
claude = ["claude", "--continue", "--model", "opus"]
codex = []
`)
	settings := currentSettings()
	if argv, ok := settings.resumeFor("claude"); !ok || !reflect.DeepEqual(argv, []string{"claude", "--continue", "--model", "opus"}) {
		t.Fatalf("claude override: %v", argv)
	}
	if _, ok := settings.resumeFor("codex"); ok {
		t.Fatal("an empty override still resumes")
	}
	if argv, ok := settings.resumeFor("opencode"); !ok || argv[0] != "opencode" {
		t.Fatal("agents without an override lost their built-in command")
	}

	// With restore off, nothing is restored or written, and the file stays.
	path := filepath.Join(t.TempDir(), "sessions.json")
	first := persistentServer(t, path)
	if r := first.request(protocol.Request{Op: "create", Name: "kept", Dir: t.TempDir()}); r.Error != "" {
		t.Fatal(r.Error)
	}
	first.Close()
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	off := New("/bin/sh")
	useConfig(t, off, "[restore]\nenabled = false\n")
	if err := off.EnablePersistence(path); err != nil {
		t.Fatal(err)
	}
	if len(off.sessions) != 0 {
		t.Fatal("restored with restore = false")
	}
	if r := off.request(protocol.Request{Op: "create", Name: "new", Dir: t.TempDir()}); r.Error != "" {
		t.Fatal(r.Error)
	}
	off.Close()
	if now, _ := os.ReadFile(path); string(now) != string(saved) {
		t.Fatal("restore = false overwrote the saved sessions")
	}
}

func TestReloadRereadsSettingsAndBumpsTheFrameRevision(t *testing.T) {
	s := editorNavigationSession(t)
	srv := New("/bin/sh")
	srv.sessions["navigation"] = s
	path := useConfig(t, srv, "")
	before, _ := s.snapshot(map[int]int{}, map[int]int{}, 0, true)
	if err := os.WriteFile(path, []byte("[agents]\nextra = [\"newagent\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := srv.request(protocol.Request{Op: "reload-config"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if agentMatch("newagent") != "newagent" {
		t.Fatal("reload didn't apply the new settings")
	}
	after, revision := s.snapshot(map[int]int{}, map[int]int{}, 0, false)
	if after == nil || after.Config <= before.Config {
		t.Fatalf("frame config revision did not advance: %v revision %d", after, revision)
	}
	if err := os.WriteFile(path, []byte("[agents]\nwaiting_after = \"oops\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := srv.request(protocol.Request{Op: "reload-config"}); r.Error == "" {
		t.Fatal("an invalid config reloaded without an error")
	}
}
