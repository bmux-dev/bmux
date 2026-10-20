package main

import (
	"errors"
	"strings"
	"testing"
)

func TestUpdateRefusesBinariesItShouldntReplace(t *testing.T) {
	// Tests run as a checkout build, which bmux update leaves alone, before
	// any network request.
	if err := runUpdate("", nil); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("dev build: %v", err)
	}
	if err := runUpdate("", []string{"now"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("extra argument: %v", err)
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
		{"/usr/local/bin/bmux", denied, "BMUX_INSTALL_DIR"},
	} {
		err := replaceable(c.exe, c.writable)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", c.exe, err, c.want)
		}
	}
}
