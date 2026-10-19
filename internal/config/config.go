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

// The layout follows Helix's config.toml: a theme name and the prefix at the
// top, then one table per area, kebab-case keys, and only what you change.
type Config struct {
	Theme   string  `toml:"theme"`
	Prefix  string  `toml:"prefix"`
	UI      UI      `toml:"ui"`
	Sidebar Sidebar `toml:"sidebar"`
	Sound   Sound   `toml:"sound"`
	Agents  Agents  `toml:"agents"`
	Restore Restore `toml:"restore"`
	Keys    Keys    `toml:"keys"`
}

type UI struct {
	Accent          string      `toml:"accent"`
	PowerlineASCII  bool        `toml:"powerline-ascii"`
	TerminalPadding bool        `toml:"terminal-padding"`
	SetTitle        bool        `toml:"set-title"`
	AgentColors     AgentColors `toml:"agent-colors"`
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
	MinScreenWidth int  `toml:"min-screen-width"`
}

type Sound struct {
	Enabled bool   `toml:"enabled"`
	Ready   string `toml:"ready"`
	Blocked string `toml:"blocked"`
}

type Agents struct {
	Detect       bool     `toml:"detect"`
	Extra        []string `toml:"extra"`
	WorkingAfter Duration `toml:"working-after"`
	WaitingAfter Duration `toml:"waiting-after"`
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
		Theme:   "dark",
		Prefix:  "C-a",
		UI:      UI{Accent: "cyan", SetTitle: true},
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
		cfg.Theme = strings.ToLower(v)
		if cfg.Theme != "light" {
			cfg.Theme = "dark"
		}
	}
	if v := env("BMUX_ACCENT"); v != "" {
		cfg.UI.Accent = v
	}
	if v := env("BMUX_POWERLINE_ASCII"); v != "" {
		cfg.UI.PowerlineASCII = v == "1"
	}
	if v := env("BMUX_TERMINAL_PADDING"); v != "" {
		cfg.UI.TerminalPadding = v == "1"
	}
	if v := env("BMUX_SOUND"); v != "" {
		cfg.Sound.Enabled = v != "0"
	}
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// PrefixKey returns the letter of a Ctrl-letter prefix, written like Helix
// keys ("C-a"; "ctrl-a" also works). Ctrl-h/j/k/l navigate, and Ctrl-i and
// Ctrl-m are Tab and Enter, so they can't be the prefix.
func PrefixKey(prefix string) (rune, error) {
	value := strings.ToLower(strings.TrimSpace(prefix))
	for _, sep := range []string{"c-", "ctrl-", "ctrl+"} {
		if letter, ok := strings.CutPrefix(value, sep); ok && len(letter) == 1 && letter[0] >= 'a' && letter[0] <= 'z' {
			if strings.ContainsRune("hijklm", rune(letter[0])) {
				return 0, fmt.Errorf("prefix %q is taken: C-h/j/k/l navigate and C-i/C-m are Tab and Enter", prefix)
			}
			return rune(letter[0]), nil
		}
	}
	return 0, fmt.Errorf("prefix %q must be a Ctrl-letter like \"C-a\"", prefix)
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
	c.Theme = strings.ToLower(c.Theme)
	if c.Theme != "dark" && c.Theme != "light" {
		bad("theme %q must be \"dark\" or \"light\"", c.Theme)
		c.Theme = d.Theme
	}
	switch accent := strings.ToLower(c.UI.Accent); {
	case accent == "cyan", accent == "amber", accent == "green", hexColor.MatchString(accent):
		c.UI.Accent = accent
	default:
		bad("ui.accent %q must be cyan, amber, green or #RRGGBB", c.UI.Accent)
		c.UI.Accent = d.UI.Accent
	}
	for _, color := range []*string{&c.UI.AgentColors.Working, &c.UI.AgentColors.Waiting, &c.UI.AgentColors.Blocked, &c.UI.AgentColors.Idle} {
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
		bad("sidebar.min-screen-width %d must leave at least 20 columns for panes", c.Sidebar.MinScreenWidth)
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
	}{{"agents.working-after", &c.Agents.WorkingAfter, d.Agents.WorkingAfter}, {"agents.waiting-after", &c.Agents.WaitingAfter, d.Agents.WaitingAfter}} {
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
	c.repairKeys(bad)
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

// DefaultFile is a Helix-style config.toml listing every setting, commented
// out, with its default; `bmux default-config` prints it. Uncommented, it
// parses to Default().
const DefaultFile = `# ~/.config/bmux/config.toml
# Uncomment only what you change. Reload with Ctrl-a r or bmux reload.

# theme = "dark"            # "dark" or "light"
# prefix = "C-a"            # any C-letter except C-h/j/k/l, C-i, C-m

[ui]
# accent = "cyan"           # cyan, amber, green or "#RRGGBB"
# powerline-ascii = false   # plain separators instead of powerline glyphs
# terminal-padding = false  # keep the outer terminal's padding
# set-title = true          # name the terminal window after the session and pane

[ui.agent-colors]           # "#RRGGBB"; empty keeps herdr's palette
# working = ""
# waiting = ""
# blocked = ""
# idle = ""

[sidebar]
# show = true               # Ctrl-a b toggles it
# width = 24                # columns, 12-60
# min-screen-width = 80     # hide it on narrower terminals

[sound]
# enabled = true            # when a background agent is ready or blocked
# ready = ""                # sound files; empty uses the system sounds
# blocked = ""

[agents]
# detect = true             # recognize agents running in panes
# extra = []                # more program names, e.g. ["myagent"]
# working-after = "2s"      # drawing this long = working
# waiting-after = "2s"      # quiet this long = waiting

[restore]
# enabled = true            # save sessions and restore them after restarts

[restore.resume]            # per-agent resume command; [] = plain shell
# claude = ["claude", "--continue"]
# codex = ["codex", "resume", "--last"]

# Keys are written like Helix's: "C-h" is Ctrl-h, "A-x" Alt-x, "S-pageup"
# Shift-PageUp, "|" or "minus" a plain key. "none" unbinds a key. Actions:
# navigate-left/down/up/right (Vim/Neovim-aware), focus-left/down/up/right,
# next-pane, resize-left/down/up/right, split-right, split-down, zoom,
# close-pane, sessions, new-session, last-session, previous-session,
# next-session, detach, copy, scroll-up, scroll-down, toggle-sidebar,
# reload-config, help, integrations, send-prefix.

[keys]
# pass-through = []         # programs that get Ctrl-h/j/k/l like Vim, e.g. ["fzf"]

[keys.root]                 # without the prefix; unbound keys reach the app
# "C-h" = "navigate-left"   # "none" gives Ctrl-h to the app
# "C-j" = "navigate-down"
# "C-k" = "navigate-up"
# "C-l" = "navigate-right"  # "none" lets Ctrl-l clear the shell
# "S-pageup" = "scroll-up"
# "S-pagedown" = "scroll-down"

[keys.prefix]               # after the prefix; the prefix twice sends it
# "|" = "split-right"
# "minus" = "split-down"
# "x" = "close-pane"
# "m" = "zoom"
# "o" = "next-pane"
# "left" = "focus-left"     # and down, up, right
# "h" = "resize-left"       # j down, k up, l right; repeat without the prefix
# "s" = "sessions"
# "n" = "new-session"
# "p" = "last-session"
# "(" = "previous-session"
# ")" = "next-session"
# "d" = "detach"
# "y" = "copy"
# "b" = "toggle-sidebar"
# "r" = "reload-config"
# "?" = "help"
# "i" = "integrations"
`
