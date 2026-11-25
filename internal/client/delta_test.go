package client

import (
	"testing"

	"github.com/bmux-dev/bmux/internal/protocol"
	"github.com/gdamore/tcell/v2"
)

func TestClientDeltaApplyFrameAndDirtyGating(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(20, 6)
	u := &ui{screen: s, name: "api", welcomed: true}

	full := &protocol.Frame{
		Name:    "api",
		Focused: 1,
		Width:   20,
		Height:  4,
		Panes: []protocol.PaneFrame{{
			ID: 1, Width: 20, Height: 4,
			Terminal: &protocol.TerminalFrame{
				Epoch: 1, Name: "api", Base: 0,
				Rows: [][]protocol.Cell{
					cells("row-zero"),
					cells("row-one"),
					cells("row-two"),
					cells("row-three"),
				},
			},
		}},
	}

	if dirty := u.handleUpdate(update{response: protocol.Response{Frame: full}}); !dirty {
		t.Fatal("initial full frame should mark dirty")
	}
	u.draw()
	if got := simRow(s, 1, 8); got != "row-zero" {
		t.Fatalf("row 0 = %q, want %q", got, "row-zero")
	}

	// Unchanged frame must return dirty=false and preserve existing rows.
	unchanged := &protocol.Frame{
		Name:    "api",
		Focused: 1,
		Width:   20,
		Height:  4,
		Panes: []protocol.PaneFrame{{
			ID: 1, Width: 20, Height: 4,
			Terminal: &protocol.TerminalFrame{Delta: true, Unchanged: true, Name: "api"},
		}},
	}
	if dirty := u.handleUpdate(update{response: protocol.Response{Frame: unchanged}}); dirty {
		t.Fatal("all-unchanged delta frame should not mark dirty")
	}
	if u.frame == nil || u.frame.Rows[0][0].Text != "r" {
		t.Fatalf("unchanged delta lost retained rows: %+v", u.frame)
	}

	// RowDelta updates only row 2.
	rowDelta := &protocol.Frame{
		Name:    "api",
		Focused: 1,
		Width:   20,
		Height:  4,
		Panes: []protocol.PaneFrame{{
			ID: 1, Width: 20, Height: 4,
			Terminal: &protocol.TerminalFrame{
				Delta: true, Epoch: 1, Name: "api", Base: 0,
				RowDeltas: []protocol.RowDelta{
					{Y: 2, Cells: cells("EDITED-2")},
				},
			},
		}},
	}
	if dirty := u.handleUpdate(update{response: protocol.Response{Frame: rowDelta}}); !dirty {
		t.Fatal("row delta should mark dirty")
	}
	u.draw()
	if got := simRow(s, 1, 8); got != "row-zero" {
		t.Fatalf("row 0 drifted: %q", got)
	}
	if got := simRow(s, 3, 8); got != "EDITED-2" {
		t.Fatalf("row 2 = %q, want %q", got, "EDITED-2")
	}

	// ScrollUp shifts existing rows up by 1 and paints new bottom row.
	scrollDelta := &protocol.Frame{
		Name:    "api",
		Focused: 1,
		Width:   20,
		Height:  4,
		Panes: []protocol.PaneFrame{{
			ID: 1, Width: 20, Height: 4,
			Terminal: &protocol.TerminalFrame{
				Delta: true, ScrollUp: 1, Epoch: 1, Name: "api", Base: 1, History: 1,
				RowDeltas: []protocol.RowDelta{
					{Y: 3, Cells: cells("row-four")},
				},
			},
		}},
	}
	if dirty := u.handleUpdate(update{response: protocol.Response{Frame: scrollDelta}}); !dirty {
		t.Fatal("scroll delta should mark dirty")
	}
	u.draw()
	if got := simRow(s, 1, 7); got != "row-one" {
		t.Fatalf("shifted row 0 = %q, want %q", got, "row-one")
	}
	if got := simRow(s, 2, 8); got != "EDITED-2" {
		t.Fatalf("shifted row 1 = %q, want %q", got, "EDITED-2")
	}
	if got := simRow(s, 4, 8); got != "row-four" {
		t.Fatalf("new bottom row 3 = %q, want %q", got, "row-four")
	}
}

func TestClientDeltaPreservesSelectionAndNoAliasing(t *testing.T) {
	u := &ui{name: "api", welcomed: true}
	initial := &protocol.Frame{
		Name: "api", Focused: 1, Width: 20, Height: 4,
		Panes: []protocol.PaneFrame{{
			ID: 1, Width: 20, Height: 4,
			Terminal: &protocol.TerminalFrame{
				Epoch: 1, Name: "api", Base: 0,
				Rows: [][]protocol.Cell{
					cells("keep-this"),
					cells("line-1"),
					cells("line-2"),
					cells("line-3"),
				},
			},
		}},
	}
	u.applyFrame(initial)
	u.selection = selection{active: true, start: point{0, 0}, end: point{8, 0}}
	u.selection.remember(u.frame)

	// Delta mutating an unselected row must keep selection intact and exact.
	u.applyFrame(&protocol.Frame{
		Name: "api", Focused: 1, Width: 20, Height: 4,
		Panes: []protocol.PaneFrame{{
			ID: 1, Width: 20, Height: 4,
			Terminal: &protocol.TerminalFrame{
				Delta: true, Epoch: 1, Name: "api", Base: 0,
				RowDeltas: []protocol.RowDelta{{Y: 3, Cells: cells("changed!")}},
			},
		}},
	})
	if !u.selection.active || u.selection.text() != "keep-this" {
		t.Fatalf("selection broken by unrelated delta: active=%v text=%q", u.selection.active, u.selection.text())
	}

	// Delta mutating the selected row must clear selection cleanly.
	u.applyFrame(&protocol.Frame{
		Name: "api", Focused: 1, Width: 20, Height: 4,
		Panes: []protocol.PaneFrame{{
			ID: 1, Width: 20, Height: 4,
			Terminal: &protocol.TerminalFrame{
				Delta: true, Epoch: 1, Name: "api", Base: 0,
				RowDeltas: []protocol.RowDelta{{Y: 0, Cells: cells("OVERWRITE")}},
			},
		}},
	})
	if u.selection.active {
		t.Fatal("selection should clear when delta overwrites selected row")
	}
}

func TestClientDeltaStaleAckKeepsDeltaStateSynced(t *testing.T) {
	u := &ui{name: "api", welcomed: true}
	u.handleUpdate(update{response: protocol.Response{
		Ack: 0,
		Frame: &protocol.Frame{
			Name: "api", Focused: 1, Width: 20, Height: 3,
			Panes: []protocol.PaneFrame{{
				ID: 1, Width: 20, Height: 3,
				Terminal: &protocol.TerminalFrame{
					Epoch: 1, Name: "api",
					Rows: [][]protocol.Cell{cells("aaa"), cells("bbb"), cells("ccc")},
				},
			}},
		},
	}})

	// Simulate an in-flight command awaiting Ack=1.
	u.pending = 1

	// Stale frame (Ack=0 < pending=1) arrives carrying a delta on row 1.
	if dirty := u.handleUpdate(update{response: protocol.Response{
		Ack: 0,
		Frame: &protocol.Frame{
			Name: "api", Focused: 1, Width: 20, Height: 3,
			Panes: []protocol.PaneFrame{{
				ID: 1, Width: 20, Height: 3,
				Terminal: &protocol.TerminalFrame{
					Delta: true, Epoch: 1, Name: "api",
					RowDeltas: []protocol.RowDelta{{Y: 1, Cells: cells("NEW")}},
				},
			}},
		},
	}}); dirty {
		t.Fatal("stale ack frame should not mark UI dirty yet")
	}

	// Matching Ack=1 arrives with Unchanged=true (because the server already sent "NEW" in the Ack=0 frame).
	if dirty := u.handleUpdate(update{response: protocol.Response{
		Ack: 1,
		Frame: &protocol.Frame{
			Name: "api", Focused: 1, Width: 20, Height: 3,
			Panes: []protocol.PaneFrame{{
				ID: 1, Width: 20, Height: 3,
				Terminal: &protocol.TerminalFrame{Delta: true, Unchanged: true, Name: "api"},
			}},
		},
	}}); !dirty {
		t.Fatal("ack frame revealing prior skipped delta must mark dirty")
	}
	if got := u.frame.Rows[1][0].Text + u.frame.Rows[1][1].Text + u.frame.Rows[1][2].Text; got != "NEW" {
		t.Fatalf("row 1 after stale-ack delta = %q, want \"NEW\"", got)
	}
}

func simRow(s tcell.SimulationScreen, y, n int) string {
	out := make([]rune, 0, n)
	for x := range n {
		r, _, _, _ := s.GetContent(x, y)
		out = append(out, r)
	}
	return string(out)
}
