package client

import (
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func (u *ui) handleRawPaste(e *rawPasteEvent) {
	u.resizeUntil = time.Time{}
	if e.Err != nil {
		u.notice(e.Err.Error())
	} else if u.picker {
		if u.prompt == "name" || u.prompt == "rename" || u.prompt == "directory" {
			u.input += e.Text
			u.promptError = ""
		} else if u.searching {
			u.query += e.Text
			u.chosen = 0
			u.requestPreview()
		} else {
			u.notice("Paste into a name, directory, or / filter")
		}
	} else if u.prompt == "delete" {
		// Pasted text never confirms deleting a session from the sidebar.
		u.prompt, u.promptError = "", ""
	} else if u.closeConfirm != 0 {
		// Pasted text never confirms closing a pane.
		u.closeConfirm, u.closeCommand = 0, ""
	} else {
		u.resumeLive()
		u.send(protocol.Request{Op: "paste", Text: e.Text})
	}
}
