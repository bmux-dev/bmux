package protocol

import (
	"encoding/json"
	"fmt"
)

// CellPaletteRunsV1 is a stateless full-snapshot encoding, not row deltas.
const CellPaletteRunsV1 = "cell-palette-runs-v1"

// MaxCompactFrameCells bounds expansion across all compact panes in a frame.
// Larger frames can mix ordinary panes with compact panes within this budget.
const MaxCompactFrameCells = MaxTerminalWidth * MaxTerminalHeight

type CompactRows struct {
	Width int          `json:"width"`
	Cells []Cell       `json:"cells"`
	Rows  []CompactRow `json:"rows"`
}

type CompactRow struct {
	Indices []int `json:"i,omitempty"`
	// Runs alternates dictionary index and positive cell repetition count.
	Runs []int `json:"r,omitempty"`
}

// CompactFrame copies only transport headers; it never changes the source
// snapshot or metadata. Each terminal dictionary is independent of all earlier
// frames and other connections. High-entropy and out-of-bounds panes stay legacy.
func CompactFrame(frame *Frame) *Frame {
	if frame == nil {
		return nil
	}
	copyFrame := *frame
	if frame.Panes != nil {
		copyFrame.Panes = append(make([]PaneFrame, 0, len(frame.Panes)), frame.Panes...)
	}
	budget := MaxCompactFrameCells
	for i, pane := range frame.Panes {
		if pane.Terminal == nil {
			continue
		}
		packed, count := compactTerminal(pane.Terminal, budget)
		copyFrame.Panes[i].Terminal = packed
		budget -= count
	}
	return &copyFrame
}

func compactTerminal(terminal *TerminalFrame, budget int) (*TerminalFrame, int) {
	rows := terminal.Rows
	if terminal.CompactRows != nil || len(rows) == 0 || len(rows) > MaxTerminalHeight {
		return terminal, 0
	}
	width := len(rows[0])
	if width == 0 || width > MaxTerminalWidth || len(rows)*width > budget {
		return terminal, 0
	}
	count := len(rows) * width
	palette := make([]Cell, 0)
	lookup := make(map[Cell]int)
	for _, row := range rows {
		if len(row) != width {
			return terminal, 0
		}
		for _, cell := range row {
			if _, ok := lookup[cell]; !ok {
				lookup[cell] = len(palette)
				palette = append(palette, cell)
				// A cheap entropy guard avoids duplicating almost every source cell in
				// a dictionary. It is not a guarantee of minimum serialized size.
				if len(palette) > count/2 {
					return terminal, 0
				}
			}
		}
	}
	packed := &CompactRows{Width: width, Cells: palette, Rows: make([]CompactRow, len(rows))}
	for y, row := range rows {
		indices := make([]int, len(row))
		runs := make([]int, 0)
		for x, cell := range row {
			index := lookup[cell]
			indices[x] = index
			if len(runs) != 0 && runs[len(runs)-2] == index {
				runs[len(runs)-1]++
			} else {
				runs = append(runs, index, 1)
			}
		}
		if integerArraySize(runs) < integerArraySize(indices) {
			packed.Rows[y].Runs = runs
		} else {
			packed.Rows[y].Indices = indices
		}
	}
	copyTerminal := *terminal
	copyTerminal.Rows, copyTerminal.CompactRows = nil, packed
	return &copyTerminal, count
}

func integerArraySize(values []int) int {
	if len(values) == 0 {
		return 2
	}
	size := 1 // [] plus n-1 commas
	for _, n := range values {
		size++
		for n >= 10 {
			size++
			n /= 10
		}
	}
	return size + len(values)
}

// DecodeResponse expands negotiated compact panes before the caller observes
// the frame. JSON parsing has the same framing as legacy responses; expansion
// allocations are bounded independently of hostile indices or run counts.
func DecodeResponse(decoder *json.Decoder, encoding string, response *Response) error {
	var decoded Response
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := ExpandFrame(decoded.Frame, encoding); err != nil {
		return err
	}
	*response = decoded
	return nil
}

// ExpandFrame validates and restores compact rows without exposing a partial
// frame on failure. Ordinary panes remain valid on a compact connection.
func ExpandFrame(frame *Frame, encoding string) error {
	if encoding != "" && encoding != CellPaletteRunsV1 {
		return fmt.Errorf("unknown frame encoding %q", encoding)
	}
	if frame == nil {
		return nil
	}
	budget := MaxCompactFrameCells
	var expanded [][][]Cell
	for i, pane := range frame.Panes {
		if pane.Terminal == nil || pane.Terminal.CompactRows == nil {
			continue
		}
		if encoding != CellPaletteRunsV1 {
			return fmt.Errorf("compact rows without negotiated encoding")
		}
		if pane.Terminal.Rows != nil {
			return fmt.Errorf("mixed ordinary and compact rows")
		}
		packed := pane.Terminal.CompactRows
		width, height := packed.Width, len(packed.Rows)
		if width < 1 || width > MaxTerminalWidth || height < 1 || height > MaxTerminalHeight || width*height > budget {
			return fmt.Errorf("compact rows exceed geometry or frame expansion limit")
		}
		count := width * height
		if len(packed.Cells) < 1 || len(packed.Cells) > count {
			return fmt.Errorf("invalid compact cell dictionary size")
		}
		// Validate all row shapes and indices before allocating expanded cells.
		for _, row := range packed.Rows {
			if (row.Indices == nil) == (row.Runs == nil) {
				return fmt.Errorf("compact row requires exactly one representation")
			}
			if row.Indices != nil {
				if len(row.Indices) != width {
					return fmt.Errorf("invalid compact index row width")
				}
				for _, index := range row.Indices {
					if index < 0 || index >= len(packed.Cells) {
						return fmt.Errorf("invalid compact cell index")
					}
				}
			} else {
				if len(row.Runs) == 0 || len(row.Runs)%2 != 0 || len(row.Runs) > 2*width {
					return fmt.Errorf("invalid compact run row")
				}
				remaining := width
				for j := 0; j < len(row.Runs); j += 2 {
					index, repeat := row.Runs[j], row.Runs[j+1]
					if index < 0 || index >= len(packed.Cells) || repeat < 1 || repeat > remaining {
						return fmt.Errorf("invalid compact cell run")
					}
					remaining -= repeat
				}
				if remaining != 0 {
					return fmt.Errorf("invalid compact run row width")
				}
			}
		}
		if expanded == nil {
			expanded = make([][][]Cell, len(frame.Panes))
		}
		rows := make([][]Cell, height)
		for y, row := range packed.Rows {
			rows[y] = make([]Cell, width)
			if row.Indices != nil {
				for x, index := range row.Indices {
					rows[y][x] = packed.Cells[index]
				}
			} else {
				x := 0
				for j := 0; j < len(row.Runs); j += 2 {
					for n := 0; n < row.Runs[j+1]; n++ {
						rows[y][x] = packed.Cells[row.Runs[j]]
						x++
					}
				}
			}
		}
		expanded[i] = rows
		budget -= count
	}
	// Commit only after the entire frame validated; no partial frame is exposed.
	for i, rows := range expanded {
		if rows != nil {
			frame.Panes[i].Terminal.Rows = rows
			frame.Panes[i].Terminal.CompactRows = nil
		}
	}
	return nil
}
