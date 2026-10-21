package vt

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Kitty keyboard protocol progressive enhancement flags.
const (
	kittyDisambiguate = 1 << iota
	kittyEventTypes
	kittyAlternateKeys
	kittyAllKeys
	kittyAssociatedText

	kittyFlagMask = kittyDisambiguate | kittyEventTypes | kittyAlternateKeys | kittyAllKeys | kittyAssociatedText
)

// kittyStackLimit bounds a screen's flag stack so a program that pushes in a
// loop cannot grow it without limit. The oldest entry is dropped first.
const kittyStackLimit = 64

// modMask is the set of modifiers carried in an encoded key. The bits match
// both xterm's and kitty's modifier parameter (parameter = 1 + mask).
const modMask = uv.ModShift | uv.ModAlt | uv.ModCtrl | uv.ModMeta | uv.ModHyper | uv.ModSuper

// kittyKeyboard is one screen's kitty keyboard flag stack. The top entry is
// the active flag set; an empty stack means no flags.
type kittyKeyboard struct {
	stack []int
}

func (k *kittyKeyboard) flags() int {
	if len(k.stack) == 0 {
		return 0
	}
	return k.stack[len(k.stack)-1]
}

// push is CSI > flags u.
func (k *kittyKeyboard) push(flags int) {
	if len(k.stack) >= kittyStackLimit {
		k.stack = append(k.stack[:0], k.stack[1:]...)
	}
	k.stack = append(k.stack, flags&kittyFlagMask)
}

// pop is CSI < n u. Popping more entries than exist empties the stack.
func (k *kittyKeyboard) pop(n int) {
	k.stack = k.stack[:max(0, len(k.stack)-n)]
}

// set is CSI = flags ; mode u: 1 replaces, 2 sets bits, 3 clears bits.
func (k *kittyKeyboard) set(flags, mode int) {
	if mode < 1 || mode > 3 {
		return
	}
	if len(k.stack) == 0 {
		k.stack = append(k.stack, 0)
	}
	top := &k.stack[len(k.stack)-1]
	flags &= kittyFlagMask
	switch mode {
	case 1:
		*top = flags
	case 2:
		*top |= flags
	case 3:
		*top &^= flags
	}
}

// kittyFlags returns the keyboard flags the program on the active screen
// asked for.
func (e *Emulator) kittyFlags() int {
	if e.scr == &e.scrs[1] {
		return e.kitty[1].flags()
	}
	return e.kitty[0].flags()
}

// resetKeyboard drops every keyboard protocol request.
func (e *Emulator) resetKeyboard() {
	e.kitty = [2]kittyKeyboard{}
	e.modifyOtherKeys = 0
}

// registerKeyboardHandlers registers the sequences programs use to ask for
// extended keys: the kitty keyboard protocol and xterm's modifyOtherKeys.
func (e *Emulator) registerKeyboardHandlers() {
	active := func() *kittyKeyboard {
		if e.scr == &e.scrs[1] {
			return &e.kitty[1]
		}
		return &e.kitty[0]
	}

	e.RegisterCsiHandler(ansi.Command('>', 0, 'u'), func(params ansi.Params) bool {
		flags, _, _ := params.Param(0, 0)
		active().push(flags)
		return true
	})
	e.RegisterCsiHandler(ansi.Command('<', 0, 'u'), func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		active().pop(n)
		return true
	})
	e.RegisterCsiHandler(ansi.Command('=', 0, 'u'), func(params ansi.Params) bool {
		flags, _, _ := params.Param(0, 0)
		mode, _, _ := params.Param(1, 1)
		active().set(flags, mode)
		return true
	})
	e.RegisterCsiHandler(ansi.Command('?', 0, 'u'), func(params ansi.Params) bool {
		_, _ = fmt.Fprintf(e.pw, "\x1b[?%du", e.kittyFlags())
		return true
	})
	e.RegisterCsiHandler(ansi.Command('>', 0, 'm'), func(params ansi.Params) bool {
		// XTMODKEYS: only resource 4, modifyOtherKeys. Omitting the value
		// resets it.
		if res, _, _ := params.Param(0, 0); res != 4 {
			return false
		}
		level, _, _ := params.Param(1, 0)
		e.modifyOtherKeys = min(max(level, 0), 2)
		return true
	})
}

// encodeKey encodes a key press that needs more than the legacy single-byte
// table: any modified key, and every key the program asked to have reported
// through the kitty keyboard protocol. It reports false to leave the key to
// the legacy table in SendKey.
func (e *Emulator) encodeKey(k KeyPressEvent) (string, bool) {
	mods := int(k.Mod & modMask)
	if flags := e.kittyFlags(); flags&(kittyDisambiguate|kittyAllKeys) != 0 {
		if seq, ok := kittyKey(k, mods, flags); ok {
			return seq, true
		}
	} else if mods != 0 && e.modifyOtherKeys > 0 {
		if seq, ok := otherKey(k, mods, e.modifyOtherKeys); ok {
			return seq, true
		}
	}
	if mods == 0 {
		return "", false
	}
	return legacyModifiedKey(k, mods)
}

// fkey describes how a function key is written with modifiers: CSI 1;mod final
// when final is set, otherwise CSI num;mod ~.
type fkey struct {
	final byte
	num   int
}

var functionKeys = map[rune]fkey{
	KeyUp: {final: 'A'}, KeyDown: {final: 'B'}, KeyRight: {final: 'C'}, KeyLeft: {final: 'D'},
	KeyBegin: {final: 'E'}, KeyEnd: {final: 'F'}, KeyHome: {final: 'H'},
	KeyInsert: {num: 2}, KeyDelete: {num: 3}, KeyPgUp: {num: 5}, KeyPgDown: {num: 6},
	KeyF1: {final: 'P'}, KeyF2: {final: 'Q'}, KeyF3: {final: 'R'}, KeyF4: {final: 'S'},
	KeyF5: {num: 15}, KeyF6: {num: 17}, KeyF7: {num: 18}, KeyF8: {num: 19},
	KeyF9: {num: 20}, KeyF10: {num: 21}, KeyF11: {num: 23}, KeyF12: {num: 24},
	KeyF13: {num: 25}, KeyF14: {num: 26}, KeyF15: {num: 28}, KeyF16: {num: 29},
	KeyF17: {num: 31}, KeyF18: {num: 32}, KeyF19: {num: 33}, KeyF20: {num: 34},
}

// functionKey writes a modified cursor, editing or function key. The kitty
// protocol writes F3 as CSI 13;mod ~ because CSI 1;mod R collides with the
// cursor position report.
func functionKey(code rune, mods int, kitty bool) (string, bool) {
	f, ok := functionKeys[code]
	if !ok {
		return "", false
	}
	if kitty && code == KeyF3 {
		f = fkey{num: 13}
	}
	if f.final != 0 {
		return "\x1b[1;" + strconv.Itoa(mods+1) + string(f.final), true
	}
	return "\x1b[" + strconv.Itoa(f.num) + ";" + strconv.Itoa(mods+1) + "~", true
}

// isTextKey reports whether code is a printable Unicode key rather than a
// named key.
func isTextKey(code rune) bool {
	return code >= ' ' && code != 0x7f && code <= unicode.MaxRune
}

func isEditKey(code rune) bool {
	return code == KeyEscape || code == KeyEnter || code == KeyTab || code == KeyBackspace
}

// keyText is the text a printable key types: its Text when it is a single
// rune (the shifted character), otherwise its key code, upper-cased under
// Shift (a key press with no text, as bmux send --key S-a makes). Bounded so
// a key never writes more than the server reserves for it.
func keyText(k KeyPressEvent) string {
	if utf8.RuneCountInString(k.Text) == 1 {
		return k.Text
	}
	if k.Mod&ModShift != 0 {
		return string(unicode.ToUpper(k.Code))
	}
	return string(k.Code)
}

// kittyKey writes a key for a program that enabled the kitty keyboard
// protocol's disambiguate or report-all-keys flag.
func kittyKey(k KeyPressEvent, mods, flags int) (string, bool) {
	code := k.Code
	allKeys := flags&kittyAllKeys != 0
	if mods != 0 {
		if seq, ok := functionKey(code, mods, true); ok {
			return seq, true
		}
	}
	switch {
	case code == KeyEscape:
		// Always escape-coded, so a lone Esc is not mistaken for the start
		// of a sequence.
	case isEditKey(code):
		if mods == 0 && !allKeys {
			return "", false
		}
	case isTextKey(code):
		if mods&^int(ModShift) == 0 && !allKeys {
			return "", false
		}
		// The key code is the unshifted key; the Shift modifier carries
		// the rest.
		if mods&int(ModShift) != 0 && unicode.IsUpper(code) {
			code = unicode.ToLower(code)
		}
	default:
		return "", false
	}

	var b strings.Builder
	b.WriteString("\x1b[")
	b.WriteString(strconv.Itoa(int(code)))
	text := ""
	// Associated text is single-rune: that bounds the sequence length the
	// server reserves for a key.
	if allKeys && flags&kittyAssociatedText != 0 && mods&^int(ModShift) == 0 &&
		isTextKey(code) && utf8.RuneCountInString(k.Text) == 1 {
		text = strconv.Itoa(int([]rune(k.Text)[0]))
	}
	if mods != 0 || text != "" {
		b.WriteByte(';')
		if mods != 0 {
			b.WriteString(strconv.Itoa(mods + 1))
		}
	}
	if text != "" {
		b.WriteByte(';')
		b.WriteString(text)
	}
	b.WriteByte('u')
	return b.String(), true
}

// otherKey writes a modified key for a program that enabled xterm's
// modifyOtherKeys: CSI 27;mod;code ~. Level 1 covers only keys the legacy
// encoding cannot express; level 2 covers every modified key.
func otherKey(k KeyPressEvent, mods, level int) (string, bool) {
	code := k.Code
	edit := isEditKey(code)
	if !edit && !isTextKey(code) {
		return "", false
	}
	if !edit && mods&^int(ModShift) == 0 {
		return "", false
	}
	if level < 2 {
		// Level 1 leaves keys with a legacy encoding alone: Enter, Tab,
		// Backspace and Esc, Alt-prefixed text and Ctrl-letter codes.
		if edit {
			return "", false
		}
		if mods&^int(ModShift|ModAlt|ModCtrl) == 0 && (mods&int(ModCtrl) == 0 || hasCtrlCode(code)) {
			return "", false
		}
	}
	return "\x1b[27;" + strconv.Itoa(mods+1) + ";" + strconv.Itoa(int(code)) + "~", true
}

// hasCtrlCode reports whether Ctrl+code has a C0 control character in the
// legacy encoding.
func hasCtrlCode(code rune) bool {
	switch {
	case code >= 'a' && code <= 'z', code >= 'A' && code <= 'Z':
		return true
	}
	switch code {
	case ' ', '@', '[', '\\', ']', '^', '_':
		return true
	}
	return false
}

// legacyModifiedKey writes a modified key for a program that asked for no
// extended keys, the way xterm does. Keys legacy terminals cannot tell apart
// from the plain key (Shift+Enter, Ctrl+Enter) are written as the plain key.
func legacyModifiedKey(k KeyPressEvent, mods int) (string, bool) {
	if seq, ok := functionKey(k.Code, mods, false); ok {
		return seq, true
	}
	alt := ""
	if mods&int(ModAlt) != 0 {
		alt = "\x1b"
	}
	rest := mods &^ int(ModAlt)
	switch {
	case k.Code == KeyEnter:
		return alt + "\r", true
	case k.Code == KeyTab:
		if rest&int(ModShift) != 0 {
			return alt + "\x1b[Z", true
		}
		return alt + "\t", true
	case k.Code == KeyBackspace:
		if rest&int(ModCtrl) != 0 {
			return alt + "\x08", true
		}
		return alt + "\x7f", true
	case k.Code == KeyEscape:
		return alt + "\x1b", true
	case isTextKey(k.Code) && rest&^int(ModShift) == 0:
		// Alt prefixes the text; Shift is already in the text.
		return alt + keyText(k), true
	}
	return "", false
}
