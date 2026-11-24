package vt

import (
	"bytes"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func typeHasPointers(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.String, reflect.Slice,
		reflect.Map, reflect.Chan, reflect.Func, reflect.Interface:
		return true
	case reflect.Array:
		return typeHasPointers(t.Elem())
	case reflect.Struct:
		for i := range t.NumField() {
			if typeHasPointers(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

func TestCompactCellSizeAndZeroPointers(t *testing.T) {
	cellType := reflect.TypeOf(Cell{})
	if got := cellType.Size(); got > 16 {
		t.Fatalf("Cell size = %d bytes, want <= 16", got)
	}
	t.Logf("Cell size = %d bytes, Style size = %d bytes, sbRow size = %d bytes",
		cellType.Size(), reflect.TypeOf(Style{}).Size(), reflect.TypeOf(sbRow{}).Size())

	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{"Cell", cellType},
		{"Style", reflect.TypeOf(Style{})},
		{"Color", reflect.TypeOf(Color(0))},
		{"sbRow", reflect.TypeOf(sbRow{})},
	} {
		if typeHasPointers(tc.typ) {
			t.Errorf("%s contains GC-scanned pointer fields", tc.name)
		}
	}
}

func TestScrollbackFootprint10kBy120(t *testing.T) {
	const (
		cols  = 120
		rows  = 40
		lines = 10000
	)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	term := NewEmulator(cols, rows)
	term.SetScrollbackSize(lines)
	line := append(bytes.Repeat([]byte("A"), cols-2), '\r', '\n')
	for range lines + rows {
		_, _ = term.Write(line)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if term.ScrollbackLen() != lines {
		t.Fatalf("ScrollbackLen = %d, want %d", term.ScrollbackLen(), lines)
	}
	// Verify oldest and newest lines have full 118 'A's.
	for _, idx := range []int{0, lines / 2, lines - 1} {
		rc := term.ScrollbackRowCells(idx)
		if len(rc) != cols-2 || rc[0].Rune != 'A' || rc[len(rc)-1].Rune != 'A' {
			t.Fatalf("scrollback row %d corrupted: len=%d", idx, len(rc))
		}
	}

	var delta uint64
	if after.HeapAlloc > before.HeapAlloc {
		delta = after.HeapAlloc - before.HeapAlloc
	}
	t.Logf("10k x 120 scrollback live heap delta: %.2f MiB (%d bytes)", float64(delta)/(1024*1024), delta)
	// Compact 12B cells * 1.2M = ~14.4 MiB (vs ~115+ MiB with uv.Cell).
	if delta > 30<<20 {
		t.Fatalf("10k x 120 scrollback heap delta = %.2f MiB, want < 30 MiB", float64(delta)/(1024*1024))
	}
	runtime.KeepAlive(term)
}

func TestAltScreenZeroScrollbackGrowth(t *testing.T) {
	term := NewEmulator(80, 24)
	if term.scrs[1].Scrollback() != nil {
		t.Fatal("alt screen must not allocate a scrollback buffer")
	}
	if term.scrs[1].HasScrollback() {
		t.Fatal("alt screen HasScrollback() = true, want false")
	}
	if !term.HasScrollback() {
		t.Fatal("main screen HasScrollback() = false, want true")
	}

	// Put some content in main scrollback first.
	for i := range 30 {
		_, _ = term.WriteString(fmt.Sprintf("main %d\r\n", i))
	}
	mainLen := term.ScrollbackLen()
	mainScrollCount := term.ScrollCount()
	mainTotal := term.ScrollbackTotal()

	// Switch to alt screen and hammer scrolling, ED 2, and ED 3.
	_, _ = term.WriteString("\x1b[?1049h")
	for i := range 500 {
		_, _ = term.WriteString(fmt.Sprintf("alt %d\r\n", i))
	}
	_, _ = term.WriteString("\x1b[2J\x1b[3J")

	if term.scrs[1].Scrollback() != nil {
		t.Fatal("alt screen acquired a scrollback buffer after scrolling/ED")
	}
	if term.ScrollCount() != mainScrollCount {
		t.Fatalf("main ScrollCount changed during alt screen: got %d, want %d", term.ScrollCount(), mainScrollCount)
	}
	if term.ScrollbackLen() != mainLen || term.ScrollbackTotal() != mainTotal {
		t.Fatalf("main scrollback changed during alt screen: len=%d/%d total=%d/%d",
			term.ScrollbackLen(), mainLen, term.ScrollbackTotal(), mainTotal)
	}

	// Resize and Reset on alt screen must keep alt scrollback nil and never panic.
	term.Resize(60, 20)
	term.scrs[1].Reset()
	if term.scrs[1].Scrollback() != nil {
		t.Fatal("alt screen acquired a scrollback buffer after Resize/Reset")
	}
}

func TestParserLazyDataBuffer(t *testing.T) {
	t.Run("new emulator avoids 4MiB eager allocation", func(t *testing.T) {
		var allocs [16]*Emulator
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := range allocs {
			allocs[i] = NewEmulator(80, 24)
		}
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		var delta uint64
		if after.HeapAlloc > before.HeapAlloc {
			delta = after.HeapAlloc - before.HeapAlloc
		}
		t.Logf("16 idle 80x24 emulators heap delta: %.2f KiB", float64(delta)/1024)
		// Previously 16 * 4 MiB = 64 MiB for parser buffers alone!
		if delta > 4<<20 {
			t.Fatalf("16 idle emulators allocated %.2f MiB, expected lazy parser buffer", float64(delta)/(1024*1024))
		}
		runtime.KeepAlive(allocs)
	})

	t.Run("large split APC payload grows on demand and shrinks after dispatch", func(t *testing.T) {
		term := NewEmulator(40, 10)
		var got []byte
		term.RegisterApcHandler(func(data []byte) bool {
			got = append([]byte(nil), data...)
			return true
		})

		payload := "G" + strings.Repeat("ABCD", 32*1024) // ~128 KiB
		seq := "\x1b_" + payload + "\x1b\\"
		mid := len(seq) / 2
		_, _ = term.WriteString(seq[:mid])
		_, _ = term.WriteString(seq[mid:])

		if string(got) != payload {
			t.Fatalf("APC payload length = %d, want %d", len(got), len(payload))
		}
		if term.parserDataLen != 0 {
			t.Fatalf("parserDataLen after dispatch = %d, want 0", term.parserDataLen)
		}

		// Subsequent normal output works cleanly.
		_, _ = term.WriteString("hello")
		if c := term.CellAt(0, 0); c == nil || c.Content != "h" {
			t.Fatalf("post-APC output failed: got %+v", c)
		}
	})

	t.Run("oversized payload is capped at 4MiB", func(t *testing.T) {
		term := NewEmulator(40, 10)
		var gotLen int
		term.RegisterApcHandler(func(data []byte) bool {
			gotLen = len(data)
			return true
		})

		chunk := bytes.Repeat([]byte("X"), 1<<20) // 1 MiB
		_, _ = term.WriteString("\x1b_G")
		for range 5 { // 5 MiB total > 4 MiB cap
			_, _ = term.Write(chunk)
		}
		_, _ = term.WriteString("\x1b\\ok")

		if gotLen != maxParserDataSize {
			t.Fatalf("capped APC payload len = %d, want %d", gotLen, maxParserDataSize)
		}
		if c := term.CellAt(0, 0); c == nil || c.Content != "o" {
			t.Fatalf("post-capped-APC output failed: got %+v", c)
		}
	})
}

func TestSideTableCleanupOnOverwriteAndEviction(t *testing.T) {
	term := NewEmulator(20, 3)
	term.SetScrollbackSize(2)

	// Write a combining cluster and an OSC 8 hyperlink on row 0.
	_, _ = term.WriteString("e\u0301\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\")
	if !term.RowCells(0)[0].HasGrapheme() || term.CellGrapheme(0, 0) != "e\u0301" {
		t.Fatalf("expected grapheme on (0,0), got %+v %q", term.RowCells(0)[0], term.CellGrapheme(0, 0))
	}
	if !term.RowCells(0)[1].HasLink() || term.CellLink(1, 0).URL != "https://example.com" {
		t.Fatalf("expected link on (1,0), got %+v %+v", term.RowCells(0)[1], term.CellLink(1, 0))
	}

	// Overwrite with plain ASCII: row 0 side-table must become empty/removed.
	_, _ = term.WriteString("\x1b[1;1Hplain text")
	if len(term.scrs[0].extras) != 0 {
		t.Fatalf("screen extras leaked after ASCII overwrite: %+v", term.scrs[0].extras)
	}

	// Push hyperlinked/grapheme lines into scrollback and then evict them.
	_, _ = term.WriteString("\x1b[1;1He\u0301\x1b]8;;https://evict.me\x1b\\x\x1b]8;;\x1b\\\r\n\r\n\r\n")
	sb := term.Scrollback()
	if len(sb.extras) == 0 {
		t.Fatal("expected scrollback extras for scrolled grapheme/link row")
	}
	// Evict with plain lines (maxLines == 2).
	_, _ = term.WriteString("a\r\nb\r\nc\r\nd\r\n")
	if len(sb.extras) != 0 {
		t.Fatalf("scrollback extras leaked after eviction: %+v", sb.extras)
	}
}

func TestReflowEdgeCasesAndClearReleasesMemory(t *testing.T) {
	t.Run("zero dimensions never panic", func(t *testing.T) {
		term := NewEmulator(10, 4)
		_, _ = term.WriteString("hello world")
		term.Resize(0, 4)
		term.Resize(10, 0)
		term.Resize(0, 0)
		term.Resize(10, 4)
		_, _ = term.WriteString("ok")
		if got := term.CellAt(0, 0); got == nil || got.Content != "o" {
			t.Fatalf("after zero-size resize round-trip got %+v", got)
		}
	})

	t.Run("wide char at width-1 wraps and wide char at width-2 sets phantom", func(t *testing.T) {
		// Width 5: "abcd" puts cursor at x=4 (width-1); "界" (width 2) must wrap cleanly.
		term := NewEmulator(5, 3)
		_, _ = term.WriteString("abcd界x")
		expectLines(t, "wide char at width-1", screenLines(term), []string{"abcd", "界x", ""})
		if !term.LineWrapped(0) {
			t.Fatal("expected row 0 wrapped when wide char wrapped to row 1")
		}

		// Width 5: "abc界" puts wide char at cols 3..4 (width-2..width-1), setting phantom.
		term = NewEmulator(5, 3)
		_, _ = term.WriteString("abc界")
		if !term.atPhantom {
			t.Fatal("expected atPhantom after wide char filled columns width-2..width-1")
		}
		_, _ = term.WriteString("z")
		expectLines(t, "char after wide phantom", screenLines(term), []string{"abc界", "z", ""})

		// Reflow after wide char at width-2 with phantom cursor places cursor right after it.
		term = NewEmulator(5, 3)
		_, _ = term.WriteString("abc界")
		term.Resize(10, 3)
		expectLines(t, "widened after wide phantom", screenLines(term), []string{"abc界", "", ""})
		expectCursor(t, term, 5, 0)
	})

	t.Run("scrollback Clear frees pages and zeroes ring", func(t *testing.T) {
		term := NewEmulator(80, 5)
		term.SetScrollbackSize(100)
		for i := range 200 {
			_, _ = term.WriteString(fmt.Sprintf("line %d content\r\n", i))
		}
		sb := term.Scrollback()
		if len(sb.pages) == 0 {
			t.Fatal("expected allocated pages before Clear")
		}
		term.ClearScrollback()
		if sb.Len() != 0 || len(sb.pages) != 0 || len(sb.extras) != 0 {
			t.Fatalf("Clear did not release state: len=%d pages=%d extras=%d", sb.Len(), len(sb.pages), len(sb.extras))
		}
		for i, slot := range sb.ring {
			if slot != (sbRow{}) {
				t.Fatalf("ring slot %d not zeroed after Clear: %+v", i, slot)
			}
		}
	})
}

func TestRowRevAndScrollCount(t *testing.T) {
	term := NewEmulator(20, 4)
	term.SetScrollbackSize(5)

	// Initial RowRevs are all distinct and non-zero.
	initial := make([]uint64, 4)
	seen := make(map[uint64]bool)
	for y := range 4 {
		r := term.RowRev(y)
		if r == 0 || seen[r] {
			t.Fatalf("initial RowRev(%d) = %d not unique non-zero", y, r)
		}
		seen[r] = true
		initial[y] = r
	}

	// Writing to row 0 bumps ONLY row 0.
	_, _ = term.WriteString("row0")
	if term.RowRev(0) <= initial[0] {
		t.Fatalf("RowRev(0) did not advance: %d <= %d", term.RowRev(0), initial[0])
	}
	for y := 1; y < 4; y++ {
		if term.RowRev(y) != initial[y] {
			t.Fatalf("RowRev(%d) unexpectedly changed: %d != %d", y, term.RowRev(y), initial[y])
		}
	}

	// Fill rows 1..3 and capture revs before scroll.
	_, _ = term.WriteString("\r\nrow1\r\nrow2\r\nrow3")
	beforeScrollRevs := []uint64{term.RowRev(0), term.RowRev(1), term.RowRev(2), term.RowRev(3)}
	beforeScrollCount := term.ScrollCount()
	beforeTotal := term.ScrollbackTotal()

	// Scroll up 1 line and write row4 on the new bottom row.
	_, _ = term.WriteString("\r\nrow4")
	if got := term.ScrollCount() - beforeScrollCount; got != 1 {
		t.Fatalf("ScrollCount delta = %d, want 1", got)
	}
	if got := term.ScrollbackTotal() - beforeTotal; got != 1 {
		t.Fatalf("ScrollbackTotal delta = %d, want 1", got)
	}
	// Wave 2 ScrollUp delta check: rows 0..2 match shifted beforeScrollRevs[1..3],
	// and only row 3 has a fresh revision.
	for y := range 3 {
		if term.RowRev(y) != beforeScrollRevs[y+1] {
			t.Fatalf("shifted RowRev(%d) = %d, want %d", y, term.RowRev(y), beforeScrollRevs[y+1])
		}
	}
	if term.RowRev(3) <= beforeScrollRevs[3] {
		t.Fatalf("new bottom RowRev(3) = %d did not advance", term.RowRev(3))
	}

	// Keep scrolling past maxLines=5: ScrollbackLen caps at 5 while ScrollCount &
	// ScrollbackTotal continue advancing monotonically.
	for i := range 10 {
		_, _ = term.WriteString(fmt.Sprintf("\r\nmore %d", i))
	}
	if term.ScrollbackLen() != 5 {
		t.Fatalf("ScrollbackLen = %d, want 5", term.ScrollbackLen())
	}
	if term.ScrollCount() != 11 || term.ScrollbackTotal() != 11 {
		t.Fatalf("ScrollCount=%d ScrollbackTotal=%d, want 11/11", term.ScrollCount(), term.ScrollbackTotal())
	}

	// Epoch increments on ClearScrollback and on width reflow.
	ep0 := term.ScrollbackEpoch()
	term.ClearScrollback()
	ep1 := term.ScrollbackEpoch()
	if ep1 <= ep0 {
		t.Fatalf("ScrollbackEpoch did not increment on ClearScrollback: %d -> %d", ep0, ep1)
	}
	term.Resize(30, 4)
	ep2 := term.ScrollbackEpoch()
	if ep2 <= ep1 {
		t.Fatalf("ScrollbackEpoch did not increment on Resize reflow: %d -> %d", ep1, ep2)
	}
}

func TestStyleTableInterningAndCompaction(t *testing.T) {
	term := NewEmulator(20, 4)
	// Test basic, 256-indexed, 24-bit RGB, underline, and attrs.
	_, _ = term.WriteString("\x1b[1;3;4;31;48;5;200;58;2;10;20;30mA\x1b[38;2;1;2;3mB\x1b[0mC")

	row := term.RowCells(0)
	stA := term.Style(row[0].StyleID)
	if !stA.Fg.IsBasic() || stA.Fg.Index() != 1 {
		t.Fatalf("A fg = %+v, want basic 1", stA.Fg)
	}
	if !stA.Bg.IsIndexed() || stA.Bg.Index() != 200 {
		t.Fatalf("A bg = %+v, want palette 200", stA.Bg)
	}
	if r, g, b := stA.UnderlineColor.RGB(); !stA.UnderlineColor.IsRGB() || r != 10 || g != 20 || b != 30 {
		t.Fatalf("A ul = %+v, want RGB(10,20,30)", stA.UnderlineColor)
	}
	if stA.Attrs&(uv.AttrBold|uv.AttrItalic) != uv.AttrBold|uv.AttrItalic || stA.Underline == 0 {
		t.Fatalf("A attrs/underline = %v/%v", stA.Attrs, stA.Underline)
	}
	stB := term.Style(row[1].StyleID)
	if r, g, b := stB.Fg.RGB(); !stB.Fg.IsRGB() || r != 1 || g != 2 || b != 3 {
		t.Fatalf("B fg = %+v, want RGB(1,2,3)", stB.Fg)
	}
	if row[2].StyleID != 0 {
		t.Fatalf("C StyleID = %d, want 0 (default)", row[2].StyleID)
	}

	// Verify compaction bounds the table under > styleCompactHighWater unique styles
	// while preserving live cell styles.
	verBefore := term.StyleTableVersion()
	var buf bytes.Buffer
	for i := range styleCompactHighWater + 100 {
		buf.Reset()
		fmt.Fprintf(&buf, "\x1b[1;1H\x1b[38;2;%d;%d;%dmX", (i>>16)&0xff, (i>>8)&0xff, i&0xff)
		_, _ = term.Write(buf.Bytes())
	}
	if term.StyleTableVersion() <= verBefore {
		t.Fatal("expected StyleTable compaction to bump version")
	}
	if term.styles.Len() > 1000 {
		t.Fatalf("StyleTable not compacted: len = %d", term.styles.Len())
	}
	// Last written cell at (0,0) still has exact RGB color.
	last := styleCompactHighWater + 99
	gotRGB := term.Style(term.RowCells(0)[0].StyleID).Fg
	r, g, b := gotRGB.RGB()
	if int(r) != (last>>16)&0xff || int(g) != (last>>8)&0xff || int(b) != last&0xff {
		t.Fatalf("after compaction cell RGB = (%d,%d,%d), want (%d,%d,%d)",
			r, g, b, (last>>16)&0xff, (last>>8)&0xff, last&0xff)
	}
}

func TestASCIIFastPathEdgeCases(t *testing.T) {
	t.Run("non-autowrap overwrites last column", func(t *testing.T) {
		term := NewEmulator(5, 2)
		_, _ = term.WriteString("\x1b[?7labcdefgh")
		expectLines(t, "no-wrap", screenLines(term), []string{"abcdh", ""})
		expectCursor(t, term, 4, 0)
	})

	t.Run("ASCII overwrites wide char halves cleanly", func(t *testing.T) {
		term := NewEmulator(6, 1)
		_, _ = term.WriteString("a界b")
		// Overwrite second half of '界' (col 2) with ASCII 'Z'.
		_, _ = term.WriteString("\x1b[1;3HZ")
		expectLines(t, "overwrite right half", screenLines(term), []string{"a Zb"})
		// Overwrite first half of a wide char at end of ASCII span.
		_, _ = term.WriteString("\x1b[1;1Ha界b\x1b[1;1HXY")
		expectLines(t, "overwrite left half", screenLines(term), []string{"XY b"})
	})

	t.Run("REP works after ASCII fast-path span", func(t *testing.T) {
		term := NewEmulator(10, 1)
		_, _ = term.WriteString("ab\x1b[3b")
		expectLines(t, "REP after fast-path", screenLines(term), []string{"abbbb"})
	})
}

func TestStyleTableCompactionBackoffAboveHighWater(t *testing.T) {
	// Create a screen + scrollback holding > styleCompactHighWater genuinely live
	// unique RGB styles, then verify subsequent SGR sequences do NOT re-compact
	// on every SGR (dynamic 2x high-water backoff).
	term := NewEmulator(100, 10)
	term.SetScrollbackSize(1000)

	// Populate > styleCompactHighWater unique live styles directly into scrollback
	// rows so the test runs in milliseconds even under -race.
	const liveNeeded = styleCompactHighWater + 500
	row := make([]Cell, 100)
	var rgb uint32 = 1
	for int(rgb) <= liveNeeded {
		for x := range row {
			if int(rgb) > liveNeeded {
				row[x] = EmptyCell
				continue
			}
			st := Style{Fg: RGBColor(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb))}
			id := term.styles.Intern(st)
			row[x] = Cell{Rune: 'x', StyleID: id, Flags: cellWidth1}
			rgb++
		}
		term.scrs[0].scrollback.pushCompact(row, nil, 0)
	}

	v0 := term.StyleTableVersion()
	// First SGR crosses styleCompactHighWater and triggers one compaction.
	_, _ = term.WriteString("\x1b[38;2;255;0;1mA")
	v1 := term.StyleTableVersion()
	if v1 <= v0 {
		t.Fatalf("expected initial compaction above high-water, version stayed %d", v1)
	}
	if term.nextStyleCompact < len(term.styles.styles)*2 {
		t.Fatalf("nextStyleCompact = %d, want >= %d", term.nextStyleCompact, len(term.styles.styles)*2)
	}

	// Next SGR sequences must NOT re-compact immediately.
	for i := range 20 {
		_, _ = term.WriteString(fmt.Sprintf("\x1b[38;2;255;1;%dmB", i))
	}
	if v2 := term.StyleTableVersion(); v2 != v1 {
		t.Fatalf("style table thrashed compaction on subsequent SGRs: version %d -> %d", v1, v2)
	}
}

func BenchmarkASCIIFastPathThroughput(b *testing.B) {
	term := NewEmulator(120, 40)
	term.SetScrollbackSize(10000)
	chunk := bytes.Repeat([]byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789________\r\n"), 500)
	// Warm up scrollback ring.
	for range 25 {
		_, _ = term.Write(chunk)
	}
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = term.Write(chunk)
	}
}

func BenchmarkZeroAllocRowRead(b *testing.B) {
	term := NewEmulator(120, 40)
	for y := range 40 {
		_, _ = term.WriteString(fmt.Sprintf("\x1b[38;2;%d;120;200mline %02d hello world from compact vt cells\r\n", y*5, y))
	}
	b.ReportAllocs()
	b.ResetTimer()
	var sink uint32
	for range b.N {
		for y := range term.Height() {
			row := term.RowCells(y)
			for _, c := range row {
				st := term.Style(c.StyleID)
				sink += uint32(c.Rune) + uint32(st.Fg)
			}
		}
	}
	_ = sink
}
