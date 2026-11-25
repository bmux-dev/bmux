package client

import (
	"github.com/bmux-dev/bmux/internal/protocol"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
)

// The server lays out panel-sized viewports before sending native source cells.
// This renderer never resizes applications, scales fonts, or reflows content.
func (u *ui) drawTerminalView(area pickerArea) {
	if u.preview == nil || len(u.preview.Panes) == 0 || area.w <= 0 || area.h <= 0 {
		return
	}
	if u.preview.PreviewMode == "terminal-viewport" {
		if !previewFitsPanel(u.preview, area) {
			return
		}
		// These are presentation rectangles, not the actual PTY dimensions.
		for _, p := range u.preview.Panes {
			if p.X < 0 || p.Y < 0 {
				continue
			}
			paneArea := pickerArea{area.x + p.X, area.y + p.Y, min(p.Width, area.w-p.X), min(p.Height, area.h-p.Y)}
			u.drawTerminalCells(paneArea, p.Terminal, p.ID == u.preview.Focused)
		}
		u.drawDividers(u.preview, area)
		u.fillUncovered(u.preview, area, uiBaseStyle)
		return
	}
	p := u.preview.Panes[0]
	for _, candidate := range u.preview.Panes {
		if candidate.ID == u.preview.Focused {
			p = candidate
			break
		}
	}
	u.drawTerminalCells(area, p.Terminal, true)
}

func (u *ui) drawTerminalCells(area pickerArea, f *protocol.TerminalFrame, focused bool) {
	if area.w <= 0 || area.h <= 0 {
		return
	}
	if f == nil {
		u.fillRect(area.x, area.y, area.w, area.h, uiBaseStyle)
		return
	}
	rowStart, columnStart := 0, 0
	if sourceCursorVisible(f) {
		rowStart = protocol.ViewportOrigin(f.CursorY, len(f.Rows), area.h)
		width := 0
		for _, row := range f.Rows {
			width = max(width, len(row))
		}
		columnStart = protocol.ViewportOrigin(f.CursorX, width, area.w)
	}
	graphics := u.graphicsOn()
	var markers imageMarkers
	if !graphics {
		markers = placeholderMarkers(f)
	}
	drawnRows := 0
	for y := 0; y < area.h && rowStart+y < len(f.Rows); y++ {
		drawnRows = y + 1
		row := f.Rows[rowStart+y]
		col := 0
		for x := 0; x < area.w && columnStart+x < len(row); x++ {
			index := columnStart + x
			c := row[index]
			if c.Width == 0 && index > 0 && row[index-1].Width > 1 {
				continue
			}
			text := c.Text
			if text == "" || c.Attrs&uv.AttrConceal != 0 || x+max(1, c.Width) > area.w {
				text = " "
			}
			if !graphics && isPlaceholder(c) {
				c.FG = "" // the image id isn't a color to show
			}
			style := cellStyle(c)
			if isPlaceholder(c) && text == c.Text {
				text, style = u.placeholderCell(c, markers, index, rowStart+y, style)
			}
			_, cellW := u.screen.Put(area.x+x, area.y+y, text, style)
			if cellW < 1 {
				cellW = 1
			}
			if x+cellW > col {
				col = x + cellW
			}
		}
		for x := col; x < area.w; x++ {
			u.screen.Put(area.x+x, area.y+y, " ", uiBaseStyle)
		}
	}
	if drawnRows < area.h {
		u.fillRect(area.x, area.y+drawnRows, area.w, area.h-drawnRows, uiBaseStyle)
	}
	// Only a plain list can lend the real terminal cursor to the view. Search
	// and prompts retain their own cursor; no synthetic blink phase is painted.
	if focused && !u.searching && u.prompt == "" && sourceCursorVisible(f) {
		x, y := f.CursorX-columnStart, f.CursorY-rowStart
		if x >= 0 && y >= 0 && x < area.w && y < area.h {
			styles := []tcell.CursorStyle{tcell.CursorStyleSteadyBlock, tcell.CursorStyleSteadyUnderline, tcell.CursorStyleSteadyBar}
			u.screen.SetCursorStyle(styles[max(0, min(f.CursorStyle, 2))])
			u.screen.ShowCursor(area.x+x, area.y+y)
		}
	}
}
