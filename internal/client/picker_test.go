package client

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func pickerUI(t *testing.T) *ui {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(100, 24)
	return &ui{screen: screen, picker: true, cwd: "/projects", sessions: []protocol.Session{{Name: "api", Dir: "/api", Panes: 2}, {Name: "web", Dir: "/web", Panes: 1}, {Name: "logs", Dir: "/logs", Panes: 1}}}
}

func runeKey(r rune) *tcell.EventKey { return tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone) }

func TestPickerDefaultsToJKListWithOptionalSearchAndActions(t *testing.T) {
	u := pickerUI(t)
	u.pickerKey(runeKey('j'))
	if u.chosen != 1 || u.query != "" || u.searching {
		t.Fatal("j typed into a default search field")
	}
	u.pickerKey(runeKey('k'))
	if u.chosen != 0 {
		t.Fatal("k didn't move up")
	}
	u.pickerKey(runeKey('n'))
	if !u.projects.open || !u.picker || u.prompt != "" {
		t.Fatal("n didn't open the new-session popup")
	}
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.projects.open || !u.picker || u.quitting {
		t.Fatal("Esc didn't return to the picker")
	}
	u.pickerKey(runeKey('d'))
	if u.prompt != "delete" || u.newName != "api" {
		t.Fatal("d did not ask to delete selected session")
	}
	u.pickerKey(runeKey('n'))
	if u.prompt != "" {
		t.Fatal("n didn't cancel deletion")
	}
	u.pickerKey(runeKey('/'))
	u.pickerKey(runeKey('w'))
	if !u.searching || u.query != "w" || len(u.filtered()) != 1 {
		t.Fatal("opt-in search not filtering")
	}
	u.pickerKey(runeKey('j'))
	if u.query != "wj" {
		t.Fatal("j navigated instead of typing in explicit search")
	}
	u.pickerKey(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone))
	u.pickerKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if u.searching || u.query != "w" {
		t.Fatal("Enter didn't finish search without attaching")
	}
	u.pickerKey(runeKey('r'))
	if u.prompt != "rename" || u.newName != "web" {
		t.Fatal("r didn't rename filtered session")
	}
}

func TestPickerPreviewRenderingMouseSelectionAndStaleResults(t *testing.T) {
	u := pickerUI(t)
	u.requestPreview()
	u.acceptPreview(previewResult{epoch: u.previewEpoch, name: "api", frame: &protocol.Frame{Name: "api", PreviewMode: "overview", Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: 20, Height: 4, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("LIVE_PREVIEW")}}}}}})
	u.draw()
	_, area := pickerAreas(100, 24)
	visiblePixels := false
	for y := 0; y < area.h; y++ {
		for x := 0; x < area.w; x++ {
			text, _, _ := u.screen.Get(area.x+x, area.y+y)
			if text != " " && text != "" {
				visiblePixels = true
			}
		}
	}
	if !visiblePixels {
		t.Fatal("native terminal view not rendered")
	}
	_, _, visible := u.screen.(tcell.SimulationScreen).GetCursor()
	if visible {
		t.Fatal("default list has a search cursor")
	}
	oldEpoch := u.previewEpoch
	u.pickerKey(runeKey('j'))
	u.acceptPreview(previewResult{epoch: oldEpoch, name: "api", frame: splitFrame()})
	if u.preview != nil || u.previewName != "web" {
		t.Fatal("stale preview replaced highlighted session")
	}
	u.pickerMouse(tcell.NewEventMouse(area.x, area.y, tcell.Button1, tcell.ModNone), area.x, area.y)
	if u.chosen != 1 {
		t.Fatal("click in preview selected a list row")
	}
	list, _ := pickerAreas(100, 24)
	u.pickerMouse(tcell.NewEventMouse(list.x, list.y+2, tcell.Button1, tcell.ModNone), list.x, list.y+2)
	if u.chosen != 2 || u.previewName != "logs" {
		t.Fatal("list click didn't select for preview")
	}
	if !u.picker || u.conn != nil {
		t.Fatal("list click attached instead of only selecting for preview")
	}
	u.acceptPreview(previewResult{epoch: u.previewEpoch, name: "logs", err: errors.New("unknown operation")})
	if !u.previewUnsupported || u.previewError == "" {
		t.Fatal("old server didn't get a preview upgrade hint")
	}
}

func TestPickerLayoutAdaptsWithoutOverlap(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {40, 12}, {30, 12}, {1, 1}, {2, 3}, {10, 6}} {
		list, preview := pickerAreas(size[0], size[1])
		if preview.w > 0 && list.x < preview.x+preview.w && preview.x < list.x+list.w && list.y < preview.y+preview.h && preview.y < list.y+list.h {
			t.Fatalf("overlap at %v", size)
		}
		if preview.w > 0 && (preview.x+preview.w > size[0] || preview.y+preview.h >= size[1]) {
			t.Fatalf("preview out of bounds at %v", size)
		}
		if list.w > 0 && list.h > 0 && (list.x+list.w > size[0] || list.y+list.h >= size[1]) {
			t.Fatalf("list out of bounds at %v: %+v", size, list)
		}
		if size[0] >= 60 && size[1] >= 20 && (list.h < 6 || preview.w == 0 || preview.h == 0) {
			t.Fatalf("insufficient list density or missing preview at %v", size)
		}
		if size == [2]int{40, 12} && preview.w != 0 {
			t.Fatal("40x12 must be list-only")
		}
	}
}

func TestDeleteConfirmationKeepsItsOriginalTarget(t *testing.T) {
	// A short private socket path avoids Darwin's sockaddr_un path limit.
	dir, err := os.MkdirTemp("", "hv-pick-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "rpc")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	requests := make(chan protocol.Request, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			var r protocol.Request
			_ = json.NewDecoder(c).Decode(&r)
			requests <- r
			_ = json.NewEncoder(c).Encode(protocol.Response{Version: protocol.Version, Sessions: []protocol.Session{{Name: "web", Panes: 1}}})
			c.Close()
		}
	}()
	u := pickerUI(t)
	u.socket = socket
	u.pickerKey(runeKey('d'))
	select {
	case r := <-requests:
		t.Fatalf("delete sent before confirmation: %+v", r)
	default:
	}
	u.chosen = 1      // A refresh must not retarget the confirmation to another session.
	u.newName = "web" // The captured session is also independent of generic prompt state.
	u.pickerKey(runeKey('y'))
	if u.prompt != "" || !u.deleteBusy || u.message != "Terminating session api…" {
		t.Fatal("confirmation did not immediately dismiss modal with progress")
	}
	select {
	case r := <-requests:
		if r.Op != "kill" || r.Name != "api" {
			t.Fatalf("wrong deletion: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no confirmed deletion")
	}
	select {
	case result := <-u.deletions:
		u.finishDelete(result)
	case <-time.After(time.Second):
		t.Fatal("no deletion completion")
	}
	if u.prompt != "" || u.message != "Deleted session api" {
		t.Fatal("deletion did not return to the list")
	}
}
