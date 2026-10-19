package client

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeClipboardUsesAttachedClientsTool(t *testing.T) {
	dir := t.TempDir()
	name := "pbcopy"
	if runtime.GOOS != "darwin" {
		name = "wl-copy"
		t.Setenv("WAYLAND_DISPLAY", "test")
	}
	destination := filepath.Join(dir, "clipboard")
	script := "#!/bin/sh\n/bin/cat > \"$BMUX_CLIPBOARD_TEST_FILE\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("BMUX_CLIPBOARD_TEST_FILE", destination)
	available, err := copyNative("hello 界\nsecond line")
	if !available || err != nil {
		t.Fatalf("copy: %v %v", available, err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello 界\nsecond line" {
		t.Fatalf("clipboard: %q", data)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho clipboard-denied >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	available, err = copyNative("failure")
	if !available || err == nil || !strings.Contains(err.Error(), "clipboard-denied") {
		t.Fatalf("failure was not reported: %v %v", available, err)
	}
}

func TestRemoteClipboardDoesNotWriteRemoteDesktop(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "remote client")
	available, err := copyNative("text")
	if available || err != nil {
		t.Fatalf("remote native access: %v %v", available, err)
	}
}

func TestAppClipboardWritesCopyQuietlyWithoutTakingOverCopyNotices(t *testing.T) {
	s := focusTestScreen(t, 40, 10)
	u := &ui{screen: s}
	u.notice("unrelated")
	// No helper available: the attached terminal gets OSC 52, with no notice.
	u.appCopy("yanked in nvim")
	if got := string(s.GetClipboardData()); got != "yanked in nvim" || u.message != "unrelated" {
		t.Fatalf("clipboard %q, notice %q", got, u.message)
	}
	// An app's copy finishing during a selection copy leaves that copy's notice.
	u.clipboardSeq = 1
	u.notice("Copying…")
	u.finishClipboard(clipboardResult{Available: true})
	if u.message != "Copying…" {
		t.Fatalf("app copy replaced the selection's notice: %q", u.message)
	}
	u.finishClipboard(clipboardResult{Seq: 1, Available: true})
	if u.message != "Copied to system clipboard" {
		t.Fatalf("selection copy notice: %q", u.message)
	}
	u.finishClipboard(clipboardResult{Err: os.ErrPermission})
	if !strings.Contains(u.message, "app's copy") {
		t.Fatalf("failed app copy was silent: %q", u.message)
	}
}

func TestPasteDoesNotInterpretbmuxBindings(t *testing.T) {
	u, requests := navigationUI(t)
	// Ctrl-a d inside pasted text must never detach the client.
	u.handleRawPaste(&rawPasteEvent{Text: "\x01d\n"})
	r := nextNavigationRequest(t, requests, "paste", "")
	if u.quitting || u.prefix || r.Text != "\x01d\n" || r.PaneID != 1 {
		t.Fatalf("paste interpreted as input: %+v", r)
	}
}
