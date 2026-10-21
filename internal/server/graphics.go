package server

import (
	"github.com/josephferrero/bmux/internal/graphics"
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
