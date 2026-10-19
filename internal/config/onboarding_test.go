package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkOnboardedKeepsTheRestOfTheFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "bmux", "config.toml")
	if err := MarkOnboarded(missing); err != nil {
		t.Fatal(err)
	}
	if cfg, err := Load(missing, noEnv); err != nil || cfg.Onboarding {
		t.Fatalf("new file: onboarding=%v %v", cfg.Onboarding, err)
	}

	user := "# my settings\ntheme = \"light\"\n\n[sidebar]\nwidth = 30\nonboarding = true\n"
	path := write(t, user)
	if err := MarkOnboarded(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if rest, ok := strings.CutPrefix(string(data), onboardedLine); !ok || rest != user {
		t.Fatalf("file not kept as written:\n%s", data)
	}

	// A top-level onboarding line is replaced, not duplicated.
	path = write(t, "prefix = \"C-b\"\nonboarding = true # show it\n[ui]\naccent = \"green\"\n")
	if err := MarkOnboarded(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "prefix = \"C-b\"\n"+onboardedLine+"[ui]\naccent = \"green\"\n" {
		t.Fatalf("existing line not replaced:\n%s", data)
	}
	cfg, err := Load(path, noEnv)
	if err != nil || cfg.Onboarding || cfg.Prefix != "C-b" || cfg.UI.Accent != "green" {
		t.Fatalf("after marking: %+v %v", cfg, err)
	}

	// A file that doesn't parse is left alone.
	broken := "theme = \n"
	path = write(t, broken)
	if err := MarkOnboarded(path); err == nil {
		t.Fatal("rewrote a file that doesn't parse")
	}
	if data, _ := os.ReadFile(path); string(data) != broken {
		t.Fatalf("broken file changed: %q", data)
	}
}
