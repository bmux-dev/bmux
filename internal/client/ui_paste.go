package client

import (
	"strings"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func (u *ui) handleRawPaste(e *rawPasteEvent) {
	u.resizeUntil = time.Time{}
	if e.Err != nil {
		u.notice(e.Err.Error())
	} else if u.projects.open && u.projects.creating {
		if u.projects.field == 1 {
			u.projects.dir += e.Text
		} else {
			u.projects.name += e.Text
		}
		u.projects.formErr, u.projects.candidates = "", nil
	} else if u.projects.open {
		u.projects.query += e.Text
		u.projects.chosen = 0
	} else if u.picker {
		if u.prompt == "rename" {
			u.input += e.Text
			u.promptError = ""
		} else if u.searching {
			u.query += e.Text
			u.chosen = 0
			u.requestPreview()
		} else {
			u.notice("Paste into a name or / filter")
		}
	} else if u.settingsPage.open {
		// Pasted text fills the value being edited and is never a command.
		if u.settingsPage.editing {
			u.settingsPage.insert(strings.Map(func(r rune) rune {
				if r < 0x20 || r == 0x7f {
					return -1
				}
				return r
			}, e.Text))
		}
	} else if u.view.active && u.view.searching {
		u.view.input += strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, e.Text)
	} else if u.view.active {
		u.notice("Paste goes to the pane: leave the scrollback view with Esc first")
	} else if u.prompt == "rename-window" {
		u.appendWindowName(e.Text)
	} else if u.prompt == "delete" {
		// Pasted text never confirms deleting a session from the sidebar.
		u.prompt, u.promptError = "", ""
	} else if u.closeConfirm != 0 || u.closeWindow != 0 {
		// Pasted text never confirms closing a pane or window.
		u.closeConfirm, u.closeWindow, u.closeCommand = 0, 0, ""
	} else {
		u.resumeLive()
		u.send(protocol.Request{Op: "paste", Text: e.Text})
	}
}
