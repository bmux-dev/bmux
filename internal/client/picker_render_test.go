package client

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func pickerScreenLine(u *ui, y int) string {
	w, _ := u.screen.Size()
	var line strings.Builder
	for x := 0; x < w; x++ {
		text, _, _ := u.screen.Get(x, y)
		if text == "" {
			text = " "
		}
		line.WriteString(text)
	}
	return line.String()
}

func pickerScreenText(u *ui) string {
	_, h := u.screen.Size()
	var lines []string
	for y := 0; y < h; y++ {
		lines = append(lines, pickerScreenLine(u, y))
	}
	return strings.Join(lines, "\n")
}

func TestPickerDenseRowsPositionAndOverflow(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {40, 12}} {
		for _, count := range []int{40, 200} {
			t.Run(fmt.Sprintf("%dx%d/%d", size[0], size[1], count), func(t *testing.T) {
				u := pickerUI(t)
				u.screen.(tcell.SimulationScreen).SetSize(size[0], size[1])
				u.sessions = nil
				for i := 0; i < count; i++ {
					u.sessions = append(u.sessions, protocol.Session{Name: fmt.Sprintf("s%03d", i), Panes: 2, Clients: 3, Dir: "/projects/界/é/long/start/directory"})
				}
				u.chosen = count - 1
				u.drawPicker(size[0], size[1])
				list, _ := pickerAreas(size[0], size[1])
				for y := list.y; y < list.y+list.h; y++ {
					line := pickerScreenLine(u, y)
					if !strings.Contains(line, "2 panes (attached: 3") {
						t.Fatalf("row %d not dense/counts hidden: %q", y, line)
					}
				}
				marker, style, _ := u.screen.Get(list.x, list.y+list.h-1)
				_, _, attrs := style.Decompose()
				if marker != "(" || attrs&tcell.AttrBold == 0 || attrs&tcell.AttrReverse == 0 {
					t.Fatalf("selection lacks monochrome marker/bold/reverse: %q %v", marker, attrs)
				}
				text := pickerScreenText(u)
				if !strings.Contains(text, fmt.Sprintf("%d/%d", count, count)) || !strings.Contains(text, fmt.Sprintf("%d-%d", count-list.h+1, count)) {
					t.Fatalf("missing position/visible range:\n%s", text)
				}
			})
		}
	}
}

func TestPickerCountsSeparateThisClientFromOthers(t *testing.T) {
	u := pickerUI(t)
	u.name, u.conn = "api", &connection{}
	u.sessions[0].Clients = 3
	u.sessions[1].Clients = 4
	u.chosen = 1
	u.drawPicker(100, 24)
	text := pickerScreenText(u)
	for _, want := range []string{"api", "2 panes (attached: here + 2 others)", "1 panes (attached: 4 clients)", "(1) + web"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}

func TestPickerDeletePromptUsesCapturedMetadata(t *testing.T) {
	u := pickerUI(t)
	u.sessions[0].Clients = 5
	u.listKey(runeKey('d'))
	if u.deleteTarget.Name != "api" || u.deleteTarget.Panes != 2 || u.deleteTarget.Clients != 5 {
		t.Fatalf("target not captured: %+v", u.deleteTarget)
	}
	u.sessions[0] = protocol.Session{Name: "replacement", Panes: 99, Clients: 99}
	u.chosen = 1
	u.drawPicker(100, 24)
	text := pickerScreenText(u)
	for _, want := range []string{"Delete \"api\"?", "2 panes · 5 attached clients", "Yes (y)", "No (n / Esc)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing captured confirmation %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Delete \"replacement\"?") || strings.Contains(text, "Enter: delete") {
		t.Fatal("confirmation retargeted or advertised unsafe Enter")
	}
}

func TestPickerPreviewHasCleanTitleAndExceptionalMessages(t *testing.T) {
	for _, state := range []struct {
		name, message string
		frame         *protocol.Frame
		err           string
	}{
		{name: "loading", message: "Loading…"},
		{name: "unavailable", message: "Unavailable · server unavailable", err: "server unavailable"},
		{name: "closed", message: "Closed · session has no running panes", frame: &protocol.Frame{}},
		{name: "fresh", frame: splitFrame()},
	} {
		t.Run(state.name, func(t *testing.T) {
			u := pickerUI(t)
			u.previewName, u.preview, u.previewError = "api", state.frame, state.err
			u.drawPicker(100, 24)
			text := pickerScreenText(u)
			for _, want := range []string{"api · 1/3", state.message} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q:\n%s", want, text)
				}
			}
			for _, unwanted := range []string{"sort:", "view:", "read-only", "refreshing"} {
				if strings.Contains(text, unwanted) {
					t.Fatalf("picker still shows %q", unwanted)
				}
			}
		})
	}
}

func TestPickerTinySizesAndPromptErrors(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 3}, {10, 6}, {40, 12}} {
		u := pickerUI(t)
		u.screen.(tcell.SimulationScreen).SetSize(size[0], size[1])
		u.drawPicker(size[0], size[1])
		u.prompt, u.input, u.promptError = "name", "界é", "Name already exists"
		u.drawPicker(size[0], size[1])
		if size[1] >= 12 && !strings.Contains(pickerScreenText(u), "Error: Name already exists") {
			t.Fatal("validation error not rendered")
		}
	}
	for _, line := range pickerWrap("界é👩‍💻long", 3) {
		if displayWidth(line) > 3 || strings.HasPrefix(line, "́") {
			t.Fatalf("wrap split or exceeded grapheme cells: %q", line)
		}
	}
}

func TestPickerNativeTerminalHasMinimalBorderTitle(t *testing.T) {
	u := pickerUI(t)
	u.preview, u.previewName = splitFrame(), "api"
	u.drawPicker(100, 24)
	text := pickerScreenText(u)
	if !strings.Contains(text, "api · 1/3") {
		t.Fatal("session name or position missing")
	}
}

func TestPickerFooterKeepsActionsInCompactWidths(t *testing.T) {
	u := pickerUI(t)
	for _, width := range []int{40, 60, 80, 120} {
		footer := u.pickerFooter(width)
		if displayWidth(footer) > width {
			t.Fatalf("footer at %d hides actions: %q", width, footer)
		}
		for _, key := range []string{"j/k", "attach", "n", "d", "r", "/", "Esc"} {
			if !strings.Contains(footer, key) {
				t.Fatalf("missing %q at %d: %q", key, width, footer)
			}
		}
	}
	for _, prompt := range []string{"delete", "name"} {
		u.prompt = prompt
		if displayWidth(u.pickerFooter(40)) > 40 {
			t.Fatalf("%s footer hides keys at 40 columns", prompt)
		}
	}
	u.prompt, u.searching = "", true
	if displayWidth(u.pickerFooter(40)) > 40 {
		t.Fatal("search footer hides keys at 40 columns")
	}
}
