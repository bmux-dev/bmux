package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Keys rebinds keys, written like Helix's: "C-h" is Ctrl-h, "A-x" Alt-x,
// "S-pageup" Shift-PageUp, "|" a plain character. Root keys work without the
// prefix; prefix keys follow it. "none" unbinds a key, so it reaches the
// application (root) or does nothing (prefix). PassThrough names programs
// that, like Vim and Neovim, receive Ctrl-h/j/k/l when navigate keys are
// pressed with them in front; the server reads it.
type Keys struct {
	PassThrough []string          `toml:"pass-through"`
	Root        map[string]string `toml:"root"`
	Prefix      map[string]string `toml:"prefix"`
}

// Unbound removes a key's binding.
const Unbound = "none"

// Actions lists every action a key can run, with what the help shows.
var Actions = []struct{ Name, Help string }{
	{"navigate-left", "focus left, or Vim/Neovim's split"},
	{"navigate-down", "focus down, or Vim/Neovim's split"},
	{"navigate-up", "focus up, or Vim/Neovim's split"},
	{"navigate-right", "focus right, or Vim/Neovim's split"},
	{"focus-left", "focus the pane left"},
	{"focus-down", "focus the pane below"},
	{"focus-up", "focus the pane above"},
	{"focus-right", "focus the pane right"},
	{"next-pane", "focus the next pane"},
	{"resize-left", "move the divider left (repeats)"},
	{"resize-down", "move the divider down (repeats)"},
	{"resize-up", "move the divider up (repeats)"},
	{"resize-right", "move the divider right (repeats)"},
	{"split-right", "split side by side"},
	{"split-down", "split stacked"},
	{"zoom", "zoom or unzoom the pane"},
	{"close-pane", "close the pane"},
	{"sessions", "session picker"},
	{"new-session", "new session"},
	{"last-session", "last session"},
	{"previous-session", "previous session"},
	{"next-session", "next session"},
	{"detach", "detach"},
	{"copy", "copy the selection"},
	{"scroll-up", "scroll history up"},
	{"scroll-down", "scroll history down"},
	{"toggle-sidebar", "show or hide the sidebar"},
	{"reload-config", "reload the config file"},
	{"help", "list these key bindings"},
	{"send-prefix", "send the prefix key to the application"},
}

func knownAction(name string) bool {
	for _, a := range Actions {
		if a.Name == name {
			return true
		}
	}
	return false
}

// DefaultRootKeys and DefaultPrefixKeys are the built-in bindings, keyed by
// canonical key names. The prefix key itself also sends the prefix.
var DefaultRootKeys = map[string]string{
	"C-h": "navigate-left", "C-j": "navigate-down", "C-k": "navigate-up", "C-l": "navigate-right",
	"S-pageup": "scroll-up", "S-pagedown": "scroll-down",
}

var DefaultPrefixKeys = map[string]string{
	"|": "split-right", "minus": "split-down", "x": "close-pane", "m": "zoom", "o": "next-pane",
	"left": "focus-left", "down": "focus-down", "up": "focus-up", "right": "focus-right",
	"h": "resize-left", "j": "resize-down", "k": "resize-up", "l": "resize-right",
	"s": "sessions", "n": "new-session", "p": "last-session", "(": "previous-session", ")": "next-session",
	"d": "detach", "y": "copy", "b": "toggle-sidebar", "r": "reload-config", "?": "help",
}

var namedKeys = map[string]string{
	"left": "left", "right": "right", "up": "up", "down": "down", "home": "home", "end": "end",
	"pageup": "pageup", "pagedown": "pagedown", "ins": "ins", "insert": "ins", "del": "del", "delete": "del",
	"backspace": "backspace", "tab": "tab", "ret": "ret", "enter": "ret", "esc": "esc", "escape": "esc",
	"space": "space", "minus": "minus",
}

// KeyName returns a key's canonical name: modifiers in the order C-, A-, S-,
// then a character or a named key. Ctrl-letters are lowercase, since
// terminals can't tell Ctrl-Shift-a from Ctrl-a; otherwise Shift on a
// character is folded into it ("S-a" is "A", "A-S-a" is "A-A"). "-" alone
// is "minus".
func KeyName(key string) (string, error) {
	rest := key
	if rest == "-" {
		return "minus", nil
	}
	var ctrl, alt, shift bool
	for len(rest) > 2 && rest[1] == '-' {
		switch rest[0] {
		case 'C', 'c':
			ctrl = true
		case 'A', 'a':
			alt = true
		case 'S', 's':
			shift = true
		default:
			return "", fmt.Errorf("key %q has an unknown modifier; use C-, A- or S-", key)
		}
		rest = rest[2:]
	}
	if rest == "-" {
		rest = "minus"
	}
	base := ""
	if r, size := utf8.DecodeRuneInString(rest); size == len(rest) && size > 0 && r != utf8.RuneError {
		if !unicode.IsPrint(r) {
			return "", fmt.Errorf("key %q is not a printable character", key)
		}
		if ctrl && unicode.IsLetter(r) {
			r, shift = unicode.ToLower(r), false
		} else if shift && !ctrl {
			// The character already says Shift: A, |, (.
			r, shift = unicode.ToUpper(r), false
		}
		switch r {
		case ' ':
			base = "space"
		case '-':
			base = "minus"
		default:
			base = string(r)
		}
	} else if named, ok := namedKeys[strings.ToLower(rest)]; ok {
		base = named
		if (named == "space" || named == "minus") && !ctrl {
			shift = false // characters, like the single-rune forms above
		}
	} else if n, err := strconv.Atoi(strings.TrimLeft(rest, "Ff")); err == nil && len(rest) > 1 && strings.ContainsRune("Ff", rune(rest[0])) && rest[1:] == strconv.Itoa(n) && n >= 1 && n <= 12 {
		base = "F" + strconv.Itoa(n)
	} else {
		return "", fmt.Errorf("key %q is not a character or a known key name", key)
	}
	name := base
	if shift {
		name = "S-" + name
	}
	if alt {
		name = "A-" + name
	}
	if ctrl {
		name = "C-" + name
	}
	return name, nil
}

// Bindings returns the root and prefix key tables in effect: the defaults,
// the prefix key sending itself, then the config file's changes.
func (c Config) Bindings() (root, prefix map[string]string) {
	merge := func(defaults, changes map[string]string) map[string]string {
		result := make(map[string]string, len(defaults)+len(changes))
		for key, action := range defaults {
			result[key] = action
		}
		for key, action := range changes {
			if action == Unbound {
				delete(result, key)
			} else {
				result[key] = action
			}
		}
		return result
	}
	prefixDefaults := merge(DefaultPrefixKeys, nil)
	if letter, err := PrefixKey(c.Prefix); err == nil {
		prefixDefaults["C-"+string(letter)] = "send-prefix"
	}
	return merge(DefaultRootKeys, c.Keys.Root), merge(prefixDefaults, c.Keys.Prefix)
}

// repairKeys canonicalizes key names and drops bindings that can't work.
func (c *Config) repairKeys(bad func(string, ...any)) {
	passThrough := c.Keys.PassThrough[:0]
	for _, name := range c.Keys.PassThrough {
		if name == "" || strings.ContainsRune(name, '/') {
			bad("keys.pass-through %q must be a program name, not a path", name)
			continue
		}
		passThrough = append(passThrough, name)
	}
	c.Keys.PassThrough = passThrough
	prefixKey := ""
	if letter, err := PrefixKey(c.Prefix); err == nil {
		prefixKey = "C-" + string(letter)
	}
	for _, table := range []struct {
		name     string
		bindings *map[string]string
	}{{"keys.root", &c.Keys.Root}, {"keys.prefix", &c.Keys.Prefix}} {
		if *table.bindings == nil {
			continue
		}
		keys := make([]string, 0, len(*table.bindings))
		for key := range *table.bindings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		repaired := make(map[string]string, len(keys))
		for _, key := range keys {
			action := (*table.bindings)[key]
			name, err := KeyName(key)
			switch {
			case err != nil:
				bad("%s: %v", table.name, err)
			case action != Unbound && !knownAction(action):
				bad("%s.%q: unknown action %q (see bmux default-config)", table.name, key, action)
			case table.name == "keys.root" && name == prefixKey:
				bad("keys.root.%q: the prefix can't also be a root key", key)
			default:
				if _, twice := repaired[name]; twice {
					bad("%s.%q: %s is bound twice", table.name, key, name)
				}
				repaired[name] = action
			}
		}
		*table.bindings = repaired
	}
}
