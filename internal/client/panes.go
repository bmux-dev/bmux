package client

import (
	"fmt"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func (u *ui) applyFrame(f *protocol.Frame) {
	if u.selections == nil {
		u.selections = make(map[int]selection)
	}
	alive := make(map[int]bool, len(f.Panes))
	for _, p := range f.Panes {
		alive[p.ID] = true
	}
	if d := u.divider; d != nil && (!alive[d.first] || !alive[d.second]) {
		u.divider = nil
	}
	if !f.Zoomed {
		for id := range u.selections {
			if !alive[id] {
				delete(u.selections, id)
				delete(u.paneSizes, id)
			}
		}
		for id := range u.paneSizes {
			if !alive[id] {
				delete(u.paneSizes, id)
			}
		}
	}
	if !alive[u.focused] {
		u.selection = selection{}
		u.appMouseDown = false
		u.clickCount = 0
		u.resizeUntil = time.Time{}
		u.focused = 0
	}
	// Resize can move terminal text. Don't retain a highlight that now covers
	// different cells and would copy the old cached text instead of what is shown.
	if u.paneSizes == nil {
		u.paneSizes = make(map[int][2]int)
	}
	for _, p := range f.Panes {
		size := [2]int{p.Width, p.Height}
		if old, ok := u.paneSizes[p.ID]; ok && old != size {
			delete(u.selections, p.ID)
			if p.ID == u.focused {
				u.selection = selection{}
			}
		}
		u.paneSizes[p.ID] = size
	}
	if alive[u.focused] {
		u.selections[u.focused] = u.selection
	}
	u.workspace, u.name = f, f.Name
	u.activatePane(f.Focused)
	u.noteEdge(f)
	u.noteConfig(f)
	u.noteFirstAttach()
	if f.Notice != "" {
		// Longer than other notices: it can name a file and what to do.
		u.message, u.messageUntil = f.Notice, time.Now().Add(15*time.Second)
	}
	if !alive[u.focused] {
		u.frame = nil
		if len(f.Panes) > 0 {
			u.activatePane(f.Panes[0].ID)
		}
	}
	for _, p := range f.Panes {
		if p.ID == u.focused {
			u.extendToEdge(p.Terminal)
			u.selection.remember(p.Terminal)
			u.selections[p.ID] = u.selection
		} else {
			s := u.selections[p.ID]
			s.remember(p.Terminal)
			u.selections[p.ID] = s
		}
	}
	u.noteViewFrame(u.frame)
}

func (u *ui) activatePane(id int) {
	if u.workspace == nil {
		return
	}
	for _, p := range u.workspace.Panes {
		if p.ID != id {
			continue
		}
		if id != u.focused {
			if u.selections == nil {
				u.selections = make(map[int]selection)
			}
			if u.focused != 0 {
				u.selections[u.focused] = u.selection
			}
			u.selection = u.selections[id]
			u.clickCount = 0
		}
		u.focused, u.frame = id, p.Terminal
		return
	}
}

// noteEdge moves keyboard focus into the sidebar when directional navigation
// runs off the left edge of the layout. The first frame of an attachment only
// records the count, so an older edge never steals focus on attach.
func (u *ui) noteEdge(f *protocol.Frame) {
	if !u.edgeKnown {
		u.edge, u.edgeKnown = f.Edge, true
		return
	}
	if f.Edge == u.edge {
		return
	}
	u.edge = f.Edge
	if f.EdgeDirection != "left" || u.picker || u.screen == nil {
		return
	}
	if w, _ := u.screen.Size(); u.sidebarShown(w) {
		u.focusSidebar()
	}
}

func (u *ui) focusedPlacement() protocol.PaneFrame {
	if u.workspace != nil {
		for _, p := range u.workspace.Panes {
			if p.ID == u.focused {
				return p
			}
		}
	}
	w, h := u.screen.Size()
	return protocol.PaneFrame{ID: u.focused, Width: u.paneViewportWidth(w), Height: paneViewportHeight(h), Terminal: u.frame}
}

func (u *ui) drawPane(p protocol.PaneFrame, w, h int) {
	f := p.Terminal
	// Layout coordinates start right of the sidebar; draw in screen columns.
	p.X += u.paneOriginX(w)
	if f == nil || p.Width < 1 || p.Height < 1 {
		return
	}
	sel := u.selections[p.ID]
	if p.ID == u.focused {
		sel = u.selection
	}
	viewing := u.view.active && p.ID == u.view.pane
	for y, row := range f.Rows {
		if y >= p.Height || paneOriginY+p.Y+y >= h-1 {
			break
		}
		if paneOriginY+p.Y+y < paneOriginY {
			continue
		}
		var matches []bool
		if viewing {
			matches = searchMatches(row, u.view.query)
		}
		for x, c := range row {
			if x >= p.Width || p.X+x >= w {
				break
			}
			if p.X+x < 0 {
				continue
			}
			if c.Width == 0 && x > 0 && row[x-1].Width > 1 {
				continue
			}
			style := cellStyle(c)
			if x < len(matches) && matches[x] {
				style = style.Background(accentColor).Foreground(chrome.background)
			}
			if sel.contains(x, f.Base+y) || (viewing && u.view.selected(x, f.Base+y)) {
				style = style.Background(selectionBackground).Foreground(selectionForeground)
			}
			if viewing && u.view.cursor == (point{x, f.Base + y}) {
				style = style.Reverse(true)
			}
			text := c.Text
			if c.Attrs&uv.AttrConceal != 0 {
				text = strings.Repeat(" ", max(1, c.Width))
			}
			if text == "" || x+max(1, c.Width) > p.Width || p.X+x+max(1, c.Width) > w {
				text = " "
			}
			u.screen.Put(p.X+x, paneOriginY+p.Y+y, text, style)
		}
	}
	if p.ID == u.focused && !viewing && f.CursorVisible && !f.Exited && f.CursorX >= 0 && f.CursorY >= 0 && f.CursorX < p.Width && f.CursorY < p.Height && p.X+f.CursorX >= 0 && p.X+f.CursorX < w && paneOriginY+p.Y+f.CursorY >= paneOriginY && paneOriginY+p.Y+f.CursorY < h-1 {
		styles := []tcell.CursorStyle{tcell.CursorStyleBlinkingBlock, tcell.CursorStyleBlinkingUnderline, tcell.CursorStyleBlinkingBar}
		style := styles[max(0, min(f.CursorStyle, 2))]
		if !f.CursorBlink {
			style++
		}
		u.screen.SetCursorStyle(style)
		u.screen.ShowCursor(p.X+f.CursorX, paneOriginY+p.Y+f.CursorY)
	}
}

// focusedPaneLabel lives in chrome, never in application cells. Pane numbers
// describe the current frame's order rather than exposing server identifiers.
func (u *ui) focusedPaneLabel() string {
	if u.workspace == nil || len(u.workspace.Panes) == 0 {
		return "pane 1/1"
	}
	panes := u.workspace.Panes
	width, height := 0, 0
	for _, p := range panes {
		width = max(width, p.X+p.Width)
		height = max(height, p.Y+p.Height)
	}
	for i, p := range panes {
		if p.ID != u.focused {
			continue
		}
		label := fmt.Sprintf("pane %d/%d", i+1, len(panes))
		var location []string
		if p.Y > 0 || p.Y+p.Height < height {
			if 2*p.Y+p.Height <= height {
				location = append(location, "upper")
			} else {
				location = append(location, "lower")
			}
		}
		if p.X > 0 || p.X+p.Width < width {
			if 2*p.X+p.Width <= width {
				location = append(location, "left")
			} else {
				location = append(location, "right")
			}
		}
		if len(location) > 0 {
			label += " · " + strings.Join(location, " ")
		}
		return label
	}
	return "pane unavailable"
}
