package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func noEnv(string) string { return "" }

func TestMissingFileGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"), noEnv)
	if err != nil || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("%+v %v", cfg, err)
	}
}

// The documented file must match the real defaults once uncommented.
func TestDefaultFileDocumentsTheDefaults(t *testing.T) {
	setting := regexp.MustCompile(`(?m)^# ([a-z_]+ = .*)$`)
	uncommented := setting.ReplaceAllString(DefaultFile, "$1")
	if uncommented == DefaultFile {
		t.Fatal("no settings found")
	}
	cfg, err := Load(write(t, uncommented), noEnv)
	want := Default()
	want.Agents.Extra = []string{}
	want.Restore.Resume = map[string][]string{"claude": {"claude", "--continue"}, "codex": {"codex", "resume", "--last"}}
	if err != nil || !reflect.DeepEqual(cfg, want) {
		t.Fatalf("documented defaults:\n got %+v\nwant %+v\n%v", cfg, want, err)
	}
}

func TestSettingsAndEnvironmentOverrides(t *testing.T) {
	path := write(t, `prefix = "Ctrl-B"
[theme]
name = "light"
accent = "#AABBCC"
[theme.agents]
waiting = "#00ff00"
[sidebar]
show = false
width = 30
min_screen_width = 100
[sound]
ready = "~/sounds/done.aiff"
[agents]
extra = ["myagent"]
working_after = "3s"
waiting_after = "1500ms"
[restore]
enabled = false
[restore.resume]
claude = ["claude", "--continue", "--model", "opus"]
codex = []
`)
	cfg, err := Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if cfg.Prefix != "Ctrl-B" || cfg.Theme.Name != "light" || cfg.Theme.Accent != "#aabbcc" || cfg.Theme.Agents.Waiting != "#00ff00" ||
		cfg.Sidebar != (Sidebar{Show: false, Width: 30, MinScreenWidth: 100}) || cfg.Sound.Ready != filepath.Join(home, "sounds/done.aiff") ||
		!reflect.DeepEqual(cfg.Agents.Extra, []string{"myagent"}) || cfg.Agents.WorkingAfter.Duration != 3*time.Second ||
		cfg.Agents.WaitingAfter.Duration != 1500*time.Millisecond || cfg.Restore.Enabled ||
		len(cfg.Restore.Resume["codex"]) != 0 || cfg.Restore.Resume["claude"][3] != "opus" {
		t.Fatalf("settings: %+v", cfg)
	}
	if key, _ := PrefixKey(cfg.Prefix); key != 'b' {
		t.Fatalf("prefix key %q", key)
	}
	env := map[string]string{"BMUX_THEME": "dark", "BMUX_ACCENT": "green", "BMUX_SOUND": "0", "BMUX_POWERLINE_ASCII": "1"}
	cfg, err = Load(path, func(k string) string { return env[k] })
	if err != nil || cfg.Theme.Name != "dark" || cfg.Theme.Accent != "green" || cfg.Sound.Enabled || !cfg.Theme.PowerlineASCII {
		t.Fatalf("environment overrides: %+v %v", cfg, err)
	}
}

func TestInvalidSettingsKeepDefaultsAndAreAllReported(t *testing.T) {
	cfg, err := Load(write(t, `prefix = "ctrl-h"
colour = "red"
[theme]
name = "neon"
accent = "pink"
[sidebar]
width = 5
[sound]
ready = "relative.aiff"
[agents]
extra = ["", "/usr/bin/agent", "ok"]
waiting_after = "10ms"
[restore.resume]
claude = ["/usr/local/bin/claude"]
`), noEnv)
	if err == nil {
		t.Fatal("invalid settings accepted silently")
	}
	for _, want := range []string{"prefix", `unknown setting "colour"`, "theme.name", "theme.accent", "sidebar.width", "sound file", "agents.extra", "agents.waiting_after", "restore.resume.claude"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error doesn't mention %s: %v", want, err)
		}
	}
	d := Default()
	if cfg.Prefix != d.Prefix || cfg.Theme.Name != d.Theme.Name || cfg.Theme.Accent != d.Theme.Accent || cfg.Sidebar.Width != d.Sidebar.Width ||
		cfg.Sound.Ready != "" || !reflect.DeepEqual(cfg.Agents.Extra, []string{"ok"}) || cfg.Agents.WaitingAfter != d.Agents.WaitingAfter ||
		cfg.Restore.Resume["claude"] != nil {
		t.Fatalf("invalid settings not reset: %+v", cfg)
	}
	if _, err := Load(write(t, "prefix = \n"), noEnv); err == nil {
		t.Fatal("malformed TOML accepted")
	}
}

func TestPrefixKeyForms(t *testing.T) {
	for in, want := range map[string]rune{"ctrl-a": 'a', "Ctrl+B": 'b', "C-z": 'z'} {
		if got, err := PrefixKey(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"ctrl-j", "ctrl-m", "alt-a", "ctrl-ab", "a", ""} {
		if _, err := PrefixKey(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}
