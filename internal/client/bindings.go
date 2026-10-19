package client

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

var namedEventKeys = map[tcell.Key]string{
	tcell.KeyLeft: "left", tcell.KeyRight: "right", tcell.KeyUp: "up", tcell.KeyDown: "down",
	tcell.KeyHome: "home", tcell.KeyEnd: "end", tcell.KeyPgUp: "pageup", tcell.KeyPgDn: "pagedown",
	tcell.KeyInsert: "ins", tcell.KeyDelete: "del", tcell.KeyBackspace: "backspace", tcell.KeyBackspace2: "backspace",
	tcell.KeyTab: "tab", tcell.KeyEnter: "ret", tcell.KeyEscape: "esc",
}

var ctrlPunctuation = map[tcell.Key]string{
	tcell.KeyCtrlSpace: "space", tcell.KeyCtrlBackslash: "\\", tcell.KeyCtrlRightSq: "]",
	tcell.KeyCtrlCarat: "^", tcell.KeyCtrlUnderscore: "_",
}

// keyName is the config.KeyName of a key event, or "" for keys that can't
// be bound. tcell reports Ctrl-letters as KeyCtrlA-KeyCtrlZ, apart from
// Backspace, Tab and Enter; the input reader turns raw BS (0x08) into Ctrl-h.
func keyName(e *tcell.EventKey) string {
	mods := e.Modifiers()
	base := ""
	k := e.Key()
	switch {
	case k == tcell.KeyRune:
		r := e.Rune()
		if mods&tcell.ModCtrl != 0 && unicode.IsLetter(r) {
			r = unicode.ToLower(r)
			mods &^= tcell.ModShift
		} else if mods&tcell.ModCtrl == 0 {
			mods &^= tcell.ModShift // the character already says Shift
		}
		switch r {
		case ' ':
			base = "space"
		case '-':
			base = "minus"
		default:
			base = string(r)
		}
	case k == tcell.KeyBacktab:
		base, mods = "tab", mods|tcell.ModShift
	case namedEventKeys[k] != "":
		base = namedEventKeys[k]
	case k >= tcell.KeyF1 && k <= tcell.KeyF12:
		base = "F" + strconv.Itoa(int(k-tcell.KeyF1)+1)
	case k >= tcell.KeyCtrlA && k <= tcell.KeyCtrlZ:
		base, mods = string(rune('a'+k-tcell.KeyCtrlA)), mods|tcell.ModCtrl
	case ctrlPunctuation[k] != "":
		base, mods = ctrlPunctuation[k], mods|tcell.ModCtrl
	default:
		return ""
	}
	name := base
	if mods&tcell.ModShift != 0 {
		name = "S-" + name
	}
	if mods&tcell.ModAlt != 0 {
		name = "A-" + name
	}
	if mods&tcell.ModCtrl != 0 {
		name = "C-" + name
	}
	return name
}

// keyTables returns the root and prefix bindings in effect.
func (u *ui) keyTables() (root, prefix map[string]string) {
	if u.rootKeys == nil {
		settings := u.settings
		if settings.Prefix == "" {
			settings.Prefix = config.Default().Prefix
		}
		u.rootKeys, u.prefixKeys = settings.Bindings()
	}
	return u.rootKeys, u.prefixKeys
}

func (u *ui) rootAction(e *tcell.EventKey) string {
	root, _ := u.keyTables()
	return root[keyName(e)]
}

func (u *ui) prefixAction(e *tcell.EventKey) string {
	_, prefix := u.keyTables()
	return prefix[keyName(e)]
}

// actionDirection splits "navigate-left" into ("navigate", "left").
func actionDirection(action string) (string, string) {
	for _, direction := range []string{"left", "down", "up", "right"} {
		if kind, ok := strings.CutSuffix(action, "-"+direction); ok {
			return kind, direction
		}
	}
	return action, ""
}

// run performs a bound action. Resize actions open the repeat window, in
// which keys bound to resize in the prefix table repeat without the prefix.
func (u *ui) run(action string, now time.Time) {
	switch kind, direction := actionDirection(action); kind {
	case "navigate":
		u.resumeLive()
		u.send(protocol.Request{Op: "navigate-pane", Direction: direction})
		return
	case "focus":
		u.send(protocol.Request{Op: "select-pane", Direction: direction})
		return
	case "resize":
		u.resizePane(direction, now)
		return
	}
	switch action {
	case "send-prefix":
		u.resumeLive()
		u.send(protocol.Request{Op: "key", Code: u.prefixLetter(), Mod: int(uv.ModCtrl)})
	case "split-right":
		u.send(protocol.Request{Op: "split", Direction: "horizontal"})
	case "split-down":
		u.send(protocol.Request{Op: "split", Direction: "vertical"})
	case "zoom":
		u.send(protocol.Request{Op: "zoom"})
	case "next-pane":
		if u.workspace != nil {
			for i, p := range u.workspace.Panes {
				if p.ID == u.focused {
					u.send(protocol.Request{Op: "select-pane", PaneID: u.workspace.Panes[(i+1)%len(u.workspace.Panes)].ID})
					break
				}
			}
		}
	case "close-pane":
		u.beginClosePane()
	case "sessions":
		u.openPicker()
	case "new-session":
		u.openPicker()
		u.beginCreate()
	case "last-session":
		if u.previous != "" {
			if err := u.switchTo(u.previous); err != nil {
				u.notice(err.Error())
			}
		}
	case "previous-session":
		u.cycleSession(-1)
	case "next-session":
		u.cycleSession(1)
	case "detach":
		u.quitting = true
	case "copy":
		u.copy()
	case "scroll-up":
		u.scroll(10)
	case "scroll-down":
		u.scroll(-10)
	case "toggle-sidebar":
		u.toggleSidebar()
	case "reload-config":
		u.requestReload()
	}
}

// keyLabel shows a canonical key name the way the status line writes keys.
func keyLabel(name string) string {
	var mods []string
	for _, m := range []struct{ prefix, label string }{{"C-", "Ctrl-"}, {"A-", "Alt-"}, {"S-", "Shift-"}} {
		if rest, ok := strings.CutPrefix(name, m.prefix); ok && rest != "" {
			mods, name = append(mods, m.label), rest
		}
	}
	switch name {
	case "minus":
		name = "-"
	case "pageup":
		name = "PageUp"
	case "pagedown":
		name = "PageDown"
	}
	return strings.Join(mods, "") + name
}

// keysFor lists the labels of the keys bound to action: plain characters
// first, then named keys, then keys with modifiers.
func keysFor(table map[string]string, action string) []string {
	var keys []string
	for key, bound := range table {
		if bound == action {
			keys = append(keys, key)
		}
	}
	rank := func(key string) int {
		switch {
		case utf8.RuneCountInString(key) == 1:
			return 0
		case strings.Contains(key, "-"):
			return 2
		}
		return 1
	}
	sort.Slice(keys, func(i, j int) bool {
		if a, b := rank(keys[i]), rank(keys[j]); a != b {
			return a < b
		}
		return keys[i] < keys[j]
	})
	labels := make([]string, len(keys))
	for i, key := range keys {
		labels[i] = keyLabel(key)
	}
	return labels
}

// prefixKey is how help text writes a prefix-table action's first key,
// e.g. "Ctrl-a s", or "" when the action has no key.
func (u *ui) prefixKey(action string) string {
	_, prefix := u.keyTables()
	if keys := keysFor(prefix, action); len(keys) > 0 {
		return u.prefixName() + " " + keys[0]
	}
	return ""
}

// directionKeys writes a four-way group compactly: "hjkl", "arrows",
// "Ctrl-h/j/k/l", or the keys joined by "/" for anything else.
func directionKeys(table map[string]string, kind string) string {
	var keys []string
	for _, direction := range []string{"left", "down", "up", "right"} {
		labels := keysFor(table, kind+"-"+direction)
		if len(labels) == 0 {
			return ""
		}
		keys = append(keys, labels[0])
	}
	single := true
	for _, key := range keys {
		single = single && utf8.RuneCountInString(key) == 1
	}
	switch {
	case strings.Join(keys, " ") == "left down up right":
		return "arrows"
	case single:
		return strings.Join(keys, "")
	}
	common := ""
	for _, m := range []string{"Ctrl-", "Alt-"} {
		if strings.HasPrefix(keys[0], m) && strings.HasPrefix(keys[1], m) && strings.HasPrefix(keys[2], m) && strings.HasPrefix(keys[3], m) {
			common = m
		}
	}
	for i := range keys {
		keys[i] = strings.TrimPrefix(keys[i], common)
	}
	return common + strings.Join(keys, "/")
}

// prefixHelp is the status line while the prefix is pending, built from the
// current bindings so rebound keys are shown as they are.
func (u *ui) prefixHelp() string {
	_, prefix := u.keyTables()
	var parts []string
	add := func(keys, label string) {
		if keys != "" {
			parts = append(parts, keys+" "+label)
		}
	}
	join := func(actions ...string) string {
		var keys []string
		for _, action := range actions {
			if labels := keysFor(prefix, action); len(labels) > 0 {
				keys = append(keys, labels[0])
			}
		}
		return strings.Join(keys, " / ")
	}
	add(join("split-right", "split-down"), "split")
	add(join("close-pane"), "close")
	add(directionKeys(prefix, "focus"), "focus")
	add(directionKeys(prefix, "resize"), "resize")
	add(join("zoom"), "zoom")
	add(strings.ReplaceAll(join("previous-session", "next-session"), " / ", " "), "sessions")
	add(join("toggle-sidebar"), "sidebar")
	add(join("reload-config"), "reload")
	add(join("sessions"), "picker")
	return u.prefixName() + ": " + strings.Join(parts, " · ")
}

// keyHints joins "KEY label" hints for the status line, leaving out
// actions that have no key.
func keyHints(hints ...[2]string) string {
	var parts []string
	for _, h := range hints {
		if h[0] != "" {
			parts = append(parts, h[0]+h[1])
		}
	}
	return strings.Join(parts, " · ")
}
