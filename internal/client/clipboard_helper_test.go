package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func clipboardStub(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	name := "pbcopy"
	if runtime.GOOS != "darwin" {
		name = "wl-copy"
		t.Setenv("WAYLAND_DISPLAY", "test")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	return path
}

func TestClipboardHelperRetainedDescriptorsDoNotKillOwnerOrDelayCopy(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "clipboard")
	alive := filepath.Join(dir, "owner-alive")
	pidfile := filepath.Join(dir, "owner-pid")
	t.Setenv("BMUX_CLIPBOARD_TEST_FILE", destination)
	t.Setenv("BMUX_CLIPBOARD_ALIVE_FILE", alive)
	t.Setenv("BMUX_CLIPBOARD_PID_FILE", pidfile)
	path := clipboardStub(t, "/bin/cat > \"$BMUX_CLIPBOARD_TEST_FILE\"\n/bin/sh -c '/bin/sleep 4; echo alive > \"$BMUX_CLIPBOARD_ALIVE_FILE\"; exec /bin/sleep 30' &\necho $! > \"$BMUX_CLIPBOARD_PID_FILE\"\nexit 0\n")
	defer func() {
		data, _ := os.ReadFile(pidfile)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			process, _ := os.FindProcess(pid)
			_ = process.Kill()
		}
	}()
	start := time.Now()
	if err := runClipboardHelper(context.Background(), path, nil, fidelityPaste, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("waited for inherited descriptors: %s", elapsed)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != fidelityPaste {
		t.Fatalf("fake clipboard data: %q %v", data, err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for {
		if data, err := os.ReadFile(alive); err == nil && string(data) == "alive\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detached clipboard owner did not survive copy return")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClipboardHelperTimeoutAndBoundedDiagnostics(t *testing.T) {
	// exec makes the sleeping process the directly owned helper, not an orphan.
	path := clipboardStub(t, "exec /bin/sleep 10\n")
	start := time.Now()
	err := runClipboardHelper(context.Background(), path, nil, "text", 40*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("helper timeout: %v after %s", err, time.Since(start))
	}
	var output cappedClipboardOutput
	for i := 0; i < 100; i++ {
		if n, err := output.Write([]byte(strings.Repeat("x", 4096))); n != 4096 || err != nil {
			t.Fatalf("drain: %d %v", n, err)
		}
	}
	if len(output.data) != clipboardDiagnosticBytes || !strings.Contains(output.String(), "truncated") {
		t.Fatalf("unbounded diagnostics: %d %v", len(output.data), output.truncated)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+strings.Repeat("x", 2*clipboardDiagnosticBytes)+" >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	err = runClipboardHelper(context.Background(), path, nil, "", 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "diagnostics truncated") || len(err.Error()) > clipboardDiagnosticBytes+len(path)+100 {
		t.Fatalf("helper error diagnostics were not capped: %v", err)
	}
}

func TestClipboardWorkerIsAsyncSerializedAndCapturesCopyIntent(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "clipboard")
	t.Setenv("BMUX_CLIPBOARD_TEST_FILE", destination)
	clipboardStub(t, "/bin/sleep 0.1\n/bin/cat >> \"$BMUX_CLIPBOARD_TEST_FILE\"\n")
	w := newClipboardWorker()
	defer w.Close()
	start := time.Now()
	for i, text := range []string{"first\n", "second\n", "third\n"} {
		if err := w.Submit(uint64(i+1), text); err != nil {
			t.Fatal(err)
		}
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("submitting a copy blocked on the helper")
	}
	for seq := uint64(1); seq <= 3; seq++ {
		select {
		case result := <-w.Results:
			if result.Seq != seq || result.Err != nil || !result.Available {
				t.Fatalf("unordered/failed result: %+v", result)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("missing clipboard result")
		}
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "first\nsecond\nthird\n" {
		t.Fatalf("copy order: %q %v", data, err)
	}
}

func TestClipboardWorkerSSHFallbackAndBoundedAdmission(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "remote")
	w := newClipboardWorker()
	if err := w.Submit(7, "captured 界"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-w.Results:
		if result.Available || result.Err != nil || result.Text != "captured 界" || result.Seq != 7 {
			t.Fatalf("OSC 52 fallback: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("missing OSC 52 fallback")
	}
	if err := w.Submit(8, strings.Repeat("x", (32<<20)+1)); err == nil {
		t.Fatal("oversized clipboard job admitted")
	}
	w.Close()
	if err := w.Submit(9, "late"); err == nil {
		t.Fatal("copy admitted after closing client")
	}
	select {
	case _, ok := <-w.Results:
		if ok {
			t.Fatal("unexpected result after shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("clipboard worker did not stop")
	}
}
