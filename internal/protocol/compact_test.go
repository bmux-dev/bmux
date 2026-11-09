package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func compactFixture() Response {
	cells := []Cell{{}, {Text: " ", Width: 1}, {Text: "e\u0301", Width: 1, FG: "#123456", BG: "15", Attrs: 255, Underline: true, Link: 1}, {Text: "界", Width: 2}, {Width: 0}, {Text: "👩‍💻", Width: 2}, {Text: "\U0010eeee", Width: 1, FG: "#000001", Attrs: 3}}
	rows := make([][]Cell, 4)
	for y := range rows {
		rows[y] = make([]Cell, 70)
		for x := range rows[y] {
			if y == 0 {
				rows[y][x] = cells[1]
			} else {
				rows[y][x] = cells[x%len(cells)]
			}
		}
	}
	terminal := &TerminalFrame{Name: "pane", Title: "title", Epoch: 8, HistoryStart: 12, Rows: rows, Base: 32, Offset: 3, History: 23,
		CursorX: 5, CursorY: 2, CursorVisible: true, CursorStyle: 2, CursorBlink: true, Mouse: true, Alt: true, Exited: true,
		Wrapped: []bool{true, false, true, false}, Links: []string{"https://example.test/界"}, Images: []Image{{ID: 1, Width: 32, Height: 64, Cols: 2, Rows: 4}}}
	return Response{Version: Version, Revision: 99, Ack: 7, Sessions: []Session{{Name: "session", Clients: 2}},
		Frame: &Frame{Generation: "generation", PreviewMode: "mode", Width: 70, Height: 4, Name: "session", Focused: 3, Zoomed: true,
			Edge: 4, EdgeDirection: "left", Config: 8, Clipboard: "clipboard", Notice: "notice", Message: "message", MessageSeq: 42,
			Update: &UpdateHint{Version: "v99", Installed: true}, Windows: []Window{{Name: "window", Title: "window title", Active: true, Panes: []int{3}}},
			Panes: []PaneFrame{{ID: 3, X: 0, Y: 1, Width: 70, Height: 4, Terminal: terminal}}},
		Command: "command", Text: "text", Dir: "dir", Env: []string{"A=B"}, Zoomed: true, Pane: &PaneActivity{Pane: 3, Seq: 4},
		Range: &TextRange{Epoch: 8, FromX: 1, ToY: 2, Mode: "char"}, Panes: []PaneInfo{{Pane: 3, Session: "session"}}, ImageChunk: &ImageChunk{ID: 1, Total: 3, Data: []byte{1, 2, 3}}}
}

func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCompactResponseActualJSONRoundTrip(t *testing.T) {
	original := compactFixture()
	before := jsonBytes(t, original)
	wire := original
	wire.Frame = CompactFrame(original.Frame)
	terminal := wire.Frame.Panes[0].Terminal
	if terminal.Rows != nil || terminal.CompactRows == nil {
		t.Fatal("fixture not compacted")
	}
	if terminal.CompactRows.Rows[0].Runs == nil || terminal.CompactRows.Rows[1].Indices == nil {
		t.Fatal("both row encodings must be exercised")
	}
	var stream bytes.Buffer
	enc := json.NewEncoder(&stream)
	for _, r := range []Response{wire, {Version: Version, NotModified: true, Revision: 99}, {Version: Version, Ack: 8, Error: "ended"}} {
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(before, jsonBytes(t, original)) {
		t.Fatal("packing mutated source")
	}
	decoder := json.NewDecoder(&stream)
	var decoded Response
	if err := DecodeResponse(decoder, CellPaletteRunsV1, &decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, jsonBytes(t, decoded)) {
		t.Fatal("rows or response metadata changed")
	}
	if decoded.Frame.Panes[0].Terminal.CompactRows != nil {
		t.Fatal("wire representation leaked to UI")
	}
	for _, expected := range []Response{{Version: Version, NotModified: true, Revision: 99}, {Version: Version, Ack: 8, Error: "ended"}} {
		if err := DecodeResponse(decoder, CellPaletteRunsV1, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded, expected) {
			t.Fatalf("stream metadata: %#v", decoded)
		}
	}
}

func TestCompactRandomizedLosslessRows(t *testing.T) {
	rng := rand.New(rand.NewSource(172))
	palette := []Cell{{}, {Text: " ", Width: 1}, {Text: "A", Width: 1}, {Text: "\"<&\n", Width: 1, Link: 2}, {Text: "界", Width: 2}, {Width: 0, BG: "#abcdef"}}
	for n := 0; n < 100; n++ {
		width, height := 20+rng.Intn(100), 1+rng.Intn(20)
		rows := make([][]Cell, height)
		for y := range rows {
			rows[y] = make([]Cell, width)
			for x := range rows[y] {
				rows[y][x] = palette[rng.Intn(len(palette))]
			}
		}
		source := &Frame{Panes: []PaneFrame{{Terminal: &TerminalFrame{Rows: rows}}}}
		packed := CompactFrame(source)
		if packed.Panes[0].Terminal.CompactRows == nil {
			t.Fatal("random fixture not compacted")
		}
		var decoded Response
		data := jsonBytes(t, Response{Version: Version, Frame: packed})
		if err := DecodeResponse(json.NewDecoder(bytes.NewReader(data)), CellPaletteRunsV1, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(source, decoded.Frame) {
			t.Fatal("random cells changed")
		}
	}
}

func TestCompactFallbackAndFrameBudget(t *testing.T) {
	entropy := make([]Cell, 80)
	for i := range entropy {
		entropy[i] = Cell{Text: fmt.Sprint(i), Width: 1}
	}
	for _, rows := range [][][]Cell{nil, {}, {nil}, {{}, {}}, {entropy}, {{Cell{}}, {Cell{}, Cell{}}}} {
		source := &Frame{Panes: []PaneFrame{{Terminal: &TerminalFrame{Rows: rows}}}}
		packed := CompactFrame(source)
		if packed.Panes[0].Terminal != source.Panes[0].Terminal {
			t.Fatal("fallback pane should stay ordinary")
		}
		if !reflect.DeepEqual(source, packed) {
			t.Fatal("fallback changed shape")
		}
	}
	rows := make([][]Cell, 101)
	for y := range rows {
		rows[y] = make([]Cell, MaxTerminalWidth)
	}
	source := &Frame{Panes: []PaneFrame{{ID: 1, Terminal: &TerminalFrame{Rows: rows}}, {ID: 2, Terminal: &TerminalFrame{Rows: rows}}}}
	packed := CompactFrame(source)
	if packed.Panes[0].Terminal.CompactRows == nil || packed.Panes[1].Terminal.CompactRows != nil {
		t.Fatal("aggregate budget must preserve oversized panes as legacy")
	}
	if err := ExpandFrame(packed, CellPaletteRunsV1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source, packed) {
		t.Fatal("mixed-pane round trip changed cells")
	}
	for _, panes := range [][]PaneFrame{nil, {}} {
		source := &Frame{Panes: panes}
		if !reflect.DeepEqual(source, CompactFrame(source)) {
			t.Fatal("empty layout shape changed")
		}
	}
}

func TestCompactMalformedPayloads(t *testing.T) {
	valid := `{"width":2,"cells":[{}],"rows":[{"r":[0,2]}]}`
	cases := []string{
		`{"width":0,"cells":[{}],"rows":[{"r":[0,2]}]}`,
		`{"width":501,"cells":[{}],"rows":[{"r":[0,501]}]}`,
		`{"width":2,"cells":[],"rows":[{"r":[0,2]}]}`,
		`{"width":2,"cells":[{},{},{}],"rows":[{"r":[0,2]}]}`,
		`{"width":2,"cells":[{}],"rows":[]}`,
		`{"width":2,"cells":[{}],"rows":[{}]}`,
		`{"width":2,"cells":[{}],"rows":[{"i":[0,0],"r":[0,2]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"i":[]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"i":[0]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"i":[0,-1]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"i":[0,1]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[0]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[0,0]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[0,-1]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[0,9223372036854775807]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[0,1]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[-1,2]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[1,2]}]}`,
		`{"width":2,"cells":[{}],"rows":[{"r":[0,1,0,1,0,1]}]}`,
	}
	for i, packed := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			data := `{"version":10,"frame":{"panes":[{"terminal":{"rows":null,"compact_rows":` + packed + `}}]}}`
			sentinel := Response{Error: "unchanged"}
			decoded := sentinel
			if err := DecodeResponse(json.NewDecoder(strings.NewReader(data)), CellPaletteRunsV1, &decoded); err == nil {
				t.Fatal("malformed accepted")
			}
			if !reflect.DeepEqual(decoded, sentinel) {
				t.Fatal("failed decode exposed partial response")
			}
		})
	}
	for _, encoding := range []string{"", "unknown"} {
		var packed CompactRows
		if err := json.Unmarshal([]byte(valid), &packed); err != nil {
			t.Fatal(err)
		}
		f := &Frame{Panes: []PaneFrame{{Terminal: &TerminalFrame{CompactRows: &packed}}}}
		if err := ExpandFrame(f, encoding); err == nil {
			t.Fatal("unnegotiated encoding accepted")
		}
	}
	f := &Frame{Panes: []PaneFrame{{Terminal: &TerminalFrame{Rows: [][]Cell{}, CompactRows: &CompactRows{Width: 2, Cells: []Cell{{}}, Rows: []CompactRow{{Runs: []int{0, 2}}}}}}}}
	if err := ExpandFrame(f, CellPaletteRunsV1); err == nil {
		t.Fatal("mixed ordinary/compact terminal accepted")
	}
}

func TestCompactExpansionBudgetAndAtomicFailure(t *testing.T) {
	rows := make([]CompactRow, MaxTerminalHeight)
	for y := range rows {
		rows[y] = CompactRow{Runs: []int{0, MaxTerminalWidth}}
	}
	packed := &CompactRows{Width: MaxTerminalWidth, Cells: []Cell{{}}, Rows: rows}
	frame := &Frame{Panes: []PaneFrame{{Terminal: &TerminalFrame{CompactRows: packed}}, {Terminal: &TerminalFrame{CompactRows: packed}}}}
	if err := ExpandFrame(frame, CellPaletteRunsV1); err == nil {
		t.Fatal("aggregate expansion limit accepted")
	}
	for _, pane := range frame.Panes {
		if pane.Terminal.Rows != nil || pane.Terminal.CompactRows == nil {
			t.Fatal("partial expansion committed")
		}
	}
	overheight := *packed
	overheight.Rows = append(append([]CompactRow(nil), rows...), rows[0])
	if err := ExpandFrame(&Frame{Panes: []PaneFrame{{Terminal: &TerminalFrame{CompactRows: &overheight}}}}, CellPaletteRunsV1); err == nil {
		t.Fatal("over-height accepted")
	}
}

func TestCompactIntegerArraySizeMatchesJSON(t *testing.T) {
	for _, values := range [][]int{{}, {0}, {9, 10, 99, 100}, {0, 500, 12345}} {
		if got, want := integerArraySize(values), len(jsonBytes(t, values)); got != want {
			t.Fatalf("size(%v) = %d, want %d", values, got, want)
		}
	}
}
