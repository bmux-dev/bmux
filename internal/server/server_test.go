package server

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestSessionLifecycleAndTerminalState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New("/bin/sh")
	defer s.Close()
	dir := t.TempDir()
	r := s.request(protocol.Request{Op: "create", Name: "api", Dir: dir, Width: 40, Height: 6})
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	if r = s.request(protocol.Request{Op: "create", Name: "api", Dir: dir}); r.Error == "" {
		t.Fatal("duplicate accepted")
	}
	workspace := s.sessions["api"]
	item := workspace.panes[1]
	if err := item.input(protocol.Request{Op: "paste", Text: "printf '\\033[31mBMUX_READY\\033[0m\\n'\n"}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, item, func(f *protocol.TerminalFrame) bool {
		for _, row := range f.Rows {
			for _, cell := range row {
				if cell.Text == "B" && cell.FG != "" {
					return strings.Contains(frameText(f), "BMUX_READY")
				}
			}
		}
		return false
	})
	if err := item.input(protocol.Request{Op: "resize", Width: 60, Height: 8}); err != nil {
		t.Fatal(err)
	}
	f, _ := item.frame(0)
	if len(f.Rows) != 8 || len(f.Rows[0]) != 60 {
		t.Fatal("resize not preserved")
	}
	// Set terminal modes directly: shell readline may restore cursor visibility
	// at the prompt immediately after a printf, which isn't an emulator failure.
	item.mu.Lock()
	_, err := item.term.WriteString("\x1b[6 q\x1b[?25l\x1b[?1000h")
	item.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, item, func(f *protocol.TerminalFrame) bool {
		return f.CursorStyle == 2 && !f.CursorBlink && !f.CursorVisible && f.Mouse
	})
	if err := item.input(protocol.Request{Op: "paste", Text: "i=0; while [ $i -lt 30 ]; do echo history-$i; i=$((i+1)); done\n"}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, item, func(f *protocol.TerminalFrame) bool {
		return f.History >= 20 && strings.Contains(frameText(f), "history-29")
	})
	f, _ = item.frame(15)
	if f.Offset != 15 || f.CursorVisible || !strings.Contains(frameText(f), "history-") {
		t.Fatalf("scrollback: %+v", f)
	}
	if r = s.request(protocol.Request{Op: "rename", Name: "api", NewName: "backend"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if s.sessions["backend"] != workspace {
		t.Fatal("rename replaced session process")
	}
	if r = s.request(protocol.Request{Op: "list"}); len(r.Sessions) != 1 || r.Sessions[0].Name != "backend" {
		t.Fatalf("list: %+v", r)
	}
	if err := item.input(protocol.Request{Op: "paste", Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-item.done:
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not exit")
	}
	f, _ = item.frame(0)
	if !f.Exited {
		t.Fatal("missing exit status")
	}
	select {
	case <-workspace.done:
	case <-time.After(5 * time.Second):
		t.Fatal("last pane did not close session")
	}
	if r = s.request(protocol.Request{Op: "list"}); len(r.Sessions) != 0 {
		t.Fatal("exited session still listed")
	}
}

func TestDetachReattachAndProtocolVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Use TCP only as an in-process test transport to avoid Darwin's very short
	// Unix-socket path limit under Go's long per-test temporary directory paths.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- s.Serve(l) }()
	defer func() {
		s.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	if r := s.request(protocol.Request{Op: "create", Name: "persist", Dir: t.TempDir()}); r.Error != "" {
		t.Fatal(r.Error)
	}
	dial := func() net.Conn {
		c, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		return c
	}
	c := dial()
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	if err := enc.Encode(protocol.Request{Version: 999, Op: "ping"}); err != nil {
		t.Fatal(err)
	}
	var r protocol.Response
	if err := dec.Decode(&r); err != nil || r.Error == "" {
		t.Fatalf("version check: %+v %v", r, err)
	}
	c.Close()
	c = dial()
	enc, dec = json.NewEncoder(c), json.NewDecoder(c)
	r = protocol.Response{}
	if err := enc.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "persist", Width: 50, Height: 8}); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&r); err != nil || r.Error != "" {
		t.Fatalf("attach: %+v %v", r, err)
	}
	if err := enc.Encode(protocol.Request{Op: "paste", Text: "BMUX_TEST_VALUE=kept; printf 'SET_OK\\n'\n"}); err != nil {
		t.Fatal(err)
	}
	awaitResponse(t, dec, "SET_OK")
	if err := enc.Encode(protocol.Request{Op: "detach"}); err != nil {
		t.Fatal(err)
	}
	c.Close()
	c = dial()
	defer c.Close()
	enc, dec = json.NewEncoder(c), json.NewDecoder(c)
	r = protocol.Response{}
	if err := enc.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "persist", Width: 50, Height: 8}); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&r); err != nil || r.Error != "" {
		t.Fatalf("reattach: %+v %v", r, err)
	}
	if err := enc.Encode(protocol.Request{Op: "paste", Text: "printf 'VALUE=%s\\n' \"$BMUX_TEST_VALUE\"\n"}); err != nil {
		t.Fatal(err)
	}
	awaitResponse(t, dec, "VALUE=kept")
	if err := enc.Encode(protocol.Request{Op: "scroll", Delta: -100000}); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.Frame != nil {
		for _, p := range r.Frame.Panes {
			if p.Terminal.Offset < 0 {
				t.Fatal("negative offset")
			}
		}
	}
}

func awaitResponse(t *testing.T, dec *json.Decoder, text string) {
	t.Helper()
	for {
		var r protocol.Response
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Error != "" {
			t.Fatal(r.Error)
		}
		if r.Frame != nil {
			for _, p := range r.Frame.Panes {
				if strings.Contains(frameText(p.Terminal), text) {
					return
				}
			}
		}
	}
}
func awaitFrame(t *testing.T, s *pane, predicate func(*protocol.TerminalFrame) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f, _ := s.frame(0)
		if predicate(f) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f, _ := s.frame(0)
	t.Fatalf("terminal condition not met: %q", frameText(f))
}
func frameText(f *protocol.TerminalFrame) string {
	var text strings.Builder
	for _, row := range f.Rows {
		for _, c := range row {
			text.WriteString(c.Text)
		}
		text.WriteByte('\n')
	}
	return text.String()
}

func TestValidationAndEnvironment(t *testing.T) {
	for _, name := range []string{"", "has space", "escape\x1b", "slash/name", strings.Repeat("a", 81)} {
		if validateName(name) == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if err := validateName("hsdp-stl-api.2"); err != nil {
		t.Fatal(err)
	}
	env := shellEnv([]string{"TERM=old", "TMUX=old", "TMUX_PANE=%1", "HOME=" + os.TempDir(), "COLORTERM=old", "BMUX_SESSION=old", "BMUX_SESSION_ID=old", "BMUX_PANE=old"}, 3, "api", 7)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "=old") || strings.Contains(joined, "TMUX=") || !strings.Contains(joined, "BMUX_SESSION=api") || !strings.Contains(joined, "BMUX_SESSION_ID=3") || !strings.Contains(joined, "BMUX_PANE=7") {
		t.Fatalf("env: %s", joined)
	}
	w, h := dimensions(-2, 999999)
	if w != 2 || h != 200 {
		t.Fatalf("unsafe dimensions: %d,%d", w, h)
	}
}
