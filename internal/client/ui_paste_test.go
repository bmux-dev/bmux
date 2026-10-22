package client

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestRawPasteResumesLiveBeforeSendingExactPayload(t *testing.T) {
	u, requests := navigationUI(t)
	u.frame = &protocol.TerminalFrame{Offset: 3}
	u.selection = selection{active: true}
	u.resizeUntil = time.Now().Add(time.Second)
	u.handleRawPaste(&rawPasteEvent{Text: fidelityPaste})
	nextNavigationRequest(t, requests, "scroll", "")
	r := nextNavigationRequest(t, requests, "paste", "")
	if r.Text != fidelityPaste || r.PaneID != 1 || u.selection.active || !u.resizeUntil.IsZero() || u.prefix || u.quitting {
		t.Fatalf("raw paste did not resume live with exact payload: %+v", r)
	}
	// Normal input remains ordered after the single atomic paste request.
	u.key(tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone))
	nextNavigationRequest(t, requests, "scroll", "")
	if r := nextNavigationRequest(t, requests, "key", ""); r.Text != "z" {
		t.Fatalf("ordinary input changed after paste: %+v", r)
	}
}

func TestRawPasteAppendsToPickerPromptsAndFilter(t *testing.T) {
	for _, prompt := range []string{"rename", "filter"} {
		t.Run(prompt, func(t *testing.T) {
			u, requests := navigationUI(t)
			u.picker = true
			u.input, u.query = "before", "before"
			u.promptError = "old error"
			u.chosen = 2
			u.resizeUntil = time.Now().Add(time.Second)
			if prompt == "filter" {
				u.searching = true
			} else {
				u.prompt = prompt
			}
			u.handleRawPaste(&rawPasteEvent{Text: fidelityPaste})
			if prompt == "filter" {
				if u.query != "before"+fidelityPaste || u.chosen != 0 || u.input != "before" {
					t.Fatalf("filter paste changed: query=%q chosen=%d input=%q", u.query, u.chosen, u.input)
				}
			} else if u.input != "before"+fidelityPaste || u.prompt != prompt || u.promptError != "" || u.query != "before" {
				t.Fatalf("prompt paste changed: prompt=%q input=%q error=%q", u.prompt, u.input, u.promptError)
			}
			if !u.resizeUntil.IsZero() || u.prefix || u.quitting {
				t.Fatal("paste triggered bindings or retained resize repetition")
			}
			select {
			case r := <-requests:
				t.Fatalf("picker paste sent session request: %+v", r)
			default:
			}
		})
	}
}

func TestRawPasteRejectsListAndDeleteConfirmation(t *testing.T) {
	for _, prompt := range []string{"", "delete"} {
		t.Run("prompt="+prompt, func(t *testing.T) {
			u := &ui{picker: true, prompt: prompt, input: "unchanged", query: "unchanged", deleteTarget: protocol.Session{Name: "keep"}}
			u.handleRawPaste(&rawPasteEvent{Text: "y\n\x01d"})
			if u.prompt != prompt || u.input != "unchanged" || u.query != "unchanged" || u.message != "Paste into a name or / filter" || !time.Now().Before(u.messageUntil) {
				t.Fatalf("paste accepted outside an editable prompt: %+v", u)
			}
			if prompt == "delete" {
				// Paste must not act as the bare, unmodified y required to terminate.
				u.pickerKey(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModCtrl))
				if u.prompt != "" || u.message != "Paste into a name or / filter" {
					t.Fatal("modified y confirmed termination")
				}
			}
		})
	}
}

func TestRawPasteRejectionIsVisibleWithoutSendingOrEditing(t *testing.T) {
	for _, picker := range []bool{false, true} {
		u, requests := navigationUI(t)
		screen := tcell.NewSimulationScreen("UTF-8")
		if err := screen.Init(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(screen.Fini)
		screen.SetSize(100, 24)
		u.screen, u.picker, u.prompt, u.input = screen, picker, "name", "unchanged"
		u.frame = &protocol.TerminalFrame{Offset: 3}
		u.selection = selection{active: true}
		u.resizeUntil = time.Now().Add(time.Second)
		message := "Paste rejected: invalid UTF-8"
		u.handleRawPaste(&rawPasteEvent{Text: "must not leak", Err: errors.New(message)})
		if u.input != "unchanged" || u.query != "" || !u.selection.active || !u.resizeUntil.IsZero() || u.message != message {
			t.Fatal("rejected paste changed input or live state")
		}
		u.draw()
		var box strings.Builder
		for y := paneOriginY; y < paneOriginY+3; y++ {
			box.WriteString(pickerScreenLine(u, y) + "\n")
		}
		if !strings.Contains(box.String(), message) {
			t.Fatalf("paste rejection is not shown in the notice box: %q", box.String())
		}
		if footer := pickerScreenLine(u, 23); strings.Contains(footer, message) {
			t.Fatalf("notice still in the footer: %q", footer)
		}
		select {
		case r := <-requests:
			t.Fatalf("rejected paste sent request: %+v", r)
		default:
		}
	}
}
