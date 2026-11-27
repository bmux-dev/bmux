package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateSkipsRuntimePrepareBeforeRunning(t *testing.T) {
	c, ok := findCommand("update")
	if !ok || c.server != noSocket {
		t.Fatalf("update server mode = %v, ok=%v; want noSocket", c.server, ok)
	}
	badRuntime := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badRuntime, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMUX_RUNTIME_DIR", badRuntime)
	err := run([]string{"update", "--check"})
	if err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("update --check with invalid runtime dir failed before runUpdate: %v", err)
	}
}

func TestUpdateRefusesBinariesItShouldntReplace(t *testing.T) {
	// Tests run as a checkout build, which bmux update leaves alone, before
	// any network request.
	if err := runUpdate("", nil); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("dev build: %v", err)
	}
	if err := runUpdate("", []string{"now"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("extra argument: %v", err)
	}
	if err := runUpdate("", []string{"--beta", "--stable"}); err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("--beta --stable: %v", err)
	}
	if err := runUpdate("", []string{"--beta"}); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("--beta on dev build: %v", err)
	}

	ok := func(string) error { return nil }
	denied := func(string) error { return errors.New("permission denied") }
	for _, c := range []struct {
		exe      string
		writable func(string) error
		want     string
	}{
		{"/home/me/.local/bin/bmux", ok, ""},
		{"/opt/homebrew/Cellar/bmux/0.6.0/bin/bmux", ok, "brew upgrade"},
		{"/home/linuxbrew/.linuxbrew/Cellar/bmux/0.6.0/bin/bmux", ok, "brew upgrade"},
		{"/opt/homebrew/Caskroom/bmux/0.7.6/bmux", ok, "brew upgrade"},
		{"/home/linuxbrew/.linuxbrew/Caskroom/bmux/0.7.6/bmux", ok, "brew upgrade"},
		{"/usr/local/bin/bmux", denied, "BMUX_INSTALL_DIR"},
	} {
		err := replaceable(c.exe, c.writable)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", c.exe, err, c.want)
		}
	}
}
