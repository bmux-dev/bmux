package vt

import (
	"fmt"
	"image/color"
	"io"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Placeholder is the Kitty graphics protocol's Unicode placeholder: a cell
// starting with it shows part of the image whose id is in its foreground color.
const Placeholder = "\U0010EEEE"

// Graphics handles Kitty graphics commands the emulator receives.
type Graphics interface {
	// Kitty runs one command, the payload of an APC G sequence without the
	// G, and returns the reply for the app, or "".
	Kitty(payload []byte) string
}

// IsPlaceholder reports whether cell content is an image placeholder cell,
// which holds no text: copies, searches and captures leave it out.
func IsPlaceholder(content string) bool {
	return strings.HasPrefix(content, Placeholder)
}

// SetGraphics installs the handler for APC G sequences. Without one they are
// dropped.
func (e *Emulator) SetGraphics(g Graphics) {
	e.graphics = g
}

// SetCellPixels sets the size of a character cell in pixels, for the window
// size reports apps ask for. 0, 0 means unknown, which leaves those queries
// unanswered.
func (e *Emulator) SetCellPixels(width, height int) {
	e.cellWidth, e.cellHeight = max(width, 0), max(height, 0)
}

// CellPixels returns the size set by SetCellPixels.
func (e *Emulator) CellPixels() (width, height int) {
	return e.cellWidth, e.cellHeight
}

func (e *Emulator) registerGraphicsHandlers() {
	e.RegisterApcHandler(func(data []byte) bool {
		if len(data) == 0 || data[0] != 'G' || e.graphics == nil {
			return false
		}
		if reply := e.graphics.Kitty(data[1:]); reply != "" {
			_, _ = io.WriteString(e.pw, reply)
		}
		return true
	})

	e.RegisterCsiHandler('t', func(params ansi.Params) bool {
		// Window manipulation reports [ansi.XTWINOPS]: only the size queries.
		op, _, _ := params.Param(0, 0)
		cw, ch := e.cellWidth, e.cellHeight
		switch {
		case op == 14 && cw > 0 && ch > 0:
			_, _ = fmt.Fprintf(e.pw, "\x1b[4;%d;%dt", e.Height()*ch, e.Width()*cw)
		case op == 16 && cw > 0 && ch > 0:
			_, _ = fmt.Fprintf(e.pw, "\x1b[6;%d;%dt", ch, cw)
		case op == 18:
			_, _ = fmt.Fprintf(e.pw, "\x1b[8;%d;%dt", e.Height(), e.Width())
		default:
			return false
		}
		return true
	})
}

// pixelSize is the text area in pixels, 0, 0 when the cell size is unknown.
func (e *Emulator) pixelSize() (width, height int) {
	return e.Width() * e.cellWidth, e.Height() * e.cellHeight
}

// PlaceholderImageIDs lists the image ids that placeholder cells on either
// screen or in scrollback show.
func (e *Emulator) PlaceholderImageIDs() map[uint32]struct{} {
	ids := make(map[uint32]struct{})
	note := func(c *uv.Cell) {
		if c != nil && IsPlaceholder(c.Content) {
			ids[PlaceholderID(c.Style.Fg)] = struct{}{}
		}
	}
	for i := range e.scrs {
		scr := &e.scrs[i]
		for y := 0; y < scr.Height(); y++ {
			for x := 0; x < scr.Width(); x++ {
				note(scr.CellAt(x, y))
			}
		}
	}
	if sb := e.Scrollback(); sb != nil {
		for _, line := range sb.Lines() {
			for x := range line {
				note(&line[x])
			}
		}
	}
	return ids
}

// PlaceholderID is the image id a Placeholder cell's foreground color
// carries: the 24-bit color, or the palette index of a 256-color one.
func PlaceholderID(fg color.Color) uint32 {
	switch c := fg.(type) {
	case nil:
		return 0
	case ansi.BasicColor:
		return uint32(c)
	case ansi.IndexedColor:
		return uint32(c)
	}
	r, g, b, _ := fg.RGBA()
	return r>>8<<16 | g>>8<<8 | b>>8
}
