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

func TestPasteDoesNotInterpretbmuxBindings(t *testing.T) {
	u, requests := navigationUI(t)
	// Ctrl-a d inside pasted text must never detach the client.
	u.handleRawPaste(&rawPasteEvent{Text: "\x01d\n"})
	r := nextNavigationRequest(t, requests, "paste", "")
	if u.quitting || u.prefix || r.Text != "\x01d\n" || r.PaneID != 1 {
		t.Fatalf("paste interpreted as input: %+v", r)
	}
}
