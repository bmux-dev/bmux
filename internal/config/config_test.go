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
	want.Hooks = map[string][]string{"agent-blocked": {"notify-send 'bmux' '#{session_name}: #{pane_agent} needs you'"}}
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

[projects]
start-dir = "~/work"
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
		len(cfg.Restore.Resume["codex"]) != 0 || cfg.Restore.Resume["claude"][3] != "opus" || cfg.Projects.StartDir != "~/work" {
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
[projects]
start-dir = "work"
`), noEnv)
	if err == nil {
		t.Fatal("invalid settings accepted silently")
	}
	for _, want := range []string{"prefix", `unknown setting "colour"`, `unknown setting "agents.waiting_after"`, `theme "neon"`, "ui.accent", "ui.graphics", "sidebar.width", "sound file", "agents.extra", "agents.waiting-after", "restore.resume.claude", "projects.start-dir"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error doesn't mention %s: %v", want, err)
		}
	}
	d := Default()
	if cfg.Prefix != d.Prefix || cfg.Theme != d.Theme || cfg.UI.Accent != d.UI.Accent || cfg.UI.Graphics != d.UI.Graphics || cfg.Sidebar.Width != d.Sidebar.Width ||
		cfg.Sound.Ready != "" || !reflect.DeepEqual(cfg.Agents.Extra, []string{"ok"}) || cfg.Agents.WaitingAfter != d.Agents.WaitingAfter ||
		cfg.Restore.Resume["claude"] != nil || cfg.Projects.StartDir != d.Projects.StartDir {
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

func TestHooksNameKnownEventsAndRealCommands(t *testing.T) {
	cfg, err := Load(write(t, `[hooks]
pane-exited = ["echo #{pane_target} >> /tmp/exits"]
agent-blocked = ["notify-send blocked", "echo second"]
on-boot = ["echo nope"]
client-attached = ["  "]
`), noEnv)
	if err == nil || !strings.Contains(err.Error(), "hooks.on-boot isn't an event") || !strings.Contains(err.Error(), "hooks.client-attached has an empty command") {
		t.Fatalf("problems: %v", err)
	}
	want := map[string][]string{"pane-exited": {"echo #{pane_target} >> /tmp/exits"}, "agent-blocked": {"notify-send blocked", "echo second"}}
	if !reflect.DeepEqual(cfg.Hooks, want) {
		t.Fatalf("hooks: %+v", cfg.Hooks)
	}
}

func TestResolveTheme(t *testing.T) {
	for name, want := range map[string]string{
		"dark": "dark", "light": "light",
		"catppuccin": "catppuccin-mocha", "tokyonight": "tokyonight-night", "gruvbox": "gruvbox-dark",
		"kanagawa": "kanagawa-wave", "everforest": "everforest-dark", "solarized": "solarized-dark",
		"nord": "nord", "onedark": "onedark", "dracula": "dracula", "rose-pine": "rose-pine",
		"catppuccin-latte": "catppuccin-latte", "Tokyonight-Storm": "tokyonight-storm", "  Nord ": "nord",
	} {
		if got, ok := ResolveTheme(name); !ok || got != want {
			t.Errorf("ResolveTheme(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "neon", "catppuccin-", "catppuccin-dawn", "rose", "solarized-mocha"} {
		if got, ok := ResolveTheme(name); ok {
			t.Errorf("ResolveTheme(%q) = %q; want no match", name, got)
		}
	}
	for _, name := range ThemeNames {
		if got, ok := ResolveTheme(name); !ok || got != name {
			t.Errorf("listed theme %q resolves to %q, %v", name, got, ok)
		}
	}
}

func TestNamedThemesFromFileAndEnvironment(t *testing.T) {
	cfg, err := Load(write(t, "theme = \"Catppuccin\"\n"), noEnv)
	if err != nil || cfg.Theme != "catppuccin-mocha" {
		t.Fatalf("family name in the file: %q %v", cfg.Theme, err)
	}
	// The environment overrides the file and accepts the same names.
	env := func(k string) string { return map[string]string{"BMUX_THEME": "gruvbox-light"}[k] }
	if cfg, err = Load(write(t, "theme = \"nord\"\n"), env); err != nil || cfg.Theme != "gruvbox-light" {
		t.Fatalf("environment name: %q %v", cfg.Theme, err)
	}
	env = func(k string) string { return map[string]string{"BMUX_THEME": "Solarized"}[k] }
	if cfg, err = Load(write(t, ""), env); err != nil || cfg.Theme != "solarized-dark" {
		t.Fatalf("environment family: %q %v", cfg.Theme, err)
	}
	// An unknown name, from either source, is reported and falls back to dark.
	for name, env := range map[string]func(string) string{
		"file": noEnv,
		"env":  func(k string) string { return map[string]string{"BMUX_THEME": "neon"}[k] },
	} {
		content := ""
		if name == "file" {
			content = "theme = \"neon\"\n"
		}
		cfg, err := Load(write(t, content), env)
		if err == nil || !strings.Contains(err.Error(), `theme "neon"`) || cfg.Theme != "dark" {
			t.Errorf("unknown theme from %s: %q %v", name, cfg.Theme, err)
		}
	}
}

func TestAccentDefaultsToTheThemeAndCyanIsItsOwnChoice(t *testing.T) {
	if got := Default().UI.Accent; got != "theme" {
		t.Fatalf("default accent %q", got)
	}
	for _, accent := range []string{"theme", "Cyan", "amber", "green", "#AABBCC"} {
		cfg, err := Load(write(t, "[ui]\naccent = \""+accent+"\"\n"), noEnv)
		if err != nil || cfg.UI.Accent != strings.ToLower(accent) {
			t.Errorf("accent %q: %q %v", accent, cfg.UI.Accent, err)
		}
	}
	cfg, err := Load(write(t, "[ui]\naccent = \"pink\"\n"), noEnv)
	if err == nil || !strings.Contains(err.Error(), "theme, cyan, amber, green") || cfg.UI.Accent != "theme" {
		t.Fatalf("bad accent: %q %v", cfg.UI.Accent, err)
	}
}

func TestPaneColorsFollowThemeAndOptions(t *testing.T) {
	cfg := Default()
	if _, fg, bg := cfg.PaneColors(); fg || bg {
		t.Fatalf("dark themes panes: fg %v bg %v", fg, bg)
	}
	cfg.Theme = "catppuccin-mocha"
	if p, fg, bg := cfg.PaneColors(); !fg || !bg || p.Foreground != 0xcdd6f4 || p.Background != 0x1e1e2e {
		t.Fatalf("named theme: %+v fg %v bg %v", p, fg, bg)
	}
	cfg.UI.PaneBackground = "terminal"
	if p, fg, bg := cfg.PaneColors(); !fg || bg || p.Foreground != 0xcdd6f4 {
		t.Fatalf("terminal background: %+v fg %v bg %v", p, fg, bg)
	}
	cfg.UI.ThemePanes = false
	if _, fg, bg := cfg.PaneColors(); fg || bg {
		t.Fatalf("theme-panes off: fg %v bg %v", fg, bg)
	}
	for _, name := range []string{"light"} {
		cfg = Default()
		cfg.Theme = name
		if _, fg, bg := cfg.PaneColors(); fg || bg {
			t.Fatalf("%s themes panes", name)
		}
	}
}

func TestEveryThemeNameHasAPalette(t *testing.T) {
	for _, name := range ThemeNames {
		if _, ok := Palettes[name]; !ok {
			t.Errorf("%s has no palette", name)
		}
	}
	if len(Palettes) != len(ThemeNames) {
		t.Errorf("%d palettes for %d names", len(Palettes), len(ThemeNames))
	}
}

func TestPaneBackgroundIsValidated(t *testing.T) {
	cfg, err := Load(write(t, "[ui]\npane-background = \"Terminal\"\n"), noEnv)
	if err != nil || cfg.UI.PaneBackground != "terminal" {
		t.Fatalf("%q %v", cfg.UI.PaneBackground, err)
	}
	cfg, err = Load(write(t, "[ui]\npane-background = \"clear\"\n"), noEnv)
	if err == nil || !strings.Contains(err.Error(), "ui.pane-background") || cfg.UI.PaneBackground != "theme" {
		t.Fatalf("%q %v", cfg.UI.PaneBackground, err)
	}
}

func TestNoticePositionIsValidated(t *testing.T) {
	if cfg := Default(); cfg.UI.NoticePosition != "top-right" {
		t.Fatalf("default %q", cfg.UI.NoticePosition)
	}
	for _, position := range NoticePositions {
		cfg, err := Load(write(t, "[ui]\nnotice-position = \""+strings.ToUpper(position)+"\"\n"), noEnv)
		if err != nil || cfg.UI.NoticePosition != position {
			t.Fatalf("%s: got %q, %v", position, cfg.UI.NoticePosition, err)
		}
	}
	cfg, err := Load(write(t, "[ui]\nnotice-position = \"middle\"\n"), noEnv)
	if err == nil || !strings.Contains(err.Error(), "ui.notice-position") || cfg.UI.NoticePosition != "top-right" {
		t.Fatalf("%q %v", cfg.UI.NoticePosition, err)
	}
}

func TestIconsSettingIsCaseInsensitiveAndRejectsUnknownValues(t *testing.T) {
	cfg, err := Load(write(t, "[ui]\nicons = \"Nerd\"\n"), noEnv)
	if err != nil || cfg.UI.Icons != "nerd" {
		t.Fatalf("icons = %q, %v", cfg.UI.Icons, err)
	}
	cfg, err = Load(write(t, "[ui]\nicons = \"emoji\"\n"), noEnv)
	if err == nil || !strings.Contains(err.Error(), "ui.icons") || cfg.UI.Icons != "auto" {
		t.Fatalf("icons = %q, %v", cfg.UI.Icons, err)
	}
}
