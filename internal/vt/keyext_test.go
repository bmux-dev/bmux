package vt

import "testing"

// press sends k and returns what the emulator wrote to the app.
func press(term *Emulator, read func() string, k KeyPressEvent) string {
	done := make(chan struct{})
	go func() {
		term.SendKey(k)
		close(done)
	}()
	got := read()
	<-done
	return got
}

func TestModifiedKeysByKeyboardMode(t *testing.T) {
	enter := func(m KeyMod) KeyPressEvent { return KeyPressEvent{Code: KeyEnter, Mod: m} }
	text := func(r rune, m KeyMod) KeyPressEvent { return KeyPressEvent{Code: r, Text: string(r), Mod: m} }
	ctrl := func(r rune) KeyPressEvent { return KeyPressEvent{Code: r, Mod: ModCtrl} }

	tests := []struct {
		name  string
		setup string
		key   KeyPressEvent
		want  string
	}{
		// Programs that asked for nothing get what legacy terminals send.
		{"legacy shift enter is enter", "", enter(ModShift), "\r"},
		{"legacy ctrl enter is enter", "", enter(ModCtrl), "\r"},
		{"legacy alt enter", "", enter(ModAlt), "\x1b\r"},
		{"legacy shift tab", "", KeyPressEvent{Code: KeyTab, Mod: ModShift}, "\x1b[Z"},
		{"legacy shift up", "", KeyPressEvent{Code: KeyUp, Mod: ModShift}, "\x1b[1;2A"},
		{"legacy shift letter without text", "", KeyPressEvent{Code: 'a', Mod: ModShift}, "A"},
		{"legacy ctrl right", "", KeyPressEvent{Code: KeyRight, Mod: ModCtrl}, "\x1b[1;5C"},
		{"legacy alt left", "", KeyPressEvent{Code: KeyLeft, Mod: ModAlt}, "\x1b[1;3D"},
		{"legacy ctrl shift home", "", KeyPressEvent{Code: KeyHome, Mod: ModCtrl | ModShift}, "\x1b[1;6H"},
		{"legacy shift delete", "", KeyPressEvent{Code: KeyDelete, Mod: ModShift}, "\x1b[3;2~"},
		{"legacy ctrl page down", "", KeyPressEvent{Code: KeyPgDown, Mod: ModCtrl}, "\x1b[6;5~"},
		{"legacy shift f1", "", KeyPressEvent{Code: KeyF1, Mod: ModShift}, "\x1b[1;2P"},
		{"legacy ctrl f5", "", KeyPressEvent{Code: KeyF5, Mod: ModCtrl}, "\x1b[15;5~"},
		{"legacy shift f13", "", KeyPressEvent{Code: KeyF13, Mod: ModShift}, "\x1b[25;2~"},
		{"legacy ctrl backspace", "", KeyPressEvent{Code: KeyBackspace, Mod: ModCtrl}, "\x08"},
		{"legacy alt letter", "", text('x', ModAlt), "\x1bx"},
		{"legacy shifted letter", "", text('A', ModShift), "A"},
		{"legacy ctrl letter", "", ctrl('c'), "\x03"},
		{"legacy alt ctrl letter", "", KeyPressEvent{Code: 'c', Mod: ModAlt | ModCtrl}, "\x1b\x03"},

		// xterm modifyOtherKeys.
		{"modifyOtherKeys 2 shift enter", "\x1b[>4;2m", enter(ModShift), "\x1b[27;2;13~"},
		{"modifyOtherKeys 2 ctrl enter", "\x1b[>4;2m", enter(ModCtrl), "\x1b[27;5;13~"},
		{"modifyOtherKeys 2 shift tab", "\x1b[>4;2m", KeyPressEvent{Code: KeyTab, Mod: ModShift}, "\x1b[27;2;9~"},
		{"modifyOtherKeys 2 ctrl letter", "\x1b[>4;2m", ctrl('i'), "\x1b[27;5;105~"},
		{"modifyOtherKeys 2 alt letter", "\x1b[>4;2m", text('x', ModAlt), "\x1b[27;3;120~"},
		{"modifyOtherKeys 2 shifted letter stays text", "\x1b[>4;2m", text('A', ModShift), "A"},
		{"modifyOtherKeys 2 keeps arrows xterm style", "\x1b[>4;2m", KeyPressEvent{Code: KeyUp, Mod: ModCtrl}, "\x1b[1;5A"},
		{"modifyOtherKeys 1 leaves shift enter legacy", "\x1b[>4;1m", enter(ModShift), "\r"},
		{"modifyOtherKeys 1 leaves ctrl letter legacy", "\x1b[>4;1m", ctrl('c'), "\x03"},
		{"modifyOtherKeys 1 encodes ctrl digit", "\x1b[>4;1m", KeyPressEvent{Code: '1', Mod: ModCtrl}, "\x1b[27;5;49~"},
		{"modifyOtherKeys reset", "\x1b[>4;2m\x1b[>4m", enter(ModShift), "\r"},
		{"other XTMODKEYS resources ignored", "\x1b[>1;2m", enter(ModShift), "\r"},

		// Kitty keyboard protocol.
		{"kitty shift enter", "\x1b[>1u", enter(ModShift), "\x1b[13;2u"},
		{"kitty ctrl enter", "\x1b[>1u", enter(ModCtrl), "\x1b[13;5u"},
		{"kitty alt shift enter", "\x1b[>1u", enter(ModAlt | ModShift), "\x1b[13;4u"},
		{"kitty plain enter stays legacy", "\x1b[>1u", enter(0), "\r"},
		{"kitty plain tab stays legacy", "\x1b[>1u", KeyPressEvent{Code: KeyTab}, "\t"},
		{"kitty shift tab", "\x1b[>1u", KeyPressEvent{Code: KeyTab, Mod: ModShift}, "\x1b[9;2u"},
		{"kitty escape", "\x1b[>1u", KeyPressEvent{Code: KeyEscape}, "\x1b[27u"},
		{"kitty ctrl backspace", "\x1b[>1u", KeyPressEvent{Code: KeyBackspace, Mod: ModCtrl}, "\x1b[127;5u"},
		{"kitty ctrl letter", "\x1b[>1u", ctrl('c'), "\x1b[99;5u"},
		{"kitty alt letter", "\x1b[>1u", text('x', ModAlt), "\x1b[120;3u"},
		{"kitty alt shift letter lowercases code", "\x1b[>1u", text('X', ModAlt|ModShift), "\x1b[120;4u"},
		{"kitty plain text stays text", "\x1b[>1u", text('a', 0), "a"},
		{"kitty shifted text stays text", "\x1b[>1u", text('A', ModShift), "A"},
		{"kitty shift up", "\x1b[>1u", KeyPressEvent{Code: KeyUp, Mod: ModShift}, "\x1b[1;2A"},
		{"kitty shift f3", "\x1b[>1u", KeyPressEvent{Code: KeyF3, Mod: ModShift}, "\x1b[13;2~"},
		{"kitty unmodified up stays legacy", "\x1b[>1u", KeyPressEvent{Code: KeyUp}, "\x1b[A"},
		{"kitty all keys enter", "\x1b[>9u", enter(0), "\x1b[13u"},
		{"kitty all keys text", "\x1b[>9u", text('a', 0), "\x1b[97u"},
		{"kitty all keys shifted text", "\x1b[>9u", text('A', ModShift), "\x1b[97;2u"},
		{"kitty associated text", "\x1b[>25u", text('a', 0), "\x1b[97;;97u"},
		{"kitty associated text shifted", "\x1b[>25u", text('A', ModShift), "\x1b[97;2;65u"},
		{"kitty flags without disambiguate stay legacy", "\x1b[>2u", enter(ModShift), "\r"},
		{"kitty wins over modifyOtherKeys", "\x1b[>4;2m\x1b[>1u", enter(ModShift), "\x1b[13;2u"},

		// The flag stack.
		{"kitty pop restores legacy", "\x1b[>1u\x1b[<u", enter(ModShift), "\r"},
		{"kitty pop restores previous flags", "\x1b[>1u\x1b[>0u\x1b[<u", enter(ModShift), "\x1b[13;2u"},
		{"kitty pop more than pushed empties the stack", "\x1b[>1u\x1b[>1u\x1b[<5u", enter(ModShift), "\r"},
		{"kitty set replaces", "\x1b[>1u\x1b[=8u", enter(0), "\x1b[13u"},
		{"kitty set ORs flags", "\x1b[=1;1u\x1b[=8;2u", enter(0), "\x1b[13u"},
		{"kitty set clears flags", "\x1b[>9u\x1b[=8;3u", enter(0), "\r"},
		{"kitty set with no stack", "\x1b[=1u", enter(ModShift), "\x1b[13;2u"},
		{"kitty stack is per screen", "\x1b[>1u\x1b[?1049h", enter(ModShift), "\r"},
		{"kitty main stack survives the alt screen", "\x1b[>1u\x1b[?1049h\x1b[>1u\x1b[?1049l", enter(ModShift), "\x1b[13;2u"},
		{"kitty alt stack starts fresh each time", "\x1b[?1049h\x1b[>1u\x1b[?1049l\x1b[?1049h", enter(ModShift), "\r"},
		{"RIS drops every request", "\x1b[>1u\x1b[>4;2m\x1bc", enter(ModShift), "\r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := newTestTerminal(t, 20, 5)
			read := replies(term)
			if tt.setup != "" {
				term.WriteString(tt.setup)
			}
			if got := press(term, read, tt.key); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKittyFlagQueryReportsTheActiveScreen(t *testing.T) {
	term := newTestTerminal(t, 20, 5)
	read := replies(term)
	query := func() string {
		done := make(chan struct{})
		go func() {
			term.WriteString("\x1b[?u")
			close(done)
		}()
		got := read()
		<-done
		return got
	}
	if got := query(); got != "\x1b[?0u" {
		t.Fatalf("fresh terminal: got %q", got)
	}
	term.WriteString("\x1b[>5u")
	if got := query(); got != "\x1b[?5u" {
		t.Fatalf("after push: got %q", got)
	}
	term.WriteString("\x1b[?1049h")
	if got := query(); got != "\x1b[?0u" {
		t.Fatalf("alt screen: got %q", got)
	}
	term.WriteString("\x1b[?1049l")
	if got := query(); got != "\x1b[?5u" {
		t.Fatalf("back on main: got %q", got)
	}
}

func TestKittyStackIsBounded(t *testing.T) {
	var k kittyKeyboard
	for range 10 * kittyStackLimit {
		k.push(kittyDisambiguate)
	}
	if len(k.stack) != kittyStackLimit {
		t.Fatalf("stack grew to %d, limit %d", len(k.stack), kittyStackLimit)
	}
	k.push(kittyAllKeys)
	if k.flags() != kittyAllKeys || len(k.stack) != kittyStackLimit {
		t.Fatalf("push at the limit: flags %d, depth %d", k.flags(), len(k.stack))
	}
}
