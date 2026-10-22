package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Kind says how a setting is shown and edited.
type Kind int

const (
	KindBool     Kind = iota // toggled
	KindEnum                 // picked from a list of Options
	KindInt                  // typed
	KindText                 // typed
	KindDuration             // typed, like "2s"
	KindList                 // typed, comma separated
)

// Setting is one option the settings page can change. Key is its path in
// the config file ("ui.accent"); Get reads it from a Config as the text the
// page shows and edits.
type Setting struct {
	Key   string
	Label string
	Help  string
	Kind  Kind
	// Group is the header the page shows above this setting and the ones
	// next to it in the same group.
	Group string
	// Options are the values an enum offers as a list. Custom lets the user
	// type another value as well, such as an accent color; the config
	// rules still decide whether it's valid.
	Options []string
	Custom  bool
	Get     func(Config) string
}

// SettingTab groups related settings under one tab of the settings page.
type SettingTab struct {
	Name string
	// Icon is a Unicode symbol and NerdIcon a Nerd Font glyph, each one cell
	// wide and shown before the name; ui.icons picks between them.
	Icon, NerdIcon string
	Settings       []Setting
}

func boolSetting(key, label, help string, field func(*Config) *bool) Setting {
	return Setting{Key: key, Label: label, Help: help, Kind: KindBool, Options: []string{"false", "true"},
		Get: func(c Config) string { return strconv.FormatBool(*field(&c)) }}
}

func intSetting(key, label, help string, field func(*Config) *int) Setting {
	return Setting{Key: key, Label: label, Help: help, Kind: KindInt,
		Get: func(c Config) string { return strconv.Itoa(*field(&c)) }}
}

func textSetting(key, label, help string, field func(*Config) *string) Setting {
	return Setting{Key: key, Label: label, Help: help, Kind: KindText,
		Get: func(c Config) string { return *field(&c) }}
}

func enumSetting(key, label, help string, options []string, custom bool, field func(*Config) *string) Setting {
	s := textSetting(key, label, help, field)
	s.Kind, s.Options, s.Custom = KindEnum, options, custom
	return s
}

func durationSetting(key, label, help string, field func(*Config) *Duration) Setting {
	return Setting{Key: key, Label: label, Help: help, Kind: KindDuration,
		Get: func(c Config) string { return field(&c).String() }}
}

func listSetting(key, label, help string, field func(*Config) *[]string) Setting {
	return Setting{Key: key, Label: label, Help: help, Kind: KindList,
		Get: func(c Config) string { return strings.Join(*field(&c), ", ") }}
}

// group labels settings with the header the page draws above them. Settings
// in a tab that follow one another with the same group share the header.
func group(name string, settings ...Setting) []Setting {
	for i := range settings {
		settings[i].Group = name
	}
	return settings
}

// SettingTabs lists the settings the page edits. Keys, the per-agent resume
// commands and onboarding stay in the config file; see Config.
var SettingTabs = []SettingTab{
	{"General", "⌘", "\uf013", slices.Concat(
		group("Keys",
			textSetting("prefix", "Prefix key", "A Ctrl-letter such as C-a; not C-h/j/k/l, C-i or C-m", func(c *Config) *string { return &c.Prefix }),
		),
		group("Updates",
			boolSetting("update.check", "Check for updates", "Look for a new release once a day", func(c *Config) *bool { return &c.Update.Check }),
		),
	)},
	{"Appearance", "◐", "\uf1fc", slices.Concat(
		group("Theme",
			enumSetting("theme", "Theme", "Colors for the bars and overlays", ThemeNames, false, func(c *Config) *string { return &c.Theme }),
			boolSetting("ui.theme-panes", "Theme the panes", "Plain pane text uses a named theme's colors; off keeps the terminal's", func(c *Config) *bool { return &c.UI.ThemePanes }),
			enumSetting("ui.pane-background", "Pane background", "theme: the theme's background fills panes; terminal: keep your terminal's own", []string{"theme", "terminal"}, false, func(c *Config) *string { return &c.UI.PaneBackground }),
			enumSetting("ui.accent", "Accent color", "Pick a preset, or Custom for a #RRGGBB color", []string{"theme", "cyan", "amber", "green"}, true, func(c *Config) *string { return &c.UI.Accent }),
		),
		group("Display",
			boolSetting("ui.powerline-ascii", "ASCII separators", "Plain separators instead of powerline glyphs", func(c *Config) *bool { return &c.UI.PowerlineASCII }),
			boolSetting("ui.terminal-padding", "Terminal padding", "Keep the outer terminal's padding", func(c *Config) *bool { return &c.UI.TerminalPadding }),
			boolSetting("ui.set-title", "Set window title", "Name the terminal window after the session and pane", func(c *Config) *bool { return &c.UI.SetTitle }),
			enumSetting("ui.icons", "Icons", "Tab icons: auto uses Nerd Font icons unless ASCII separators are on; plain needs no special font", []string{"auto", "nerd", "plain"}, false, func(c *Config) *string { return &c.UI.Icons }),
			enumSetting("ui.graphics", "Images", "Show images programs draw: auto detects Kitty graphics, on assumes it, off never", []string{"auto", "on", "off"}, false, func(c *Config) *string { return &c.UI.Graphics }),
			enumSetting("ui.notice-position", "Notice position", "Where notices appear", NoticePositions, false, func(c *Config) *string { return &c.UI.NoticePosition }),
		),
		group("Agent colors",
			textSetting("ui.agent-colors.working", "Working color", "#RRGGBB; empty keeps the default", func(c *Config) *string { return &c.UI.AgentColors.Working }),
			textSetting("ui.agent-colors.waiting", "Waiting color", "#RRGGBB; empty keeps the default", func(c *Config) *string { return &c.UI.AgentColors.Waiting }),
			textSetting("ui.agent-colors.blocked", "Blocked color", "#RRGGBB; empty keeps the default", func(c *Config) *string { return &c.UI.AgentColors.Blocked }),
			textSetting("ui.agent-colors.idle", "Idle color", "#RRGGBB; empty keeps the default", func(c *Config) *string { return &c.UI.AgentColors.Idle }),
		),
	)},
	{"Sidebar", "≡", "\uf0db", slices.Concat(
		group("Visibility",
			boolSetting("sidebar.show", "Show sidebar", "The prefix key then b toggles it", func(c *Config) *bool { return &c.Sidebar.Show }),
		),
		group("Size",
			intSetting("sidebar.width", "Width", "Columns, 12-60", func(c *Config) *int { return &c.Sidebar.Width }),
			intSetting("sidebar.min-screen-width", "Minimum screen width", "Hide the sidebar on narrower terminals; at least width + 20", func(c *Config) *int { return &c.Sidebar.MinScreenWidth }),
		),
	)},
	{"Sound", "♪", "\uf028", group("Alerts",
		boolSetting("sound.enabled", "Alert sounds", "When a background agent is ready or blocked", func(c *Config) *bool { return &c.Sound.Enabled }),
		textSetting("sound.ready", "Ready sound", "Absolute path or ~/ file; empty uses the system sound", func(c *Config) *string { return &c.Sound.Ready }),
		textSetting("sound.blocked", "Blocked sound", "Absolute path or ~/ file; empty uses the system sound", func(c *Config) *string { return &c.Sound.Blocked }),
	)},
	{"Agents", "◉", "\uf2db", slices.Concat(
		group("Detection",
			boolSetting("agents.detect", "Detect agents", "Recognize agents running in panes", func(c *Config) *bool { return &c.Agents.Detect }),
			listSetting("agents.extra", "Extra programs", "More program names to treat as agents, comma separated", func(c *Config) *[]string { return &c.Agents.Extra }),
		),
		group("Timing",
			durationSetting("agents.working-after", "Working after", "Drawing this long means working, 1s-1m", func(c *Config) *Duration { return &c.Agents.WorkingAfter }),
			durationSetting("agents.waiting-after", "Waiting after", "Quiet this long means waiting, 1s-1m", func(c *Config) *Duration { return &c.Agents.WaitingAfter }),
		),
	)},
	{"Projects", "▤", "\uf07c", slices.Concat(
		group("Search",
			listSetting("projects.roots", "Roots", "Folders searched for git repositories, comma separated", func(c *Config) *[]string { return &c.Projects.Roots }),
			intSetting("projects.depth", "Depth", "Levels to search below each root, 1-6", func(c *Config) *int { return &c.Projects.Depth }),
		),
		group("New sessions",
			textSetting("projects.start-dir", "Start directory", "Where the new-session Create tab starts, such as ~; empty uses the current directory", func(c *Config) *string { return &c.Projects.StartDir }),
		),
	)},
	{"Sessions", "↻", "\uf1da", slices.Concat(
		group("Restore",
			boolSetting("restore.enabled", "Restore sessions", "Save sessions and restore them after restarts", func(c *Config) *bool { return &c.Restore.Enabled }),
		),
		group("History",
			intSetting("scrollback.lines", "Scrollback lines", "History kept per pane, 100-200000", func(c *Config) *int { return &c.Scrollback.Lines }),
		),
	)},
}

// Literal turns what the user typed into a TOML value for this setting.
// It checks the shape only; Save checks the value against the config rules.
func (s Setting) Literal(input string) (string, error) {
	input = strings.TrimSpace(input)
	switch s.Kind {
	case KindBool:
		if input != "true" && input != "false" {
			return "", errors.New("must be true or false")
		}
		return input, nil
	case KindInt:
		n, err := strconv.Atoi(input)
		if err != nil {
			return "", errors.New("must be a whole number")
		}
		return strconv.Itoa(n), nil
	case KindDuration:
		if _, err := time.ParseDuration(input); err != nil {
			return "", errors.New(`must be a duration such as "2s" or "1500ms"`)
		}
		return tomlString(input)
	case KindList:
		var items []string
		for _, item := range strings.Split(input, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			quoted, err := tomlString(item)
			if err != nil {
				return "", err
			}
			items = append(items, quoted)
		}
		return "[" + strings.Join(items, ", ") + "]", nil
	default:
		return tomlString(input)
	}
}

// tomlString quotes s as a TOML basic string.
func tomlString(s string) (string, error) {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("can't contain control characters")
		}
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`, nil
}

// Save sets this setting to input in the config file at path, creating the
// file if needed. Comments and every other line stay as written. Nothing is
// written when input is the wrong shape, breaks a config rule (the rules
// Load applies), or can't be placed in the file as it's laid out. Problems
// the file already had don't stop a save.
func (s Setting) Save(path, input string) error {
	literal, err := s.Literal(input)
	if err != nil {
		return fmt.Errorf("%s %v", s.Key, err)
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, known, err := s.decode(string(data))
	if err != nil {
		return fmt.Errorf("%s: can't change settings until it parses: %v", path, err)
	}
	table, key := s.location()
	updated := setLine(string(data), table, key, literal)
	got, problems, err := s.decode(updated)
	if err == nil {
		var probe string
		if table == "" {
			probe = key + " = " + literal + "\n"
		} else {
			probe = "[" + table + "]\n" + key + " = " + literal + "\n"
		}
		var want string
		if want, _, err = s.decode(probe); err == nil && got != want {
			err = fmt.Errorf("%s is set in a form bmux can't edit; change it in the file", s.Key)
		}
	}
	if err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	for _, problem := range problems {
		if !contains(known, problem) {
			return errors.New(problem)
		}
	}
	return writeAtomic(path, []byte(updated))
}

// decode parses text over the defaults. It returns this setting's value as
// written, before the config rules reset it, and the problems those rules
// find.
func (s Setting) decode(text string) (value string, problems []string, err error) {
	cfg := Default()
	if _, err := toml.Decode(text, &cfg); err != nil {
		return "", nil, err
	}
	value = s.Get(cfg)
	return value, cfg.repair(), nil
}

// location splits "ui.accent" into its table and key.
func (s Setting) location() (table, key string) {
	if i := strings.LastIndex(s.Key, "."); i >= 0 {
		return s.Key[:i], s.Key[i+1:]
	}
	return "", s.Key
}

func contains(list []string, item string) bool {
	for _, have := range list {
		if have == item {
			return true
		}
	}
	return false
}
