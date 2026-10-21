package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
	"golang.org/x/term"
)

// The bmux CLI (bmux focus) running inside a pane looks like this: read the
// session and pane identity from the environment, hand the edge direction to
// the server over the local socket. The test server listens on TCP only to
// avoid Darwin's short Unix-socket path limit under temporary directories.
func TestFocusPaneHelper(t *testing.T) {
	if os.Getenv("BMUX_TEST_FOCUS_HELPER") != "1" {
		return
	}
	writeFocusResult := func(text string) {
		_ = os.WriteFile(os.Getenv("BMUX_TEST_FOCUS_RESULT"), []byte(text), 0600)
	}
	fail := func(err error) {
		writeFocusResult(err.Error())
		os.Exit(2)
	}
	c, err := net.Dial("tcp", os.Getenv("BMUX_TEST_TCP_ADDR"))
	if err != nil {
		fail(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	sessionID, _ := strconv.ParseUint(os.Getenv("BMUX_SESSION_ID"), 10, 64)
	paneID, _ := strconv.Atoi(os.Getenv("BMUX_PANE"))
	if err := json.NewEncoder(c).Encode(protocol.Request{
		Version: protocol.Version, Op: "focus-pane", Name: os.Getenv("BMUX_SESSION"),
		SessionID: sessionID, PaneID: paneID, Direction: os.Getenv("BMUX_TEST_FOCUS_DIR"),
	}); err != nil {
		fail(err)
	}
	var response protocol.Response
	if err := json.NewDecoder(c).Decode(&response); err != nil {
		fail(err)
	}
	writeFocusResult(response.Error)
}

func TestEditorNavigationHelper(t *testing.T) {
	if os.Getenv("BMUX_TEST_EDITOR_HELPER") != "1" {
		return
	}
	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		os.Exit(2)
	}
	defer term.Restore(int(os.Stdin.Fd()), state)
	fmt.Print("EDITOR_READY\r\n")
	r := bufio.NewReader(os.Stdin)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return
		}
		if b == 'q' {
			return
		}
		fmt.Printf("CTRL_%02x\r\n", b)
	}
}

func editorNavigationSession(t *testing.T) *session {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("navigation", t.TempDir(), "/bin/sh", 160, 40)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.stop)
	if err := s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
		t.Fatal(err)
	}
	// Navigation forwards the key to the app when bmux can't read a pane's
	// foreground job, as while a starting shell forks and execs its startup
	// helpers. Tests move focus, so wait until both shells have started and
	// sit idle at a prompt: each prints a marker only its output contains.
	s.mu.Lock()
	panes := map[int]*pane{1: s.panes[1], 2: s.panes[2]}
	s.mu.Unlock()
	for id, p := range panes {
		if err := s.input(protocol.Request{Op: "paste", PaneID: id, Text: "echo IDLE_$((40+2))\n"}); err != nil {
			t.Fatal(err)
		}
		awaitFrame(t, p, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "IDLE_42") })
		waitEditorForeground(t, p, false)
	}
	return s
}

func waitEditorForeground(t *testing.T, p *pane, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		editor, err := p.foregroundEditor()
		if err == nil && editor == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	editor, err := p.foregroundEditor()
	group, _ := foregroundPTYGroup(p.pty)
	processes, _ := exec.Command("ps", "-g", strconv.Itoa(group), "-o", "pid=,pgid=,comm=,stat=").CombinedOutput()
	t.Fatalf("foreground editor=%v error=%v, want %v; group=%d processes=%s", editor, err, want, group, processes)
}

func startTestEditor(t *testing.T, s *session, id int) *pane {
	t.Helper()
	return startNavigationHelper(t, s, id, "nvim")
}

// startNavigationHelper runs a copy of the test binary named program in the
// pane, printing each byte it reads, and waits until it gets navigation keys.
func startNavigationHelper(t *testing.T, s *session, id int, program string) *pane {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	path := filepath.Join(t.TempDir(), program)
	destination, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(destination, source)
	closeErr := destination.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	s.mu.Lock()
	p := s.panes[id]
	s.mu.Unlock()
	command := "BMUX_TEST_EDITOR_HELPER=1 " + strconv.Quote(path) + " -test.run '^TestEditorNavigationHelper$'\n"
	if err := s.input(protocol.Request{Op: "paste", PaneID: id, Text: command}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, p, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "EDITOR_READY") })
	waitEditorForeground(t, p, true)
	return p
}

func focusedPaneID(s *session) int { s.mu.Lock(); defer s.mu.Unlock(); return s.focusedLocked() }

func TestEditorNavigationForwardsAllFourKeysAndResumesAfterExit(t *testing.T) {
	s := editorNavigationSession(t)
	p := startTestEditor(t, s, 1)
	if err := s.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ direction, marker string }{{"left", "CTRL_08"}, {"down", "CTRL_0a"}, {"up", "CTRL_0b"}, {"right", "CTRL_0c"}} {
		if err := s.input(protocol.Request{Op: "navigate-pane", Direction: tc.direction}); err != nil {
			t.Fatal(err)
		}
		if focusedPaneID(s) != 1 {
			t.Fatal("editor navigation moved bmux focus instead of forwarding")
		}
		awaitFrame(t, p, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), tc.marker) })
	}
	if err := s.input(protocol.Request{Op: "key", Code: 'q', PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	waitEditorForeground(t, p, false)
	if err := s.input(protocol.Request{Op: "navigate-pane", Direction: "right"}); err != nil {
		t.Fatal(err)
	}
	if focusedPaneID(s) != 2 {
		t.Fatal("shell pane navigation did not resume after editor exit")
	}
}

func TestPassThroughProgramsGetNavigationKeys(t *testing.T) {
	s := editorNavigationSession(t)
	useConfig(t, New("/bin/sh"), "[keys]\npass-through = [\"fzf\"]\n")
	p := startNavigationHelper(t, s, 1, "fzf")
	if err := s.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.input(protocol.Request{Op: "navigate-pane", Direction: "right"}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, p, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "CTRL_0c") })
	if focusedPaneID(s) != 1 {
		t.Fatal("a pass-through program's navigation key moved bmux focus")
	}
	// Off the list, the same program no longer gets them.
	activeSettings.Store(settingsFrom(config.Default()))
	if err := s.input(protocol.Request{Op: "navigate-pane", Direction: "right"}); err != nil {
		t.Fatal(err)
	}
	if focusedPaneID(s) != 2 {
		t.Fatal("navigation went to a program that isn't on the pass-through list")
	}
}

func TestEditorNavigationUsesFocusedForegroundGroupNotTitlesOrOtherPanes(t *testing.T) {
	s := editorNavigationSession(t)
	_ = startTestEditor(t, s, 2)
	if err := s.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	shell := s.panes[1]
	s.mu.Unlock()
	waitEditorForeground(t, shell, false)
	shell.mu.Lock()
	_, _ = shell.term.WriteString("\x1b]0;nvim\x07")
	shell.mu.Unlock()
	if err := s.input(protocol.Request{Op: "navigate-pane", Direction: "right"}); err != nil {
		t.Fatal(err)
	}
	if focusedPaneID(s) != 2 {
		t.Fatal("editor in another pane or spoofed title captured shell navigation")
	}
	if err := s.input(protocol.Request{Op: "select-pane", Direction: "left"}); err != nil {
		t.Fatal(err)
	}
	if focusedPaneID(s) != 1 {
		t.Fatal("explicit bmux focus was intercepted by the editor")
	}
	if err := s.input(protocol.Request{Op: "navigate-pane", Direction: "invalid"}); err == nil {
		t.Fatal("invalid navigation direction accepted")
	}
}

func TestFocusPaneMovesFromRequestedPane(t *testing.T) {
	s := editorNavigationSession(t)
	// After the split: pane 1 left, pane 2 right, focus on pane 2.
	if err := s.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	if got := focusedPaneID(s); got != 1 {
		t.Fatalf("setup focus: %d", got)
	}
	// The request targets the pane the editor lives in; focus moves from there.
	if err := s.input(protocol.Request{Op: "focus-pane", PaneID: 2, Direction: "left"}); err != nil {
		t.Fatal(err)
	}
	if got := focusedPaneID(s); got != 1 {
		t.Fatalf("focus-pane left of pane 2: focus %d, want 1", got)
	}
	// No neighbor is a silent no-op, not an error.
	if err := s.input(protocol.Request{Op: "focus-pane", PaneID: 1, Direction: "left"}); err != nil {
		t.Fatal(err)
	}
	if got := focusedPaneID(s); got != 1 {
		t.Fatalf("edge crossing without neighbor moved focus to %d", got)
	}
	if err := s.input(protocol.Request{Op: "focus-pane", PaneID: 1, Direction: "diagonal"}); err == nil {
		t.Fatal("unknown direction accepted")
	}
	if err := s.input(protocol.Request{Op: "focus-pane", PaneID: 99, Direction: "left"}); err == nil {
		t.Fatal("unknown pane accepted")
	}
}

// Like tmux's select-pane, directional navigation from a zoomed pane unzooms
// and moves. With no neighbor in that direction, the pane stays zoomed.
func TestDirectionalNavigationUnzoomsWhileZoomed(t *testing.T) {
	for _, op := range []string{"select-pane", "navigate-pane", "focus-pane"} {
		t.Run(op, func(t *testing.T) {
			s := editorNavigationSession(t)
			zoom := func() {
				t.Helper()
				if err := s.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
					t.Fatal(err)
				}
				if err := s.input(protocol.Request{Op: "zoom", PaneID: 1}); err != nil {
					t.Fatal(err)
				}
			}
			move := func(direction string) {
				t.Helper()
				r := protocol.Request{Op: op, Direction: direction}
				if op == "focus-pane" {
					r.PaneID = 1
				}
				if err := s.input(r); err != nil {
					t.Fatal(err)
				}
			}
			zoom()
			move("left")
			if f := snapshot(s); f.Focused != 1 || !f.Zoomed {
				t.Fatalf("navigation without neighbor: focused %d zoomed %v, want 1 zoomed", f.Focused, f.Zoomed)
			}
			move("right")
			f := snapshot(s)
			if f.Focused != 2 || f.Zoomed || len(f.Panes) != 2 {
				t.Fatalf("navigation while zoomed: focused %d zoomed %v panes %d, want 2 unzoomed with 2 panes", f.Focused, f.Zoomed, len(f.Panes))
			}
		})
	}
}

func TestStandaloneFocusPaneRouting(t *testing.T) {
	s := editorNavigationSession(t)
	srv := New("/bin/sh")
	srv.sessions["api"] = s
	if r := srv.request(protocol.Request{Op: "focus-pane", Name: "api", PaneID: 2, Direction: "left"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if got := focusedPaneID(s); got != 1 {
		t.Fatalf("routed focus-pane left of pane 2: focus %d, want 1", got)
	}
	if r := srv.request(protocol.Request{Op: "focus-pane", Name: "missing", Direction: "left"}); r.Error == "" {
		t.Fatal("unknown session accepted")
	}
	if r := srv.request(protocol.Request{Op: "paste", Name: "api", Text: "x"}); r.Error == "" {
		t.Fatal("one-shot paste must not bypass attached-client routing")
	}
}

// zoomed answers for the window showing the requested pane, not the active
// window, and leaves focus and recent-use order alone.
func TestZoomedReportsRequestedPanesWindow(t *testing.T) {
	s := editorNavigationSession(t)
	srv := New("/bin/sh")
	srv.sessions["api"] = s
	zoomed := func(r protocol.Request) protocol.Response {
		t.Helper()
		r.Op, r.Name = "zoomed", "api"
		return srv.request(r)
	}
	if r := zoomed(protocol.Request{PaneID: 2}); r.Error != "" || r.Zoomed {
		t.Fatalf("unzoomed window: %+v", r)
	}
	if err := s.input(protocol.Request{Op: "zoom", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	// Zoom is per window: the hidden neighbor shares the answer.
	for _, id := range []int{0, 1, 2} {
		if r := zoomed(protocol.Request{PaneID: id}); r.Error != "" || !r.Zoomed {
			t.Fatalf("pane %d in zoomed window: %+v", id, r)
		}
	}
	// A second window comes forward; the first stays zoomed behind it.
	if err := s.input(protocol.Request{Op: "new-window"}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	newPane := s.focusedLocked()
	s.mu.Unlock()
	if newPane == 1 || newPane == 2 {
		t.Fatalf("new window focus %d", newPane)
	}
	if r := zoomed(protocol.Request{PaneID: 1}); r.Error != "" || !r.Zoomed {
		t.Fatalf("background zoomed window: %+v", r)
	}
	if r := zoomed(protocol.Request{PaneID: newPane}); r.Error != "" || r.Zoomed {
		t.Fatalf("active unzoomed window: %+v", r)
	}
	// Read-only: no focus change, no recent-use update.
	s.mu.Lock()
	before := s.info.Used
	s.mu.Unlock()
	zoomed(protocol.Request{PaneID: 1})
	s.mu.Lock()
	after := s.info.Used
	s.mu.Unlock()
	if got := focusedPaneID(s); got != newPane || !after.Equal(before) {
		t.Fatalf("zoomed changed state: focus %d, used %v -> %v", got, before, after)
	}
	if r := zoomed(protocol.Request{PaneID: 99}); r.Error == "" {
		t.Fatal("unknown pane accepted")
	}
	if r := srv.request(protocol.Request{Op: "zoomed", Name: "missing", PaneID: 1}); r.Error == "" {
		t.Fatal("unknown session accepted")
	}
	if r := zoomed(protocol.Request{SessionID: s.id + 100, PaneID: 1}); r.Error == "" {
		t.Fatal("unknown session ID fell back to the session name")
	}
}

// Panes keep the BMUX_SESSION they started with. The session ID keeps
// bmux focus working after a rename, even if the old name is reused.
func TestFocusPaneFollowsRenamedSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("/bin/sh")
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	for _, r := range []protocol.Request{
		{Op: "create", Name: "api", Dir: dir, Width: 160, Height: 40},
		{Op: "rename", Name: "api", NewName: "web"},
		{Op: "create", Name: "api", Dir: dir, Width: 160, Height: 40},
	} {
		if response := srv.request(r); response.Error != "" {
			t.Fatalf("%s: %s", r.Op, response.Error)
		}
	}
	renamed, reused := srv.sessions["web"], srv.sessions["api"]
	for _, s := range []*session{renamed, reused} {
		if err := s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
			t.Fatal(err)
		}
	}
	if r := srv.request(protocol.Request{Op: "focus-pane", Name: "api", SessionID: renamed.id, PaneID: 2, Direction: "left"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if got, other := focusedPaneID(renamed), focusedPaneID(reused); got != 1 || other != 2 {
		t.Fatalf("focus after rename: renamed session %d, reused name %d; want 1 and 2", got, other)
	}
	if r := srv.request(protocol.Request{Op: "focus-pane", Name: "web", SessionID: reused.id + 100, PaneID: 2, Direction: "left"}); r.Error == "" {
		t.Fatal("unknown session ID fell back to the session name")
	}
	if !strings.Contains(strings.Join(renamed.panes[1].cmd.Env, "\n"), "BMUX_SESSION_ID="+strconv.FormatUint(renamed.id, 10)) {
		t.Fatal("pane environment is missing BMUX_SESSION_ID")
	}
}

func TestEditorCommandMatchingIsExact(t *testing.T) {
	for _, command := range []string{"nvim", "/usr/local/bin/nvim", "vim", "vimdiff", "view"} {
		if !isEditorCommand(command) {
			t.Fatalf("editor command not recognized: %s", command)
		}
	}
	for _, command := range []string{"sh", "bash", "nvim-helper", "not-nvim", "vim-language-server"} {
		if isEditorCommand(command) {
			t.Fatalf("unrelated command recognized as editor: %s", command)
		}
	}
}

func TestRealNeovimControlNavigationStaysInsideEditor(t *testing.T) {
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("Neovim not installed")
	}
	s := editorNavigationSession(t)
	if err := s.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	output, config := filepath.Join(dir, "position"), filepath.Join(dir, "init.lua")
	lua := fmt.Sprintf(`
vim.o.splitright = true
vim.o.splitbelow = true
local function record(key)
  local p = vim.api.nvim_win_get_position(0)
  vim.fn.writefile({key, tostring(vim.api.nvim_get_current_win()), tostring(p[1]), tostring(p[2])}, %q)
end
vim.cmd("vsplit")
vim.cmd("split")
vim.cmd("wincmd h")
vim.cmd("split")
vim.cmd("wincmd t")
for _, key in ipairs({"h", "j", "k", "l"}) do
  local direction = key
  vim.keymap.set("n", "<C-" .. key .. ">", function()
    vim.cmd("wincmd " .. direction)
    record(direction)
  end)
end
record("ready")
`, output)
	if err := os.WriteFile(config, []byte(lua), 0600); err != nil {
		t.Fatal(err)
	}
	command := strconv.Quote(nvim) + " -u " + strconv.Quote(config) + " -i NONE -n\n"
	if err := s.input(protocol.Request{Op: "paste", PaneID: 1, Text: command}); err != nil {
		t.Fatal(err)
	}
	readPosition := func(key string) (int, int, int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(output)
			fields := strings.Fields(string(data))
			if len(fields) == 4 && fields[0] == key {
				id, e1 := strconv.Atoi(fields[1])
				row, e2 := strconv.Atoi(fields[2])
				col, e3 := strconv.Atoi(fields[3])
				if e1 == nil && e2 == nil && e3 == nil {
					return id, row, col
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		data, _ := os.ReadFile(output)
		t.Fatalf("Neovim didn't receive %q navigation; last position %q", key, data)
		return 0, 0, 0
	}
	initial, _, _ := readPosition("ready")
	s.mu.Lock()
	p := s.panes[1]
	s.mu.Unlock()
	waitEditorForeground(t, p, true)
	for _, tc := range []struct {
		direction, key string
		bottom, right  bool
	}{
		{"right", "l", false, true}, {"down", "j", true, true}, {"left", "h", true, false}, {"up", "k", false, false},
	} {
		if err := s.input(protocol.Request{Op: "navigate-pane", Direction: tc.direction}); err != nil {
			t.Fatal(err)
		}
		id, row, col := readPosition(tc.key)
		if (row > 0) != tc.bottom || (col > 0) != tc.right {
			t.Fatalf("wrong Neovim split after %s: row=%d col=%d", tc.direction, row, col)
		}
		if focusedPaneID(s) != 1 {
			t.Fatal("Ctrl navigation escaped into adjacent bmux pane")
		}
		if tc.key == "k" && id != initial {
			t.Fatal("four motions did not return to starting Neovim window")
		}
	}
}

func TestRealNeovimEdgeHandsFocusTobmuxPane(t *testing.T) {
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("Neovim not installed")
	}
	t.Setenv("HOME", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- s.Serve(l) }()
	t.Cleanup(func() {
		s.Close()
		_ = <-finished
	})
	// The helper identity must be in the process environment before the panes
	// are created: pane shellEnv captures and forwards it to the editor.
	t.Setenv("BMUX_TEST_BIN", func() string { b, _ := os.Executable(); return b }())
	binary := os.Getenv("BMUX_TEST_BIN")
	t.Setenv("BMUX_TEST_TCP_ADDR", l.Addr().String())
	dir := t.TempDir()
	result, config := filepath.Join(dir, "focus-result"), filepath.Join(dir, "init.lua")
	t.Setenv("BMUX_TEST_FOCUS_RESULT", result)
	stub := filepath.Join(dir, "bmux")
	script := "#!/bin/sh\nBMUX_TEST_FOCUS_HELPER=1 BMUX_TEST_FOCUS_DIR=\"$2\" exec " +
		strconv.Quote(binary) + " -test.run '^TestFocusPaneHelper$' >/dev/null 2>&1\n"
	if err := os.WriteFile(stub, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	// The mapping mirrors the user-facing Neovim integration: move inside the
	// editor, and at an edge ask bmux to focus the neighboring pane.
	lua := fmt.Sprintf(`
local bmux_command = %q
local function record(key)
  local p = vim.api.nvim_win_get_position(0)
  vim.fn.writefile({key, tostring(vim.api.nvim_get_current_win()), tostring(p[1]), tostring(p[2])}, %q)
end
local directions = { h = "left", j = "down", k = "up", l = "right" }
local keys = {}
for key, direction in pairs(directions) do
  keys[key] = direction
  vim.keymap.set("n", "<C-" .. key .. ">", function()
    record(direction)
    local before = vim.fn.winnr()
    pcall(vim.cmd, "wincmd " .. key)
    if vim.fn.winnr() == before then
      vim.fn.system({ bmux_command, "focus", direction })
    end
  end, { silent = true })
end
vim.o.splitright = true
vim.cmd("vsplit")
vim.cmd("wincmd h")
record("ready")
`, stub, filepath.Join(dir, "position"))
	if err := os.WriteFile(config, []byte(lua), 0600); err != nil {
		t.Fatal(err)
	}
	if r := s.request(protocol.Request{Op: "create", Name: "edge", Dir: dir, Width: 160, Height: 40}); r.Error != "" {
		t.Fatal(r.Error)
	}
	sess := s.sessions["edge"]
	if err := sess.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
		t.Fatal(err)
	}
	// Layout: pane 1 left (Neovim), pane 2 right (shell). Start focused on 1.
	if err := sess.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	if got := focusedPaneID(sess); got != 1 {
		t.Fatalf("setup focus: %d", got)
	}
	sess.mu.Lock()
	p := sess.panes[1]
	sess.mu.Unlock()
	command := strconv.Quote(nvim) + " -u " + strconv.Quote(config) + " -i NONE -n\n"
	if err := p.input(protocol.Request{Op: "paste", Text: command}); err != nil {
		t.Fatal(err)
	}
	readRecord := func(key string) (int, int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(filepath.Join(dir, "position"))
			fields := strings.Fields(string(data))
			if len(fields) == 4 && fields[0] == key {
				row, e1 := strconv.Atoi(fields[2])
				col, e2 := strconv.Atoi(fields[3])
				if e1 == nil && e2 == nil {
					return row, col
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "position"))
		t.Fatalf("Neovim didn't receive %q navigation; last position %q", key, data)
		return 0, 0
	}
	_, _ = readRecord("ready")
	waitEditorForeground(t, p, true)
	// First right: moves inside Neovim's own split. bmux must keep focus.
	if err := sess.input(protocol.Request{Op: "navigate-pane", Direction: "right"}); err != nil {
		t.Fatal(err)
	}
	readRecord("right")
	if got := focusedPaneID(sess); got != 1 {
		t.Fatalf("internal Neovim navigation escaped to bmux pane %d", got)
	}
	// Second right: past the editor edge, so Neovim hands off via bmux focus.
	if err := sess.input(protocol.Request{Op: "navigate-pane", Direction: "right"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for focusedPaneID(sess) != 2 {
		if time.Now().After(deadline) {
			handoff, _ := os.ReadFile(result)
			t.Fatalf("edge crossing did not move bmux focus; handoff error %q", handoff)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(result); err == nil {
		if data, readErr := os.ReadFile(result); readErr == nil && len(data) > 0 {
			t.Fatalf("handoff reported an error: %q", data)
		}
	}
}
