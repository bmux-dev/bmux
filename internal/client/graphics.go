package client

import (
	"github.com/gdamore/tcell/v2"

	"github.com/josephferrero/bmux/internal/protocol"
)

func (u *ui) initImages() {
	u.imageResults = make(chan imageResult, 8)
	u.imageWake = make(chan struct{}, 1)
	u.images = newImageSender(u.graphics.write, func(id uint32) ([]byte, error) { return fetchImage(u.socket, id) }, u.imageResults, u.done)
}

// graphicsOn reports whether this client draws images: the terminal answered
// the Kitty query (ui.graphics = "auto") or the user said it does ("on"), and
// tcell can still send exact colors, which carry image ids.
func (u *ui) graphicsOn() bool {
	if u.graphics.write == nil || exactColorOff() {
		return false
	}
	switch u.settings.UI.Graphics {
	case "on":
		return true
	case "off":
		return false
	}
	return u.graphics.probe.kitty
}

// graphicsReport is what the server is told on attach and resize: whether
// this client shows images, and the size of its cells in pixels.
func (u *ui) graphicsReport() *protocol.Graphics {
	var width, height int
	if u.graphics.cellPixels != nil {
		width, height = u.graphics.cellPixels()
	}
	on := u.graphicsOn()
	if !on && width == 0 {
		return nil
	}
	return &protocol.Graphics{Kitty: on, CellWidth: width, CellHeight: height}
}

// terminalColors is the terminal's default foreground and background, which
// the server hands to programs that ask for them; nil when it didn't answer.
func (u *ui) terminalColors() *protocol.TerminalColors {
	p := u.graphics.probe
	if p.foreground == "" && p.background == "" {
		return nil
	}
	return &protocol.TerminalColors{Foreground: p.foreground, Background: p.background}
}

// sendResize tells the server the size of the pane area and of one cell.
func (u *ui) sendResize() {
	w, h := u.screen.Size()
	u.send(protocol.Request{Op: "resize", Width: u.paneViewportWidth(w), Height: paneViewportHeight(h), Graphics: u.graphicsReport()})
}

// shownImages lists the images the current screen's frames show, once each.
func (u *ui) shownImages() []protocol.Image {
	var frames []*protocol.TerminalFrame
	switch {
	case u.picker:
		if u.preview != nil && u.preview.PreviewMode != "" {
			for _, p := range u.preview.Panes {
				frames = append(frames, p.Terminal)
			}
		} else if u.preview != nil && len(u.preview.Panes) > 0 {
			// Like drawTerminalView: the focused pane, else the first.
			shown := u.preview.Panes[0]
			for _, p := range u.preview.Panes {
				if p.ID == u.preview.Focused {
					shown = p
					break
				}
			}
			frames = append(frames, shown.Terminal)
		}
	case u.workspace != nil:
		for _, p := range u.workspace.Panes {
			frames = append(frames, p.Terminal)
		}
	case u.frame != nil:
		frames = append(frames, u.frame)
	}
	var images []protocol.Image
	seen := make(map[int]bool)
	for _, f := range frames {
		if f == nil {
			continue
		}
		for _, img := range f.Images {
			if !seen[img.ID] {
				seen[img.ID] = true
				images = append(images, img)
			}
		}
	}
	return images
}

// drawImages puts the screen's images on the terminal ahead of the cells that
// show them. Placeholder cells draw blank until their image arrives.
func (u *ui) drawImages() {
	if u.images == nil || !u.graphicsOn() {
		return
	}
	images := u.shownImages()
	u.images.want(images)
	more, err := u.images.flush(images)
	if err != nil {
		u.notice("Images: " + err.Error())
	}
	if more {
		select {
		case u.imageWake <- struct{}{}:
		default:
		}
	}
}

// placeholderCell is what draws a placeholder cell. With images on, the cell
// keeps its exact colors, which select the image; selection and search colors
// would hide it. Without, it is blank, or a letter of the image's marker.
func (u *ui) placeholderCell(c protocol.Cell, markers imageMarkers, x, y int, style tcell.Style) (string, tcell.Style) {
	if u.graphicsOn() {
		return c.Text, cellStyle(c)
	}
	return markers.at(x, y), style
}
