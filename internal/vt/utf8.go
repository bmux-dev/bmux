package vt

import (
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const (
	// maxGraphemeBytes bounds buffered non-ASCII runes/combining marks before
	// forcing segmentation so adversarial sequences cannot grow memory or trigger
	// quadratic clustering passes.
	maxGraphemeBytes    = 256
	graphemeShrinkLimit = 4096
)

// printableASCII reports whether r is a character that can never join the
// grapheme cluster beside it. No ASCII character is a combining mark, a ZWJ, a
// spacing mark or a regional indicator, and CR and LF are not printable, so a
// cluster boundary between two of these is certain without asking the
// segmenter.
func printableASCII(r rune) bool {
	return r >= ansi.SP && r < ansi.DEL
}

// writeASCIISpan writes a contiguous run of printable ASCII bytes (0x20..0x7e)
// directly into the current screen rows without parser state transitions,
// grapheme segmentation, or heap allocations.
// Caller guarantees: gsingle == 0, charsets[gl] == nil, !insert,
// scr.cur.Link.IsZero(), and len(e.grapheme) == 0.
func (e *Emulator) writeASCIISpan(span []byte) {
	scr := e.scr
	w := scr.width
	if w <= 0 || scr.height <= 0 || len(span) == 0 {
		return
	}

	styleID := scr.curStyleID
	awm := e.autoWrap
	oldPos := scr.cur.Position
	x, y := scr.cur.X, scr.cur.Y
	e.lastChar = rune(span[len(span)-1])

	for len(span) > 0 {
		if e.atPhantom && awm {
			scr.setFlag(y, FlagWrapped, true)
			scr.cur.X = x
			e.index()
			_, y = scr.CursorPosition()
			x = 0
		}

		row := scr.rows[y]
		avail := w - x
		n := min(len(span), avail)

		// Clean up any wide characters straddling the overwritten span edges.
		if row[x].Width() == 0 && row[x].IsZero() {
			scr.clearWideOverlap(y, x)
		}
		if endCol := x + n - 1; row[endCol].Width() > 1 {
			scr.clearWideOverlap(y, endCol)
		}
		if len(scr.extras) > 0 {
			scr.clearRowExtrasRange(y, x, x+n)
		}

		dst := row[x : x+n]
		src := span[:n]
		for k, b := range src {
			dst[k] = Cell{Rune: rune(b), StyleID: styleID, Flags: cellWidth1}
		}
		scr.bumpRow(y)

		span = span[n:]
		if !awm {
			if len(span) > 0 {
				// Without autowrap, remaining bytes overwrite the last column.
				lastCol := w - 1
				if row[lastCol].Width() != 1 {
					scr.clearWideOverlap(y, lastCol)
				} else if row[lastCol].HasExtra() {
					scr.clearCellExtra(y, lastCol)
				}
				row[lastCol] = Cell{Rune: rune(span[len(span)-1]), StyleID: styleID, Flags: cellWidth1}
				x = lastCol
				e.atPhantom = false
				break
			}
			x += n
			if x >= w {
				x = w - 1
			}
			e.atPhantom = false
			break
		}

		if x+n >= w {
			x = w - 1
			e.atPhantom = true
		} else {
			x += n
			e.atPhantom = false
		}
	}

	scr.cur.X, scr.cur.Y = x, y
	if scr.cb != nil && scr.cb.CursorPosition != nil && (oldPos.X != x || oldPos.Y != y) {
		scr.cb.CursorPosition(oldPos, uv.Pos(x, y))
	}
}

// handlePrint handles printable characters.
//
// A character is held back rather than printed straight away, because the one
// after it may be a combining mark that belongs to the same cluster. Printing
// "e" from "e\u0301" before the mark arrives puts the base in one cell and
// leaves the mark to become a zero-width cell of its own, which the next write
// or erase then destroys.
func (e *Emulator) handlePrint(r rune) {
	// No ASCII character can extend the cluster before it: none of them is
	// Extend, ZWJ, SpacingMark, Prepend, a regional indicator, or part of an
	// Indic conjunct. So an ASCII character arriving is proof that whatever is
	// buffered is finished, and the buffer can go out.
	//
	// This is also what keeps the buffer from being re-segmented as it grows.
	// Asking where the clusters are on every character costs the length of the
	// buffer each time, which is quadratic over a long run of combining marks
	// that never resolves into more than one cluster.
	if printableASCII(r) {
		// Two printable ASCII characters in a row is the common case by a wide
		// margin, and needs no segmenting at all.
		if len(e.grapheme) == 1 && printableASCII(rune(e.grapheme[0])) {
			e.handleSingleRune(rune(e.grapheme[0]), 1)
			e.grapheme[0] = byte(r)
			return
		}
		e.flushGrapheme()
	} else if len(e.grapheme) >= maxGraphemeBytes {
		e.flushGrapheme()
	}

	e.grapheme = utf8.AppendRune(e.grapheme, r)
}

// flushGrapheme flushes the current grapheme buffer, if any, and handles the
// grapheme as a single unit.
func (e *Emulator) flushGrapheme() {
	if len(e.grapheme) == 0 {
		return
	}

	// Fast-path single buffered ASCII byte without invoking grapheme segmenter.
	if len(e.grapheme) == 1 && printableASCII(rune(e.grapheme[0])) {
		b := e.grapheme[0]
		e.grapheme = e.grapheme[:0]
		e.handleSingleRune(rune(b), 1)
		return
	}

	// XXX: We always use [ansi.GraphemeWidth] here to report accurate widths
	// and it's up to the caller to decide how to handle Unicode vs non-Unicode
	// modes.
	method := ansi.GraphemeWidth
	graphemes := e.grapheme
	for len(graphemes) > 0 {
		cluster, width := ansi.FirstGraphemeCluster(graphemes, method)
		if len(cluster) == 0 {
			break
		}
		e.handleGraphemeBytes(cluster, width)
		graphemes = graphemes[len(cluster):]
	}
	if cap(e.grapheme) > graphemeShrinkLimit {
		e.grapheme = nil
	} else {
		e.grapheme = e.grapheme[:0] // Reset the grapheme buffer.
	}
}

// handleSingleRune writes a single-rune grapheme directly without allocating a string.
func (e *Emulator) handleSingleRune(r rune, width int) {
	e.writeCell(r, "", width)
}

// handleGraphemeBytes writes a UTF-8 grapheme cluster, avoiding heap string
// allocation whenever the cluster is a single rune.
func (e *Emulator) handleGraphemeBytes(cluster []byte, width int) {
	r, size := utf8.DecodeRune(cluster)
	if size == len(cluster) {
		e.writeCell(r, "", width)
		return
	}
	e.writeCell(r, string(cluster), width)
}

// writeCell places a single rune (grapheme == "") or multi-rune cluster
// (grapheme != "", first rune in r) at the cursor, handling autowrap,
// charsets, insert mode, wide-char boundaries, and phantom cursor state.
func (e *Emulator) writeCell(r rune, grapheme string, width int) {
	awm := e.autoWrap
	scr := e.scr

	// A single shift applies to the next character, whatever that character
	// turns out to be. Take it now, so it cannot leak onto the one after this
	// when this is a cluster no charset has a mapping for.
	single := e.gsingle
	e.gsingle = 0

	// Handle character set mappings for single-byte characters.
	if grapheme == "" && r >= 0 && r < 256 {
		var charset CharSet
		c := byte(r)
		if single > 1 && single < 4 {
			charset = e.charsets[single]
		} else if c < 128 {
			charset = e.charsets[e.gl]
		} else {
			charset = e.charsets[e.gr]
		}

		if charset != nil {
			if mapped, ok := charset[c]; ok {
				mr, msize := utf8.DecodeRuneInString(mapped)
				r = mr
				if msize < len(mapped) {
					grapheme = mapped
				}
				width = 1
			}
		}
	}

	if width == 1 && grapheme == "" && r < 128 {
		e.lastChar = r
	}

	x, y := scr.CursorPosition()
	if e.atPhantom && awm {
		// bmux: the row the cursor leaves continues onto the next one.
		scr.setFlag(y, FlagWrapped, true)
		// moves cursor down similar to [Terminal.linefeed] except it doesn't
		// respect [ansi.LNM] mode.
		// This will reset the phantom state i.e. pending wrap state.
		e.index()
		_, y = scr.CursorPosition()
		x = 0
	} else if awm && width > 1 && width <= scr.width && x > 0 && x+width > scr.width {
		// Wide character does not fit in the remaining column(s) of this row:
		// wrap to the next row rather than splitting or dropping it.
		scr.setFlag(y, FlagWrapped, true)
		e.index()
		_, y = scr.CursorPosition()
		x = 0
	}

	if e.insert {
		// Insert mode: push the rest of the line right, up to the right margin.
		scr.insertCellArea(x, y, max(1, width), scr.blankCompactCell(), scr.scroll)
	}

	flags := cellWidthFlags(width)
	var ex cellExtra
	hasExtra := false
	if grapheme != "" {
		flags |= cellFlagGrapheme
		ex.grapheme = grapheme
		hasExtra = true
	}
	if link := scr.cursorLink(); !link.IsZero() {
		flags |= cellFlagLink
		ex.link = link
		hasExtra = true
	}

	cell := Cell{
		Rune:    r,
		StyleID: scr.curStyleID,
		Flags:   flags,
	}
	scr.setCompactCell(x, y, cell, ex, hasExtra)

	// Handle phantom state at the end of the line (including wide chars at width-2).
	cw := max(1, width)
	e.atPhantom = awm && x+cw >= scr.width
	if e.atPhantom {
		x = scr.width - 1
	} else {
		x += width
	}

	// NOTE: We don't reset the phantom state here, we handle it up above.
	scr.setCursor(x, y, false)
}
