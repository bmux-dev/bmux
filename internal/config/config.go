// Package config reads the optional bmux config file. Every setting has a
// default, so a missing file means defaults; BMUX_* environment variables
// override the file. Client settings (keys, theme, sidebar, sound) and server
// settings (agents, restore) live in the same file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Prefix  string  `toml:"prefix"`
	Theme   Theme   `toml:"theme"`
	Sidebar Sidebar `toml:"sidebar"`
	Sound   Sound   `toml:"sound"`
	Agents  Agents  `toml:"agents"`
	Restore Restore `toml:"restore"`
}

type Theme struct {
	Name            string      `toml:"name"`
	Accent          string      `toml:"accent"`
	PowerlineASCII  bool        `toml:"powerline_ascii"`
	TerminalPadding bool        `toml:"terminal_padding"`
	Agents          AgentColors `toml:"agents"`
}

// AgentColors override the sidebar's state colors; "" keeps herdr's palette.
type AgentColors struct {
	Working string `toml:"working"`
	Waiting string `toml:"waiting"`
	Blocked string `toml:"blocked"`
	Idle    string `toml:"idle"`
}

type Sidebar struct {
	Show           bool `toml:"show"`
	Width          int  `toml:"width"`
	MinScreenWidth int  `toml:"min_screen_width"`
}

type Sound struct {
	Enabled bool   `toml:"enabled"`
	Ready   string `toml:"ready"`
	Blocked string `toml:"blocked"`
}

type Agents struct {
	Detect       bool     `toml:"detect"`
	Extra        []string `toml:"extra"`
	WorkingAfter Duration `toml:"working_after"`
	WaitingAfter Duration `toml:"waiting_after"`
}

type Restore struct {
	Enabled bool `toml:"enabled"`
	// Resume replaces an agent's built-in resume command; an empty list
	// restores that agent's panes as plain shells.
	Resume map[string][]string `toml:"resume"`
}

// Duration reads TOML strings such as "2s" or "1500ms".
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	d.Duration = parsed
	return err
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func Default() Config {
	return Config{
		Prefix:  "ctrl-a",
		Theme:   Theme{Name: "dark", Accent: "cyan"},
		Sidebar: Sidebar{Show: true, Width: 24, MinScreenWidth: 80},
		Sound:   Sound{Enabled: true},
		Agents:  Agents{Detect: true, WorkingAfter: Duration{2 * time.Second}, WaitingAfter: Duration{2 * time.Second}},
		Restore: Restore{Enabled: true},
	}
}

// Path is $BMUX_CONFIG, else $XDG_CONFIG_HOME/bmux/config.toml, else
// ~/.config/bmux/config.toml on every platform.
func Path() (string, error) {
	if path := os.Getenv("BMUX_CONFIG"); path != "" {
		return filepath.Abs(path)
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "bmux", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "bmux", "config.toml"), nil
}

// Load reads the config file at path, then applies environment overrides.
// It always returns a usable config: a missing file gives the defaults, and
// each invalid setting keeps its default. The error lists every problem, so
// callers can show it without refusing to start.
func Load(path string, env func(string) string) (Config, error) {
	cfg := Default()
	var problems []string
	if data, err := os.ReadFile(path); err == nil {
		meta, err := toml.Decode(string(data), &cfg)
		if err != nil {
			cfg = Default()
			problems = append(problems, err.Error())
		}
		for _, key := range meta.Undecoded() {
			problems = append(problems, fmt.Sprintf("unknown setting %q", key.String()))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		problems = append(problems, err.Error())
	}
	applyEnv(&cfg, env)
	problems = append(problems, cfg.repair()...)
	if len(problems) > 0 {
		return cfg, fmt.Errorf("%s: %s", path, strings.Join(problems, "; "))
	}
	return cfg, nil
}

func applyEnv(cfg *Config, env func(string) string) {
	if env == nil {
		return
	}
	if v := env("BMUX_THEME"); v != "" {
		cfg.Theme.Name = strings.ToLower(v)
		if cfg.Theme.Name != "light" {
			cfg.Theme.Name = "dark"
		}
	}
	if v := env("BMUX_ACCENT"); v != "" {
		cfg.Theme.Accent = v
	}
	if v := env("BMUX_POWERLINE_ASCII"); v != "" {
		cfg.Theme.PowerlineASCII = v == "1"
	}
	if v := env("BMUX_TERMINAL_PADDING"); v != "" {
		cfg.Theme.TerminalPadding = v == "1"
	}
	if v := env("BMUX_SOUND"); v != "" {
		cfg.Sound.Enabled = v != "0"
	}
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// PrefixKey returns the letter of a "ctrl-<letter>" prefix. Ctrl-h/j/k/l
// navigate, and Ctrl-i/Ctrl-m are Tab and Enter, so they can't be the prefix.
func PrefixKey(prefix string) (rune, error) {
	value := strings.ToLower(strings.TrimSpace(prefix))
	for _, sep := range []string{"ctrl-", "ctrl+", "c-"} {
		if letter, ok := strings.CutPrefix(value, sep); ok && len(letter) == 1 && letter[0] >= 'a' && letter[0] <= 'z' {
			if strings.ContainsRune("hijklm", rune(letter[0])) {
				return 0, fmt.Errorf("prefix %q is taken: Ctrl-h/j/k/l navigate and Ctrl-i/Ctrl-m are Tab and Enter", prefix)
			}
			return rune(letter[0]), nil
		}
	}
	return 0, fmt.Errorf("prefix %q must look like \"ctrl-a\"", prefix)
}

// ValidResume follows the server's rules for resume commands.
func ValidResume(argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	if argv[0] == "" || strings.ContainsRune(argv[0], '/') {
		return fmt.Errorf("must start with a command name on PATH")
	}
	for _, arg := range argv {
		for _, r := range arg {
			if unicode.IsControl(r) {
				return fmt.Errorf("must not contain control characters")
			}
		}
	}
	return nil
}

// repair resets invalid settings to their defaults and describes each one.
func (c *Config) repair() []string {
	d := Default()
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if _, err := PrefixKey(c.Prefix); err != nil {
		bad("%v", err)
		c.Prefix = d.Prefix
	}
	c.Theme.Name = strings.ToLower(c.Theme.Name)
	if c.Theme.Name != "dark" && c.Theme.Name != "light" {
		bad("theme.name %q must be \"dark\" or \"light\"", c.Theme.Name)
		c.Theme.Name = d.Theme.Name
	}
	switch accent := strings.ToLower(c.Theme.Accent); {
	case accent == "cyan", accent == "amber", accent == "green", hexColor.MatchString(accent):
		c.Theme.Accent = accent
	default:
		bad("theme.accent %q must be cyan, amber, green or #RRGGBB", c.Theme.Accent)
		c.Theme.Accent = d.Theme.Accent
	}
	for _, color := range []*string{&c.Theme.Agents.Working, &c.Theme.Agents.Waiting, &c.Theme.Agents.Blocked, &c.Theme.Agents.Idle} {
		if *color != "" && !hexColor.MatchString(*color) {
			bad("agent color %q must be #RRGGBB", *color)
			*color = ""
		}
	}
	if c.Sidebar.Width < 12 || c.Sidebar.Width > 60 {
		bad("sidebar.width %d must be between 12 and 60", c.Sidebar.Width)
		c.Sidebar.Width = d.Sidebar.Width
	}
	if c.Sidebar.MinScreenWidth < c.Sidebar.Width+20 {
		bad("sidebar.min_screen_width %d must leave at least 20 columns for panes", c.Sidebar.MinScreenWidth)
		c.Sidebar.MinScreenWidth = max(d.Sidebar.MinScreenWidth, c.Sidebar.Width+20)
	}
	for _, sound := range []*string{&c.Sound.Ready, &c.Sound.Blocked} {
		if *sound == "" {
			continue
		}
		if expanded, err := expandHome(*sound); err != nil || !filepath.IsAbs(expanded) {
			bad("sound file %q must be an absolute path or start with ~/", *sound)
			*sound = ""
		} else {
			*sound = expanded
		}
	}
	for _, timing := range []struct {
		name  string
		value *Duration
		def   Duration
	}{{"agents.working_after", &c.Agents.WorkingAfter, d.Agents.WorkingAfter}, {"agents.waiting_after", &c.Agents.WaitingAfter, d.Agents.WaitingAfter}} {
		if timing.value.Duration < time.Second || timing.value.Duration > time.Minute {
			bad("%s %s must be between 1s and 1m", timing.name, timing.value.Duration)
			*timing.value = timing.def
		}
	}
	extra := c.Agents.Extra[:0]
	for _, name := range c.Agents.Extra {
		if name == "" || strings.ContainsRune(name, '/') {
			bad("agents.extra %q must be a program name, not a path", name)
			continue
		}
		extra = append(extra, name)
	}
	c.Agents.Extra = extra
	for agent, argv := range c.Restore.Resume {
		if err := ValidResume(argv); err != nil {
			bad("restore.resume.%s %v", agent, err)
			delete(c.Restore.Resume, agent)
		}
	}
	return problems
}

func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	return path, nil
}

// DefaultFile documents every setting with its default; `bmux default-config`
// prints it. Uncommented, it parses to Default().
const DefaultFile = `# bmux configuration. Save as ~/.config/bmux/config.toml (or set BMUX_CONFIG).
# Every setting is optional; these are the defaults. Apply changes with
# Ctrl-a r or bmux reload. BMUX_THEME, BMUX_ACCENT, BMUX_POWERLINE_ASCII,
# BMUX_TERMINAL_PADDING and BMUX_SOUND still override this file.

# The key that starts commands: ctrl-a through ctrl-z, except
# ctrl-h/j/k/l (navigation) and ctrl-i/ctrl-m (Tab and Enter).
# prefix = "ctrl-a"

[theme]
# "dark" or "light".
# name = "dark"
# cyan, amber, green or "#RRGGBB".
# accent = "cyan"
# Plain-text separators instead of powerline glyphs.
# powerline_ascii = false
# Keep the outer terminal's padding (read when a client starts).
# terminal_padding = false

[theme.agents]
# Sidebar state colors as "#RRGGBB"; empty keeps herdr's palette.
# working = ""
# waiting = ""
# blocked = ""
# idle = ""

[sidebar]
# Show the sidebar when a client starts; Ctrl-a b toggles it.
# show = true
# Columns, including its border (12-60).
# width = 24
# Hide it on terminals narrower than this.
# min_screen_width = 80

[sound]
# Play a sound when an agent you aren't looking at becomes ready or blocked.
# enabled = true
# Custom sound files (absolute or ~/ paths); empty uses the system sounds.
# ready = ""
# blocked = ""

[agents]
# Recognize coding agents in panes and show them in the sidebar.
# detect = true
# More program names to recognize as agents.
# extra = []
# Output this long in a row means working; quiet this long means waiting.
# working_after = "2s"
# waiting_after = "2s"

[restore]
# Save sessions and restore them after restarts and reboots.
# enabled = true

[restore.resume]
# Replace an agent's resume command; an empty list restores it as a shell.
# claude = ["claude", "--continue"]
# codex = ["codex", "resume", "--last"]
`
