package server

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	"github.com/josephferrero/bmux/internal/graphics"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/vt"
)

// images holds every pane's images, with the server-wide cap.
var images = graphics.NewStore(graphics.DefaultLimits)

// trackGraphics gives the pane its own image space: Kitty graphics commands
// from its program are stored, never passed to an attached terminal.
func (s *pane) trackGraphics(store *graphics.Store) {
	s.graphics = store.NewPane(graphics.Hooks{
		Supported:  s.graphicsOK.Load,
		CellPixels: s.term.CellPixels,
		Referenced: func(owned bool) (map[uint32]struct{}, bool) {
			if !owned {
				// Another pane's output may be running; never wait for it.
				if !s.mu.TryLock() {
					return nil, false
				}
				defer s.mu.Unlock()
			}
			return s.term.PlaceholderImageIDs(), true
		},
	})
	s.term.SetGraphics(kittyGraphics{s.graphics})
}

// kittyGraphics adapts graphics.Pane to the emulator's handler interface.
type kittyGraphics struct{ pane *graphics.Pane }

func (g kittyGraphics) Kitty(payload []byte) string { return g.pane.Handle(payload) }

// setGraphics records what the attached clients can display and the size of
// their cells in pixels, which answers apps' size queries and fills the PTY's
// pixel size. cellWidth and cellHeight are 0 when no client has said.
func (s *pane) setGraphics(supported bool, cellWidth, cellHeight int) error {
	s.graphicsOK.Store(supported)
	s.mu.Lock()
	defer s.mu.Unlock()
	if w, h := s.term.CellPixels(); w == cellWidth && h == cellHeight {
		return nil
	}
	s.term.SetCellPixels(cellWidth, cellHeight)
	if s.info.Exited {
		return nil
	}
	return resizePTY(s.pty, s.term.Width(), s.term.Height(), cellWidth, cellHeight)
}

// frameImages remaps a frame's placeholder cells from the program's image ids
// to server-wide ones, which the attached terminal sees, and collects the
// images they show. nil when the pane has no placed images.
type frameImages struct {
	pane  *graphics.Pane
	byApp map[uint32]graphics.Info
	shown map[uint32]graphics.Info // by server-wide id
}

func (s *pane) frameImages() *frameImages {
	if s.graphics == nil {
		return nil
	}
	byApp := s.graphics.Snapshot()
	if len(byApp) == 0 {
		return nil
	}
	return &frameImages{pane: s.graphics, byApp: byApp, shown: make(map[uint32]graphics.Info)}
}

// remap rewrites a converted placeholder cell. A cell whose image is gone, or
// not placed, draws blank. Only cells starting with the placeholder rune are
// touched: anything else keeps its colors, whatever they say.
func (f *frameImages) remap(cell protocol.Cell, fg color.Color) protocol.Cell {
	if !strings.HasPrefix(cell.Text, vt.Placeholder) {
		return cell
	}
	var info graphics.Info
	ok := false
	if f != nil {
		info, ok = f.byApp[vt.PlaceholderID(fg)]
	}
	if !ok {
		cell.Text, cell.Width, cell.FG, cell.Underline = " ", 1, "", false
		return cell
	}
	f.shown[info.ID] = info
	cell.FG = fmt.Sprintf("#%06x", info.ID)
	return cell
}

// finish lists the shown images in the frame, oldest id first, and marks them
// used so eviction prefers others.
func (f *frameImages) finish(frame *protocol.TerminalFrame) {
	if f == nil || len(f.shown) == 0 {
		return
	}
	ids := make([]uint32, 0, len(f.shown))
	for id := range f.shown {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	f.pane.Touch(ids)
	for _, id := range ids {
		info := f.shown[id]
		frame.Images = append(frame.Images, protocol.Image{ID: int(id), Width: info.Width, Height: info.Height, Cols: info.Cols, Rows: info.Rows})
	}
}

// clientGraphics is what one attached client's terminal can display.
type clientGraphics struct {
	caps protocol.Graphics
	seq  uint64 // newer reports win ties
}

// maxCellPixels bounds a reported cell size; anything bigger is a lie.
const maxCellPixels = 512

// reportGraphics records what client c's terminal can display, from its attach
// or latest resize, and applies the session-wide result to every pane. A nil
// report means nothing is known.
func (s *session) reportGraphics(c *clientGraphics, g *protocol.Graphics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var caps protocol.Graphics
	if g != nil {
		caps = *g
	}
	if caps.CellWidth < 1 || caps.CellWidth > maxCellPixels || caps.CellHeight < 1 || caps.CellHeight > maxCellPixels {
		caps.CellWidth, caps.CellHeight = 0, 0
	}
	s.graphicsSeq++
	c.caps, c.seq = caps, s.graphicsSeq
	if s.graphicsClients == nil {
		s.graphicsClients = make(map[*clientGraphics]struct{})
	}
	s.graphicsClients[c] = struct{}{}
	s.applyGraphicsLocked()
}

// dropGraphics forgets a detached client.
func (s *session) dropGraphics(c *clientGraphics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.graphicsClients, c)
	s.applyGraphicsLocked()
}

// applyGraphicsLocked tells the panes whether some client can display images,
// and the cell size of the best client to size them for: one that shows images
// over one that doesn't, then the most recent report. With no client the last
// size stays, since programs may still hold sizes computed from it.
func (s *session) applyGraphicsLocked() {
	var best *clientGraphics
	supported := false
	for c := range s.graphicsClients {
		supported = supported || c.caps.Kitty
		if c.caps.CellWidth == 0 {
			continue
		}
		if best == nil || (c.caps.Kitty && !best.caps.Kitty) || (c.caps.Kitty == best.caps.Kitty && c.seq > best.seq) {
			best = c
		}
	}
	s.graphicsSupported = supported
	if best != nil {
		s.cellWidth, s.cellHeight = best.caps.CellWidth, best.caps.CellHeight
	}
	for _, p := range s.panes {
		_ = p.setGraphics(supported, s.cellWidth, s.cellHeight)
	}
}

// imageChunk answers image-data: up to size bytes of an image's PNG from offset.
func imageChunk(r protocol.Request) (*protocol.ImageChunk, error) {
	data, ok := images.Data(r.ImageID)
	if !ok {
		return nil, fmt.Errorf("image not found")
	}
	if r.Offset < 0 || r.Offset > len(data) {
		return nil, fmt.Errorf("image offset out of range")
	}
	size := r.Size
	if size <= 0 || size > protocol.MaxImageChunk {
		size = protocol.MaxImageChunk
	}
	end := min(len(data), r.Offset+size)
	return &protocol.ImageChunk{ID: int(r.ImageID), Total: len(data), Offset: r.Offset, Data: data[r.Offset:end]}, nil
}
