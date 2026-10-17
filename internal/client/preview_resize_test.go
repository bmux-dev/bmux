package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestPreviewResizeWaitsForNewViewportGeometry(t *testing.T) {
	u := pickerUI(t)
	u.requestPreview()
	oldEpoch := u.previewEpoch
	_, oldArea := pickerAreas(100, 24)
	f := &protocol.Frame{Name: "api", PreviewMode: "terminal-viewport", Width: oldArea.w, Height: oldArea.h, Panes: []protocol.PaneFrame{{ID: 1, Width: oldArea.w, Height: oldArea.h, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("CACHED_OUTPUT")}, CursorVisible: true}}}}
	u.acceptPreview(previewResult{epoch: oldEpoch, name: "api", frame: f})
	u.screen.(tcell.SimulationScreen).SetSize(380, 150)
	u.requestPreview()
	_, area := pickerAreas(380, 150)
	if u.previewEpoch == oldEpoch || u.previewSize != [2]int{area.w, area.h} || u.preview != nil {
		t.Fatal("resize didn't invalidate old requests and hide old-size cells")
	}
	u.acceptPreview(previewResult{epoch: oldEpoch, name: "api", frame: splitFrame()})
	if u.preview != nil {
		t.Fatal("old-epoch result was shown while resize was pending")
	}
	u.draw()
	if text, _, _ := u.screen.Get(area.x, area.y); text != "L" {
		t.Fatal("panel did not show loading before the sized frame arrived")
	}
	if _, _, visible := u.screen.(tcell.SimulationScreen).GetCursor(); visible {
		t.Fatal("old-size cursor flashed while loading")
	}
	// A snapshot laid out for a different panel must not be displayed here.
	u.acceptPreview(previewResult{epoch: u.previewEpoch, name: "api", frame: f})
	u.draw()
	if text, _, _ := u.screen.Get(area.x, area.y); text != "W" {
		t.Fatal("mismatched same-epoch frame was displayed")
	}
	sized := *f
	sized.Width, sized.Height = area.w, area.h
	sized.Panes = []protocol.PaneFrame{{ID: 1, Width: area.w, Height: area.h, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("SIZED_OUTPUT")}, CursorVisible: true}}}
	u.acceptPreview(previewResult{epoch: u.previewEpoch, name: "api", frame: &sized})
	u.draw()
	if text, _, _ := u.screen.Get(area.x, area.y); text != "S" {
		t.Fatal("correctly sized frame wasn't revealed")
	}
	// The native view stays below the list at both window sizes.
	u.screen.(tcell.SimulationScreen).SetSize(60, 20)
	u.requestPreview()
	_, area = pickerAreas(60, 20)
	if area.y <= 4 || u.previewSize != [2]int{area.w, area.h} {
		t.Fatal("narrow-window preview didn't move below list")
	}
}

func TestPreviewGeometryGateHonorsServerBounds(t *testing.T) {
	f := &protocol.Frame{PreviewMode: "terminal-viewport", Width: protocol.MaxPreviewWidth, Height: protocol.MaxPreviewHeight, Panes: []protocol.PaneFrame{{ID: 1}}}
	if !previewFitsPanel(f, pickerArea{w: 5000, h: 3000}) {
		t.Fatal("clamped server size never becomes visible in large panel")
	}
	if previewFitsPanel(f, pickerArea{w: 80, h: 24}) {
		t.Fatal("oversized source accepted for small panel")
	}
	u := pickerUI(t)
	u.preview = f
	u.screen.HideCursor()
	u.drawTerminalView(pickerArea{1, 13, 80, 8})
	if _, _, visible := u.screen.(tcell.SimulationScreen).GetCursor(); visible {
		t.Fatal("mismatched source stole cursor")
	}
}
