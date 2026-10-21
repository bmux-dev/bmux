package client

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestDeleteModalOverlaysPickerAndKeepsStatusBars(t *testing.T) {
	u := pickerUI(t)
	u.preview, u.previewName = splitFrame(), "api"
	u.pickerKey(runeKey('d'))
	u.draw()
	if !strings.Contains(pickerScreenLine(u, 0), "") || !strings.Contains(pickerScreenLine(u, 23), "n/Esc no") {
		t.Fatal("modal hid status bars")
	}
	if !strings.Contains(pickerScreenLine(u, 1), "(0) + api") || !strings.Contains(pickerScreenLine(u, 2), "(1) + web") {
		t.Fatal("modal replaced picker instead of overlaying it")
	}
	_, style, _ := u.screen.Get(0, 2)
	_, _, attrs := style.Decompose()
	if attrs&tcell.AttrDim == 0 {
		t.Fatal("picker backdrop wasn't dimmed")
	}
	text, _, _ := u.screen.Get(32, 9)
	if text != "┌" {
		t.Fatalf("modal isn't centered: %q", text)
	}
	text, _, _ = u.screen.Get(67, 14)
	if text != "┘" {
		t.Fatal("modal bottom border missing")
	}
	if _, _, visible := u.screen.(tcell.SimulationScreen).GetCursor(); visible {
		t.Fatal("background preview/search cursor leaked through modal")
	}
	u.pickerMouse(tcell.NewEventMouse(0, 2, tcell.Button1, tcell.ModNone), 0, 2)
	if u.chosen != 0 {
		t.Fatal("modal allowed click-through to background list")
	}
	u.pickerKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	u.draw()
	if strings.Contains(pickerScreenText(u), "Delete \"api\"?") {
		t.Fatal("cancelled modal remained visible")
	}
	_, style, _ = u.screen.Get(0, 2)
	_, _, attrs = style.Decompose()
	if attrs&tcell.AttrDim != 0 {
		t.Fatal("modal left background dimmed after cancellation")
	}
}

func TestDeleteModalShowsUsefulCountsAndSimpleYesNo(t *testing.T) {
	for _, clients := range []int{0, 1, 5} {
		u := pickerUI(t)
		u.deleteTarget = protocol.Session{Name: "api", Panes: 3, Clients: clients, Created: time.Now()}
		u.drawDeleteModal(100, 24)
		text := pickerScreenText(u)
		for _, want := range []string{"Delete \"api\"?", "3 panes", "Yes (y)", "No (n / Esc)"} {
			if !strings.Contains(text, want) {
				t.Fatalf("simple modal missing %q", want)
			}
		}
		for _, unwanted := range []string{"0 attached", "owned jobs", "detached", "Only y"} {
			if strings.Contains(text, unwanted) {
				t.Fatalf("modal still contains unnecessary/stale detail %q", unwanted)
			}
		}
		if clients > 0 && !strings.Contains(text, "attached client") {
			t.Fatal("useful attachment count was omitted")
		}
	}
}

func TestDeleteCountsRefreshWithoutUsingReplacementMetadata(t *testing.T) {
	u := pickerUI(t)
	target := protocol.Session{Name: "api", Created: time.Now(), Panes: 2}
	u.deleteTarget = target
	u.sessions = []protocol.Session{target}
	u.sessions[0].Panes, u.sessions[0].Clients = 3, 2
	if got := u.deleteModalCounts(); got != "3 panes · 2 attached clients" {
		t.Fatalf("counts didn't refresh: %q", got)
	}
	u.sessions[0].Created = target.Created.Add(time.Second)
	if got := u.deleteModalCounts(); got != "2 panes" {
		t.Fatalf("replacement metadata leaked into captured modal: %q", got)
	}
	if !u.deleteTarget.Created.Equal(target.Created) {
		t.Fatal("count refresh changed action identity")
	}
	u.sessions = []protocol.Session{target}
	u.conn, u.name = &connection{}, "api"
	u.workspace = &protocol.Frame{Generation: protocol.SessionGeneration(target.Created)}
	if got := u.deleteModalCounts(); got != "2 panes · 1 attached client" {
		t.Fatalf("known local attachment shown as detached: %q", got)
	}
	u.sessions[0].Clients = 3
	if got := u.deleteModalCounts(); got != "2 panes · 3 attached clients" {
		t.Fatalf("local attachment counted twice: %q", got)
	}
	u.sessions[0].Clients = 0
	u.workspace.Generation = "new-instance"
	if got := u.deleteModalCounts(); got != "2 panes" {
		t.Fatalf("replacement attachment counted for old target: %q", got)
	}
}

func TestDeleteModalCancellationNeverSubmitsRequest(t *testing.T) {
	for _, key := range []*tcell.EventKey{tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), runeKey('Y'), runeKey('n'), runeKey('j')} {
		u := pickerUI(t)
		u.pickerKey(runeKey('d'))
		u.pickerKey(key)
		if u.prompt != "" || u.deleteBusy || u.deletions != nil {
			t.Fatal("non-confirmation key submitted a termination")
		}
	}
	for _, size := range [][2]int{{1, 1}, {2, 3}, {10, 6}, {40, 12}, {80, 24}} {
		u := pickerUI(t)
		u.screen.(tcell.SimulationScreen).SetSize(size[0], size[1])
		u.pickerKey(runeKey('d'))
		u.promptError = "Session changed; captured action cancelled"
		u.draw()
		if _, _, visible := u.screen.(tcell.SimulationScreen).GetCursor(); visible {
			t.Fatal("small modal borrowed source cursor")
		}
	}
}

func TestDeleteRPCDoesNotBlockPickerAndCannotSubmitTwice(t *testing.T) {
	dir, err := os.MkdirTemp("", "hv-del-")
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
	requests := make(chan protocol.Request, 1)
	release := make(chan struct{})
	releaseResponse := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var r protocol.Request
		if json.NewDecoder(c).Decode(&r) != nil {
			return
		}
		requests <- r
		<-release
		_ = json.NewEncoder(c).Encode(protocol.Response{Version: protocol.Version})
	}()
	defer func() { releaseResponse(); l.Close(); <-done }()
	u := pickerUI(t)
	u.socket = socket
	u.sessions[0].Created = time.Now()
	u.pickerKey(runeKey('d'))
	target := u.deleteTarget
	start := time.Now()
	u.pickerKey(runeKey('y'))
	if time.Since(start) > 250*time.Millisecond {
		t.Fatal("confirmation waited for process cleanup")
	}
	select {
	case r := <-requests:
		if r.Op != "kill" || r.Name != target.Name || r.Generation != protocol.SessionGeneration(target.Created) {
			t.Fatalf("lost captured identity: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no deletion request")
	}
	u.pickerKey(runeKey('j'))
	if u.chosen != 1 || !u.deleteBusy || u.prompt != "" {
		t.Fatal("picker was blocked while termination ran")
	}
	u.pickerKey(runeKey('d'))
	if u.prompt != "" || u.deletePending.Name != target.Name {
		t.Fatal("second termination displaced pending operation")
	}
	u.draw()
	select {
	case <-u.deletions:
		t.Fatal("cleanup finished before server acknowledged it")
	default:
	}
	// Completing a deletion must not replace a new prompt the user opened.
	u.newName, u.input, u.prompt = "web", "web", "rename"
	releaseResponse()
	select {
	case result := <-u.deletions:
		u.finishDelete(result)
	case <-time.After(time.Second):
		t.Fatal("no server acknowledgement")
	}
	if u.deleteBusy || u.prompt != "rename" || u.message != "Deleted session api" {
		t.Fatal("completion changed unrelated picker state")
	}
}

func TestDeleteFailureReturnsCapturedErrorWithoutDisplacingApp(t *testing.T) {
	target := protocol.Session{Name: "original", Created: time.Now(), Panes: 2, Clients: 3}
	u := pickerUI(t)
	u.deleteBusy = true
	u.finishDelete(deleteResult{target: target, err: errors.New("session changed; captured action cancelled")})
	if u.prompt != "delete" || u.deleteTarget.Name != "original" || !u.deleteTarget.Created.Equal(target.Created) || u.promptError == "" {
		t.Fatal("failure lost immutable confirmation/error")
	}
	u.draw()
	if !strings.Contains(pickerScreenText(u), "Error: session changed") {
		t.Fatal("modal did not show server rejection")
	}
	u.picker = false
	u.prompt = ""
	u.name = "new-app"
	u.finishDelete(deleteResult{target: target, err: errors.New("connection failed")})
	if u.picker || u.prompt != "" || u.name != "new-app" {
		t.Fatal("old deletion failure pulled user out of application")
	}
}
