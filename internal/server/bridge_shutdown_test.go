package server

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/josephferrero/bmux/internal/protocol"
	"golang.org/x/sys/unix"
)

func bounded(t *testing.T, limit time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("operation exceeded %s watchdog", limit)
	}
}

type gatedWriter struct {
	gate chan struct{}
	mu   sync.Mutex
	data bytes.Buffer
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	<-w.gate
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.data.Write(p)
}

func TestBridgeWholeAdmissionLosslessAndReplyOverflow(t *testing.T) {
	term := vt.NewEmulator(80, 24)
	w := &gatedWriter{gate: make(chan struct{})}
	b := newInputBridge(term, w)
	defer func() { b.close(io.ErrClosedPipe); _ = term.InputPipe().(io.Closer).Close() }()
	var release sync.Once
	defer release.Do(func() { close(w.gate) })
	if _, err := term.Write([]byte("\x1b[?2004h")); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("a", protocol.MaxPasteBytes)
	expected := []byte("\x1b[200~" + payload + "\x1b[201~")
	if len(expected) != MaxPendingInputBytes {
		t.Fatalf("encoded maximum = %d, queue budget = %d", len(expected), MaxPendingInputBytes)
	}
	bounded(t, 2*time.Second, func() {
		if err := b.beginUserInput(MaxPendingInputBytes); err != nil {
			t.Error(err)
			return
		}
		term.Paste(payload)
		if err := b.finishUserInput(term.InputPipe()); err != nil {
			t.Error(err)
		}
	})
	b.mu.Lock()
	if b.pendingUser != MaxPendingInputBytes || b.remainingReservation != 0 || b.admittingUserInput {
		t.Errorf("incorrect maximum admission accounting: pending=%d remaining=%d admitting=%v", b.pendingUser, b.remainingReservation, b.admittingUserInput)
	}
	b.mu.Unlock()
	if err := b.beginUserInput(1); err == nil {
		t.Fatal("saturated queue accepted another request")
	}
	b.mu.Lock()
	if b.pendingUser != MaxPendingInputBytes || b.remainingReservation != 0 || b.admittingUserInput {
		t.Error("rejected intent changed admission/accounting")
	}
	b.mu.Unlock()
	bounded(t, 3*time.Second, func() {
		_, _ = term.Write([]byte(strings.Repeat("\x1b[6n", 20000)))
		if err := b.fenceGeneratedInput(term.InputPipe()); err != nil {
			t.Error(err)
		}
	})
	b.mu.Lock()
	if b.droppedReplies == 0 || b.pendingReply > maxPendingReplyBytes {
		t.Error("reply flood did not obey bounded overflow policy")
	}
	if accounted := uint64(b.pendingReply) + b.droppedReplies; accounted != uint64(20000*len("\x1b[1;1R")) {
		t.Errorf("reply flood accounted for %d bytes, want %d", accounted, 20000*len("\x1b[1;1R"))
	}
	b.mu.Unlock()
	// Release the independent writer and verify the accepted payload is intact.
	release.Do(func() { close(w.gate) })
	deadline := time.Now().Add(3 * time.Second)
	for {
		w.mu.Lock()
		data := append([]byte(nil), w.data.Bytes()...)
		w.mu.Unlock()
		if len(data) >= len(expected) {
			if !bytes.Equal(data[:len(expected)], expected) {
				t.Fatal("accepted user bytes changed or interleaved")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("accepted bytes never delivered after writer resumed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	awaitBridgeAccounting(t, b, 0, 0)
	bounded(t, time.Second, func() {
		if err := b.beginUserInput(16); err != nil {
			t.Error("admission did not resume:", err)
			return
		}
		_, _ = term.InputPipe().Write([]byte("resumed"))
		if err := b.finishUserInput(term.InputPipe()); err != nil {
			t.Error(err)
		}
	})
	awaitBridgeAccounting(t, b, 0, 0)
	w.mu.Lock()
	defer w.mu.Unlock()
	if !bytes.HasSuffix(w.data.Bytes(), []byte("resumed")) {
		t.Fatal("resumed user bytes were not delivered")
	}
}

func scriptedPane(t *testing.T, body string) (*pane, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	p, err := newPane("regression", dir, path, 80, 24, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Independent cleanup does not depend on the state locks being tested.
	t.Cleanup(func() { p.owner.signal(unix.SIGKILL); p.closeIO(); _ = p.cmd.Process.Kill() })
	return p, dir
}

func awaitFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing readiness file %s", path)
	return ""
}

func TestPaneBackpressureAndDSRFloodRemainResponsive(t *testing.T) {
	for _, mode := range []string{"cat", "sleep", "dsr"} {
		t.Run(mode, func(t *testing.T) {
			body := "stty raw -echo\necho ready > ready\n"
			switch mode {
			case "cat":
				body += "exec cat"
			case "sleep":
				body += "exec sleep 60"
			case "dsr":
				body += "while :; do printf '\033[6n\033[6n\033[6n\033[6n\033[6n\033[6n\033[6n\033[6n'; done"
			}
			p, dir := scriptedPane(t, body)
			awaitFile(t, filepath.Join(dir, "ready"))
			if mode != "dsr" {
				bounded(t, time.Second, func() {
					if err := p.input(protocol.Request{Op: "paste", Text: strings.Repeat("x", 1<<20)}); err != nil {
						t.Error(err)
					}
				})
			} else {
				deadline := time.Now().Add(5 * time.Second)
				for {
					p.bridge.mu.Lock()
					dropped := p.bridge.droppedReplies
					p.bridge.mu.Unlock()
					if dropped > 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("DSR replies never saturated")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			bounded(t, 500*time.Millisecond, func() {
				p.frame(0)
				p.preview(20, 5)
				_ = p.input(protocol.Request{Op: "resize", Width: 81, Height: 25})
			})
			conn, err := p.pty.SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			var flags int
			if err := conn.Control(func(fd uintptr) { flags, err = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
				t.Fatal(err)
			}
			if err != nil || flags&unix.O_NONBLOCK == 0 {
				t.Fatalf("resize disabled nonblocking PTY integration: flags=%x err=%v", flags, err)
			}
			bounded(t, 2*time.Second, p.stop)
			for _, done := range []chan struct{}{p.bridge.drainDone, p.bridge.writerDone} {
				select {
				case <-done:
				case <-time.After(500 * time.Millisecond):
					t.Fatal("PTY IO goroutine failed to cancel")
				}
			}
		})
	}
}

func TestShutdownHUPTrapJobGroupsAndUnrelatedControl(t *testing.T) {
	control := exec.Command("sleep", "60")
	if err := control.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = control.Process.Kill(); _ = control.Wait() }()
	p, dir := scriptedPane(t, "set -m\ntrap 'echo hup > hup; exit 0' HUP\n/bin/sh -c 'trap \"\" HUP TERM; echo $$ > child; exec sleep 60' &\necho $! > background\nwhile [ ! -s child ]; do sleep 0.01; done\nkill -STOP $!\necho ready > ready\nwhile :; do sleep 1; done")
	awaitFile(t, filepath.Join(dir, "ready"))
	pid, err := strconv.Atoi(awaitFile(t, filepath.Join(dir, "child")))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := inspectProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid, err := unix.Getpgid(pid); err != nil || pgid == p.owner.sid {
		t.Fatalf("fixture did not establish separate job-control group: %d %v", pgid, err)
	}
	t.Cleanup(func() {
		if current, err := inspectProcess(pid); err == nil && current.birth == identity.birth {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	bounded(t, 2*time.Second, p.stop)
	if data, err := os.ReadFile(filepath.Join(dir, "hup")); err != nil || strings.TrimSpace(string(data)) != "hup" {
		t.Fatalf("shell HUP trap did not run: %s %v", data, err)
	}
	if current, err := inspectProcess(pid); err == nil && current.birth == identity.birth && !current.zombie {
		t.Fatal("stopped HUP/TERM-ignoring job survived shutdown")
	}
	if err := unix.Kill(control.Process.Pid, 0); err != nil {
		t.Fatalf("unrelated control process was signalled: %v", err)
	}
}

func TestDetachedShutdownHelper(t *testing.T) {
	if os.Getenv("BMUX_TEST_DETACHED") != "1" {
		return
	}
	if _, err := unix.Setsid(); err != nil {
		os.Exit(2)
	}
	signal.Ignore(unix.SIGHUP, unix.SIGTERM)
	if err := os.WriteFile("detached", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(3)
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestShutdownExcludesDeliberatelyDetachedProcess(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, dir := scriptedPane(t, "BMUX_TEST_DETACHED=1 "+strconv.Quote(binary)+" -test.run '^TestDetachedShutdownHelper$' &\nwhile [ ! -s detached ]; do sleep 0.01; done\necho ready > ready\nwhile :; do sleep 1; done")
	awaitFile(t, filepath.Join(dir, "ready"))
	pid, err := strconv.Atoi(awaitFile(t, filepath.Join(dir, "detached")))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := inspectProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if current, err := inspectProcess(pid); err == nil && current.birth == identity.birth {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	if identity.sid == p.owner.sid {
		t.Fatal("fixture did not detach")
	}
	bounded(t, 2*time.Second, p.stop)
	if current, err := inspectProcess(pid); err != nil || current.birth != identity.birth || current.zombie {
		t.Fatalf("deliberately detached process was terminated: %v", err)
	}
}

func TestSessionShutdownConcurrentAndIndependentOfStateLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("many", t.TempDir(), "/bin/sh", 240, 80)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.shutdownMu.Lock()
		defer s.shutdownMu.Unlock()
		for p := range s.owned {
			p.owner.signal(unix.SIGKILL)
			p.closeIO()
		}
	})
	for i := range 7 {
		direction := "horizontal"
		if i%2 == 1 {
			direction = "vertical"
		}
		if err := s.input(protocol.Request{Op: "split", Direction: direction}); err != nil {
			t.Fatal(err)
		}
	}
	s.shutdownMu.Lock()
	items := make([]*pane, 0, len(s.owned))
	for p := range s.owned {
		items = append(items, p)
	}
	s.shutdownMu.Unlock()
	for _, p := range items {
		if err := p.input(protocol.Request{Op: "paste", Text: "trap '' HUP TERM; echo ready > ready-$$; sleep 60\n"}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	// Deliberately hold layout and terminal locks: IO cancellation must not
	// queue behind either. Watchers may finish after these locks are released.
	s.mu.Lock()
	defer s.mu.Unlock()
	items[0].mu.Lock()
	defer items[0].mu.Unlock()
	bounded(t, 2*time.Second, s.stop)
	for _, p := range items {
		select {
		case <-p.bridge.done:
		default:
			t.Error("pane IO not cancelled")
		}
	}
}
