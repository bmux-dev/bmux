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
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
)

// The layout follows Helix's config.toml: a theme name and the prefix at the
// top, then one table per area, kebab-case keys, and only what you change.
type Config struct {
	Theme  string `toml:"theme"`
	Prefix string `toml:"prefix"`
	// Onboarding shows the welcome screen on the next attach; bmux sets it
	// to false once the screen has been seen.
	Onboarding bool       `toml:"onboarding"`
	UI         UI         `toml:"ui"`
	Sidebar    Sidebar    `toml:"sidebar"`
	Sound      Sound      `toml:"sound"`
	Agents     Agents     `toml:"agents"`
	Restore    Restore    `toml:"restore"`
	Projects   Projects   `toml:"projects"`
	Scrollback Scrollback `toml:"scrollback"`
	Update     Update     `toml:"update"`
	// Hooks maps an event (HookEvents) to shell commands the server runs
	// when it happens, with #{...} format variables for its pane.
	Hooks map[string][]string `toml:"hooks"`
	Keys  Keys                `toml:"keys"`
}

type UI struct {
	Accent          string `toml:"accent"`
	PowerlineASCII  bool   `toml:"powerline-ascii"`
	TerminalPadding bool   `toml:"terminal-padding"`
	SetTitle        bool   `toml:"set-title"`
	// Graphics is "auto" (show images in terminals that support the Kitty
	// graphics protocol), "on" (assume the terminal does) or "off".
	Graphics    string      `toml:"graphics"`
	AgentColors AgentColors `toml:"agent-colors"`
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

// Projects configures the new-session popup (Ctrl-a n, bmux project). Roots are
// searched for git repositories down to Depth levels; paths may start with
// ~/. StartDir is where the Create tab starts and what its relative paths are
// relative to; "" means the directory bmux was started in.
type Projects struct {
	Roots    []string `toml:"roots"`
	Depth    int      `toml:"depth"`
	StartDir string   `toml:"start-dir"`
}

// Scrollback is how many lines of history each pane keeps.
type Scrollback struct {
	Lines int `toml:"lines"`
}

// Update controls the daily check for new bmux releases; bmux update
// installs one.
type Update struct {
	Check bool `toml:"check"`
}

// HookEvents are the events [hooks] can name.
var HookEvents = []string{
	"session-created", "session-closed", "window-created", "pane-exited",
	"client-attached", "client-detached", "agent-blocked", "agent-idle",
}

// Bounds for scrollback.lines; each line holds a row of cells.
const (
	MinScrollbackLines = 100
	MaxScrollbackLines = 200000
)

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
		Theme:      "dark",
		Prefix:     "C-a",
		Onboarding: true,
		UI:         UI{Accent: "cyan", SetTitle: true, Graphics: "auto"},
		Sidebar:    Sidebar{Show: true, Width: 24, MinScreenWidth: 80},
		Sound:      Sound{Enabled: true},
		Agents:     Agents{Detect: true, WorkingAfter: Duration{2 * time.Second}, WaitingAfter: Duration{2 * time.Second}},
		Restore:    Restore{Enabled: true},
		Projects:   Projects{Roots: []string{"~/code", "~/src", "~/projects", "~/dev"}, Depth: 3},
		Scrollback: Scrollback{Lines: 10000},
		Update:     Update{Check: true},
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
	if env("BMUX_NO_UPDATE_CHECK") == "1" {
		cfg.Update.Check = false
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
	c.UI.Graphics = strings.ToLower(c.UI.Graphics)
	if c.UI.Graphics != "auto" && c.UI.Graphics != "on" && c.UI.Graphics != "off" {
		bad("ui.graphics %q must be \"auto\", \"on\" or \"off\"", c.UI.Graphics)
		c.UI.Graphics = d.UI.Graphics
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
	roots := c.Projects.Roots[:0]
	for _, root := range c.Projects.Roots {
		if expanded, err := expandHome(root); err != nil || !filepath.IsAbs(expanded) {
			bad("projects.roots %q must be an absolute path or start with ~/", root)
			continue
		}
		roots = append(roots, root)
	}
	c.Projects.Roots = roots
	if c.Projects.StartDir != "" {
		if expanded, err := expandHome(c.Projects.StartDir); err != nil || !filepath.IsAbs(expanded) {
			bad("projects.start-dir %q must be an absolute path or start with ~/", c.Projects.StartDir)
			c.Projects.StartDir = d.Projects.StartDir
		}
	}
	if c.Projects.Depth < 1 || c.Projects.Depth > 6 {
		bad("projects.depth %d must be between 1 and 6", c.Projects.Depth)
		c.Projects.Depth = d.Projects.Depth
	}
	if c.Scrollback.Lines < MinScrollbackLines || c.Scrollback.Lines > MaxScrollbackLines {
		bad("scrollback.lines %d must be between %d and %d", c.Scrollback.Lines, MinScrollbackLines, MaxScrollbackLines)
		c.Scrollback.Lines = d.Scrollback.Lines
	}
	for event, commands := range c.Hooks {
		if !slices.Contains(HookEvents, event) {
			bad("hooks.%s isn't an event; use one of %s", event, strings.Join(HookEvents, ", "))
			delete(c.Hooks, event)
			continue
		}
		if slices.ContainsFunc(commands, func(s string) bool { return strings.TrimSpace(s) == "" }) {
			bad("hooks.%s has an empty command", event)
			delete(c.Hooks, event)
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
# onboarding = true         # welcome screen on the next attach; bmux sets false

[ui]
# accent = "cyan"           # cyan, amber, green or "#RRGGBB"
# powerline-ascii = false   # plain separators instead of powerline glyphs
# terminal-padding = false  # keep the outer terminal's padding
# set-title = true          # name the terminal window after the session and pane
# graphics = "auto"         # show images programs draw: "auto" detects Kitty
                            # graphics support, "on" assumes it, "off" never

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

[projects]                  # folders the new-session popup lists, Ctrl-a n and bmux project
# roots = ["~/code", "~/src", "~/projects", "~/dev"]  # searched for git repositories
# depth = 3                 # how many levels down each root to look
# start-dir = ""            # where the Create tab starts, e.g. "~"; empty = the current directory

[scrollback]
# lines = 10000             # history kept per pane, 100-200000; bmux reload applies it

[update]
# check = true              # look for a new release once a day; bmux update installs it

[hooks]                     # shell commands run on events, with #{...} variables for the event's pane
# Events: session-created, session-closed, window-created, pane-exited,
# client-attached, client-detached, agent-blocked, agent-idle. See bmux guide.
# agent-blocked = ["notify-send 'bmux' '#{session_name}: #{pane_agent} needs you'"]

# Keys are written like Helix's: "C-h" is Ctrl-h, "A-x" Alt-x, "S-pageup"
# Shift-PageUp, "|" or "minus" a plain key. "none" unbinds a key. Actions:
# navigate-left/down/up/right (Vim/Neovim-aware), focus-left/down/up/right,
# next-pane, last-pane, resize-left/down/up/right, split-right, split-down,
# zoom, close-pane, swap-pane-previous, swap-pane-next, rotate-window,
# break-pane, join-pane, next-layout, layout-even-horizontal,
# layout-even-vertical, layout-main-horizontal, layout-main-vertical,
# layout-tiled, equalize-layout, new-window, close-window, rename-window,
# next-window, previous-window, last-window, select-window-1 to
# select-window-9, scrollback, search-scrollback, sessions, new-session, last-session, previous-session,
# next-session, detach, copy, scroll-up, scroll-down, toggle-sidebar,
# reload-config, help, integrations, settings, send-prefix. "run-shell COMMAND"
# runs a shell command for the focused pane, e.g. "run-shell bmux split -- lazygit".

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
# ";" = "last-pane"
# "{" = "swap-pane-previous"
# "}" = "swap-pane-next"
# "C-o" = "rotate-window"
# "!" = "break-pane"        # to a new window
# "@" = "join-pane"         # into the last window, beside its focused pane
# "space" = "next-layout"
# "A-1" = "layout-even-horizontal"  # A-2 even-vertical, A-3 main-horizontal,
#                                   # A-4 main-vertical, A-5 tiled
# "E" = "equalize-layout"
# "left" = "focus-left"     # and down, up, right
# "h" = "resize-left"       # j down, k up, l right; repeat without the prefix
# "s" = "sessions"
# "n" = "new-session"       # find a project or create a session
# "p" = "last-session"
# "(" = "previous-session"
# ")" = "next-session"
# "c" = "new-window"        # in the focused pane's directory
# "&" = "close-window"      # asks first if a program is running
# "R" = "rename-window"
# "]" = "next-window"
# "[" = "previous-window"
# "tab" = "last-window"
# "1" = "select-window-1"   # through "9"
# "d" = "detach"
# "y" = "copy"
# "v" = "scrollback"        # keyboard view of history: move, select, copy
# "/" = "search-scrollback" # search history, newest first
# "b" = "toggle-sidebar"
# "r" = "reload-config"
# "?" = "help"
# "i" = "integrations"
# "," = "settings"          # change settings; saved to this file
`
