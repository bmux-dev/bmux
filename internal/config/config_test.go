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
	setting := regexp.MustCompile(`(?m)^# ([a-z-]+ = .*)$`)
	uncommented := setting.ReplaceAllString(DefaultFile, "$1")
	if uncommented == DefaultFile {
		t.Fatal("no settings found")
	}
	cfg, err := Load(write(t, uncommented), noEnv)
	want := Default()
	want.Agents.Extra = []string{}
	want.Keys = Keys{PassThrough: []string{}, Root: map[string]string{}, Prefix: map[string]string{}}
	want.Restore.Resume = map[string][]string{"claude": {"claude", "--continue"}, "codex": {"codex", "resume", "--last"}}
	if err != nil || !reflect.DeepEqual(cfg, want) {
		t.Fatalf("documented defaults:\n got %+v\nwant %+v\n%v", cfg, want, err)
	}
	// Every documented key binding is a real default.
	binding := regexp.MustCompile(`(?m)^# ("[^"]+" = "[a-z-]+")`)
	cfg, err = Load(write(t, binding.ReplaceAllString(DefaultFile, "$1")), noEnv)
	if err != nil || len(cfg.Keys.Root) != len(DefaultRootKeys) || len(cfg.Keys.Prefix) == 0 {
		t.Fatalf("documented keys: %+v %v", cfg.Keys, err)
	}
	for table, documented := range map[string]map[string]string{"root": cfg.Keys.Root, "prefix": cfg.Keys.Prefix} {
		defaults := map[string]map[string]string{"root": DefaultRootKeys, "prefix": DefaultPrefixKeys}[table]
		for key, action := range documented {
			if defaults[key] != action {
				t.Errorf("keys.%s documents %s = %s, default is %q", table, key, action, defaults[key])
			}
		}
	}
}

func TestSettingsAndEnvironmentOverrides(t *testing.T) {
	path := write(t, `theme = "light"
prefix = "C-b"

[ui]
accent = "#AABBCC"

[ui.agent-colors]
waiting = "#00ff00"

[sidebar]
show = false
width = 30
min-screen-width = 100

[sound]
ready = "~/sounds/done.aiff"

[agents]
extra = ["myagent"]
working-after = "3s"
waiting-after = "1500ms"

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
	if cfg.Prefix != "C-b" || cfg.Theme != "light" || cfg.UI.Accent != "#aabbcc" || cfg.UI.AgentColors.Waiting != "#00ff00" ||
		cfg.Sidebar != (Sidebar{Show: false, Width: 30, MinScreenWidth: 100}) || cfg.Sound.Ready != filepath.Join(home, "sounds/done.aiff") ||
		!reflect.DeepEqual(cfg.Agents.Extra, []string{"myagent"}) || cfg.Agents.WorkingAfter.Duration != 3*time.Second ||
		cfg.Agents.WaitingAfter.Duration != 1500*time.Millisecond || cfg.Restore.Enabled ||
		len(cfg.Restore.Resume["codex"]) != 0 || cfg.Restore.Resume["claude"][3] != "opus" {
		t.Fatalf("settings: %+v", cfg)
	}
	if key, _ := PrefixKey(cfg.Prefix); key != 'b' {
		t.Fatalf("prefix key %q", key)
	}
	env := map[string]string{"BMUX_THEME": "dark", "BMUX_ACCENT": "green", "BMUX_SOUND": "0", "BMUX_POWERLINE_ASCII": "1", "BMUX_NO_UPDATE_CHECK": "1"}
	cfg, err = Load(path, func(k string) string { return env[k] })
	if err != nil || cfg.Theme != "dark" || cfg.UI.Accent != "green" || cfg.Sound.Enabled || !cfg.UI.PowerlineASCII || cfg.Update.Check {
		t.Fatalf("environment overrides: %+v %v", cfg, err)
	}
}

func TestInvalidSettingsKeepDefaultsAndAreAllReported(t *testing.T) {
	cfg, err := Load(write(t, `prefix = "C-h"
colour = "red"
theme = "neon"
[ui]
accent = "pink"
graphics = "sixel"
[sidebar]
width = 5
[sound]
ready = "relative.aiff"
[agents]
extra = ["", "/usr/bin/agent", "ok"]
waiting_after = "10ms"
waiting-after = "10ms"
[restore.resume]
claude = ["/usr/local/bin/claude"]
`), noEnv)
	if err == nil {
		t.Fatal("invalid settings accepted silently")
	}
	for _, want := range []string{"prefix", `unknown setting "colour"`, `unknown setting "agents.waiting_after"`, `theme "neon"`, "ui.accent", "ui.graphics", "sidebar.width", "sound file", "agents.extra", "agents.waiting-after", "restore.resume.claude"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error doesn't mention %s: %v", want, err)
		}
	}
	d := Default()
	if cfg.Prefix != d.Prefix || cfg.Theme != d.Theme || cfg.UI.Accent != d.UI.Accent || cfg.UI.Graphics != d.UI.Graphics || cfg.Sidebar.Width != d.Sidebar.Width ||
		cfg.Sound.Ready != "" || !reflect.DeepEqual(cfg.Agents.Extra, []string{"ok"}) || cfg.Agents.WaitingAfter != d.Agents.WaitingAfter ||
		cfg.Restore.Resume["claude"] != nil {
		t.Fatalf("invalid settings not reset: %+v", cfg)
	}
	if _, err := Load(write(t, "prefix = \n"), noEnv); err == nil {
		t.Fatal("malformed TOML accepted")
	}
}

func TestGraphicsSettingIsCaseInsensitive(t *testing.T) {
	cfg, err := Load(write(t, "[ui]\ngraphics = \"Off\"\n"), noEnv)
	if err != nil || cfg.UI.Graphics != "off" {
		t.Fatalf("graphics = %q, %v", cfg.UI.Graphics, err)
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
