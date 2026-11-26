package protocol

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestProtocolVersionIs11(t *testing.T) {
	if Version != 11 {
		t.Fatalf("Version = %d, want 11", Version)
	}
}

func TestPaletteColorZeroAlloc(t *testing.T) {
	for i := range 256 {
		if got, want := PaletteColor(i), strconv.Itoa(i); got != want {
			t.Fatalf("PaletteColor(%d) = %q, want %q", i, got, want)
		}
	}
	if got := PaletteColor(-1); got != "-1" {
		t.Fatalf("PaletteColor(-1) = %q, want \"-1\"", got)
	}
	if got := PaletteColor(256); got != "256" {
		t.Fatalf("PaletteColor(256) = %q, want \"256\"", got)
	}
	var sink string
	allocs := testing.AllocsPerRun(1000, func() {
		for i := range 256 {
			sink = PaletteColor(i)
		}
	})
	_ = sink
	if allocs != 0 {
		t.Fatalf("PaletteColor(0..255) allocated %.2f times/op, want 0", allocs)
	}
}

func testRow(s string) []Cell {
	row := make([]Cell, len(s))
	for i := range s {
		row[i] = Cell{Text: s[i : i+1], Width: 1}
	}
	return row
}

func roundTripFrame(t *testing.T, f *TerminalFrame) (*TerminalFrame, string) {
	t.Helper()
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out TerminalFrame
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &out, string(raw)
}

func TestApplyTerminalDeltaRoundTrip(t *testing.T) {
	base := &TerminalFrame{
		Epoch:         1,
		Name:          "dev",
		Title:         "shell",
		Base:          10,
		History:       10,
		CursorX:       1,
		CursorY:       0,
		CursorVisible: true,
		Rows: [][]Cell{
			testRow("row0"),
			testRow("row1"),
			testRow("row2"),
			testRow("row3"),
		},
		Wrapped: []bool{false, true, false, false},
		Links:   []string{"https://example.com"},
	}

	// Unchanged delta omits rows and row_deltas on the wire and returns prev.
	unchangedWire, raw := roundTripFrame(t, &TerminalFrame{
		Delta:     true,
		Unchanged: true,
		Name:      "dev",
		Base:      10,
		History:   10,
	})
	if strings.Contains(raw, `"rows"`) || strings.Contains(raw, `"row_deltas"`) {
		t.Fatalf("unchanged delta should omit rows/row_deltas, got %s", raw)
	}
	gotUnchanged := ApplyTerminalDelta(base, unchangedWire)
	if gotUnchanged != base {
		t.Fatalf("unchanged delta did not return prev frame pointer")
	}

	// Single-row delta updates row 2 without mutating base, and clears wrapped on row 1.
	editWire, raw := roundTripFrame(t, &TerminalFrame{
		Delta:         true,
		Epoch:         1,
		Name:          "dev",
		Title:         "vim",
		Base:          10,
		History:       10,
		CursorX:       3,
		CursorY:       2,
		CursorVisible: true,
		RowDeltas: []RowDelta{
			{Y: 1, Cells: testRow("ROW1"), Wrapped: false},
			{Y: 2, Cells: testRow("EDIT"), Wrapped: false},
		},
		Links: []string{"https://example.com", "https://bmux.dev"},
	})
	if strings.Contains(raw, `"rows"`) {
		t.Fatalf("row delta should omit rows, got %s", raw)
	}
	edited := ApplyTerminalDelta(base, editWire)
	if edited.Delta || edited.Unchanged || edited.ScrollUp != 0 || edited.RowDeltas != nil {
		t.Fatalf("reconstructed frame kept delta fields: %+v", edited)
	}
	if edited.Title != "vim" || edited.CursorX != 3 || edited.CursorY != 2 {
		t.Fatalf("metadata not updated: %+v", edited)
	}
	if edited.Wrapped != nil {
		t.Fatalf("expected Wrapped to become nil when all wrapped rows cleared, got %v", edited.Wrapped)
	}
	// Verify base was not mutated.
	if base.Rows[1][0].Text != "r" || base.Rows[2][0].Text != "r" || !base.Wrapped[1] {
		t.Fatalf("ApplyTerminalDelta mutated previous frame!")
	}
	if !reflect.DeepEqual(edited.Rows[0], testRow("row0")) ||
		!reflect.DeepEqual(edited.Rows[1], testRow("ROW1")) ||
		!reflect.DeepEqual(edited.Rows[2], testRow("EDIT")) ||
		!reflect.DeepEqual(edited.Rows[3], testRow("row3")) {
		t.Fatalf("unexpected edited rows: %+v", edited.Rows)
	}

	// ScrollUp shifts rows and Wrapped upward before applying bottom RowDeltas.
	scrollStart := &TerminalFrame{
		Epoch:   1,
		Name:    "dev",
		Base:    10,
		History: 10,
		Rows: [][]Cell{
			testRow("line0"),
			testRow("line1"),
			testRow("line2"),
			testRow("line3"),
		},
		Wrapped: []bool{false, false, true, false},
	}
	scrollWire, _ := roundTripFrame(t, &TerminalFrame{
		Delta:    true,
		ScrollUp: 2,
		Epoch:    1,
		Name:     "dev",
		Base:     12,
		History:  12,
		RowDeltas: []RowDelta{
			{Y: 2, Cells: testRow("line4"), Wrapped: false},
			{Y: 3, Cells: testRow("line5"), Wrapped: true},
		},
	})
	scrolled := ApplyTerminalDelta(scrollStart, scrollWire)
	wantRows := [][]Cell{
		testRow("line2"),
		testRow("line3"),
		testRow("line4"),
		testRow("line5"),
	}
	if !reflect.DeepEqual(scrolled.Rows, wantRows) {
		t.Fatalf("scrolled rows mismatch:\ngot:  %+v\nwant: %+v", scrolled.Rows, wantRows)
	}
	wantWrapped := []bool{true, false, false, true}
	if !reflect.DeepEqual(scrolled.Wrapped, wantWrapped) {
		t.Fatalf("scrolled wrapped = %v, want %v", scrolled.Wrapped, wantWrapped)
	}
	// Original scrollStart must remain untouched.
	if scrollStart.Rows[0][4].Text != "0" || scrollStart.Wrapped[0] || !scrollStart.Wrapped[2] {
		t.Fatalf("ScrollUp mutated previous frame")
	}

	// Metadata-only delta (ScrollUp == 0, RowDeltas == nil) preserves all rows.
	metaWire, _ := roundTripFrame(t, &TerminalFrame{
		Delta:         true,
		Epoch:         1,
		Name:          "dev",
		Title:         "cursor-moved",
		Base:          12,
		History:       12,
		CursorX:       4,
		CursorY:       3,
		CursorVisible: true,
	})
	metaUpdated := ApplyTerminalDelta(scrolled, metaWire)
	if !reflect.DeepEqual(metaUpdated.Rows, wantRows) || metaUpdated.CursorX != 4 || metaUpdated.Title != "cursor-moved" {
		t.Fatalf("metadata-only delta failed: %+v", metaUpdated)
	}
}

func TestApplyTerminalDeltaCapsHugeRowDeltaY(t *testing.T) {
	base := &TerminalFrame{
		Epoch: 1,
		Name:  "dev",
		Rows:  [][]Cell{testRow("row0"), testRow("row1")},
	}
	got := ApplyTerminalDelta(base, &TerminalFrame{
		Delta: true,
		Epoch: 1,
		Name:  "dev",
		RowDeltas: []RowDelta{
			{Y: 1 << 30, Cells: testRow("oom")},
			{Y: -1, Cells: testRow("neg")},
			{Y: 1, Cells: testRow("ok")},
		},
	})
	if len(got.Rows) > MaxTerminalHeight {
		t.Fatalf("rows len = %d, exceeds MaxTerminalHeight (%d)", len(got.Rows), MaxTerminalHeight)
	}
	if !reflect.DeepEqual(got.Rows[1], testRow("ok")) {
		t.Fatalf("valid row delta not applied: %+v", got.Rows)
	}
}
