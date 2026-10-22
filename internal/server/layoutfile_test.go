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

const handLayout = `active = 2

[[windows]]
name = "edit"
focused = 2
[windows.layout]
direction = "horizontal"
[windows.layout.first]
pane = 1
[windows.layout.second]
pane = 2

[[windows]]
name = "logs"
[windows.layout]
pane = 3

[[panes]]
id = 1

[[panes]]
id = 2
dir = "sub"
command = "echo LAYOUT_$((40+2))"

[[panes]]
id = 3
command = "echo LOGS_$((1+1))"
`

func layoutProject(t *testing.T, content string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := realDir(t, t.TempDir())
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LayoutFileName), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func createIn(t *testing.T, srv *Server, name, dir string) *session {
	t.Helper()
	if r := srv.request(protocol.Request{Op: "create", Name: name, Dir: dir, Width: 120, Height: 40}); r.Error != "" {
		t.Fatal(r.Error)
	}
	return srv.sessions[name]
}

func TestTrustedLayoutFileBuildsWindowsAndRunsCommands(t *testing.T) {
	dir := layoutProject(t, handLayout)
	srv := New("/bin/sh")
	t.Cleanup(srv.Close)
	srv.SetLayoutTrust(filepath.Join(t.TempDir(), "trust.json"))
	// Trusted through a symlink, used through the real path: one file.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := TrustLayout(srv.trustPath(), link); err != nil {
		t.Fatal(err)
	}
	s := createIn(t, srv, "proj", dir)
	for _, info := range srv.request(protocol.Request{Op: "list"}).Sessions {
		if info.Restored {
			t.Fatalf("a session built from a layout file is marked restored: %+v", info)
		}
	}
	f := snapshot(s)
	if got := windowPaneIDs(f); !reflect.DeepEqual(got, [][]int{{1, 2}, {3}}) || activeWindow(f) != 2 {
		t.Fatalf("windows %v active %d", got, activeWindow(f))
	}
	if f.Windows[0].Name != "edit" || f.Windows[1].Name != "logs" {
		t.Fatalf("names %+v", f.Windows)
	}
	s.mu.Lock()
	focused, sub, logs := s.windows[0].focused, s.panes[2], s.panes[3]
	s.mu.Unlock()
	if focused != 2 {
		t.Fatalf("window 1 focus %d", focused)
	}
	awaitCwd(t, sub, filepath.Join(dir, "sub"))
	awaitFrame(t, sub, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "LAYOUT_42") })
	awaitFrame(t, logs, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "LOGS_2") })
	if notice := s.takeNotice(); notice != "" {
		t.Fatalf("trusted layout left a notice: %q", notice)
	}

	// Editing the file untrusts it: the next session gets its windows but
	// none of its commands.
	edited := strings.Replace(handLayout, "LOGS_", "EDITED_", 1)
	if err := os.WriteFile(filepath.Join(dir, LayoutFileName), []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	again := createIn(t, srv, "proj-2", dir)
	if got := windowPaneIDs(snapshot(again)); !reflect.DeepEqual(got, [][]int{{1, 2}, {3}}) {
		t.Fatalf("untrusted windows %v", got)
	}
	notice := again.takeNotice()
	if !strings.Contains(notice, "isn't trusted") || !strings.Contains(notice, "bmux layout trust") || again.takeNotice() != "" {
		t.Fatalf("notice %q", notice)
	}
	time.Sleep(300 * time.Millisecond)
	again.mu.Lock()
	pane := again.panes[3]
	again.mu.Unlock()
	if f, _ := pane.frame(0); strings.Contains(frameText(f), "EDITED_") {
		t.Fatal("an untrusted layout's command was typed")
	}
}

func TestBrokenLayoutFileGivesOnePaneAndSaysWhy(t *testing.T) {
	for name, content := range map[string]string{
		"syntax":        "[[windows]\n",
		"unknown key":   "[[windows]]\nlayuot = 1\n",
		"missing pane":  "[[windows]]\n[windows.layout]\npane = 4\n[[panes]]\nid = 1\n",
		"unused pane":   "[[windows]]\n[windows.layout]\npane = 1\n[[panes]]\nid = 1\n[[panes]]\nid = 2\n",
		"missing dir":   "[[windows]]\n[windows.layout]\npane = 1\n[[panes]]\nid = 1\ndir = \"nope\"\n",
		"two lines":     "[[windows]]\n[windows.layout]\npane = 1\n[[panes]]\nid = 1\ncommand = \"a\\nb\"\n",
		"no windows":    "[[panes]]\nid = 1\n",
		"active window": "active = 3\n[[windows]]\n[windows.layout]\npane = 1\n[[panes]]\nid = 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := layoutProject(t, content)
			srv := New("/bin/sh")
			t.Cleanup(srv.Close)
			s := createIn(t, srv, "proj", dir)
			s.mu.Lock()
			panes, windows := len(s.panes), len(s.windows)
			s.mu.Unlock()
			if panes != 1 || windows != 1 {
				t.Fatalf("%d panes in %d windows", panes, windows)
			}
			if notice := s.takeNotice(); !strings.Contains(notice, LayoutFileName) || !strings.Contains(notice, "started one pane instead") {
				t.Fatalf("notice %q", notice)
			}
		})
	}
}

func TestSavedLayoutRebuildsTheSameWindows(t *testing.T) {
	dir := layoutProject(t, handLayout)
	srv := New("/bin/sh")
	t.Cleanup(srv.Close)
	s := createIn(t, srv, "proj", dir)
	s.mu.Lock()
	awaitPane := s.panes[2]
	s.mu.Unlock()
	awaitCwd(t, awaitPane, filepath.Join(dir, "sub"))
	if err := s.reportAgent(3, protocol.AgentIdle, "", []string{"pi", "--session", "a b"}); err != nil {
		t.Fatal(err)
	}
	data, err := s.layout(dir)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	// Pane 2's command comes from the file it replaces; pane 3 now runs an
	// agent, whose resume command wins.
	for _, want := range []string{"dir = \"sub\"", "command = \"echo LAYOUT_$((40+2))\"", "command = \"pi --session 'a b'\"", "active = 2", "# bmux layout"} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved layout lacks %q:\n%s", want, text)
		}
	}
	saved, err := parseLayout(data, "copy", dir)
	if err != nil {
		t.Fatalf("saved layout doesn't parse: %v\n%s", err, text)
	}
	original, _ := s.persistentState()
	if !reflect.DeepEqual(saved.Windows, original.Windows) || saved.Active != original.Active {
		t.Fatalf("round trip changed the windows:\n got %+v\nwant %+v", saved.Windows, original.Windows)
	}
	for i, p := range saved.Panes {
		if p.ID != original.Panes[i].ID || p.Dir != original.Panes[i].Dir {
			t.Fatalf("pane %d: %+v, want %+v", i, p, original.Panes[i])
		}
	}
}
