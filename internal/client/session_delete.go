package client

import (
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

type deleteResult struct {
	target protocol.Session
	err    error
}

// beginDelete asks to delete s, from d in the picker or on a sidebar session
// row. The target is captured now, so a refresh can't retarget the question.
func (u *ui) beginDelete(s protocol.Session) {
	if u.deleteBusy {
		u.notice("Already terminating session " + u.deletePending.Name)
		return
	}
	u.deleteTarget = s
	u.promptError = ""
	u.newName, u.prompt = s.Name, "delete"
}

// answerDelete terminates the captured session on a plain y; any other key
// keeps it.
func (u *ui) answerDelete(e *tcell.EventKey) {
	if e.Key() == tcell.KeyRune && e.Rune() == 'y' && e.Modifiers() == tcell.ModNone {
		u.deleteSelectedSession()
	} else {
		u.prompt, u.promptError = "", ""
	}
}

// Capture immutable identity before leaving the modal. Process cleanup is a
// bounded RPC, but it must never block the terminal event loop or redraws.
func (u *ui) deleteSelectedSession() {
	if u.deleteBusy {
		return
	}
	target := u.deleteTarget
	if u.deletions == nil {
		u.deletions = make(chan deleteResult, 1)
	}
	u.deletePending, u.deleteBusy = target, true
	u.prompt, u.promptError = "", ""
	u.stopPreview()
	u.notice("Terminating session " + target.Name + "…")
	socket, results, done := u.socket, u.deletions, u.done
	go func() {
		_, err := Request(socket, protocol.Request{Op: "kill", Name: target.Name, Generation: protocol.SessionGeneration(target.Created)})
		select {
		case results <- deleteResult{target: target, err: err}:
		case <-done:
		}
	}()
}

func (u *ui) finishDelete(result deleteResult) {
	u.deleteBusy = false
	u.deletePending = protocol.Session{}
	if result.err != nil {
		u.notice("Could not delete " + result.target.Name + ": " + result.err.Error())
		// Do not displace a new/rename prompt, or pull the user out of an app.
		if u.picker && u.prompt == "" {
			u.deleteTarget = result.target
			u.prompt, u.promptError = "delete", result.err.Error()
			u.stopPreview()
		}
		return
	}
	// Attached-session exit is handled by its own stream, not a name-based
	// action here: a newly attached replacement might reuse the deleted name.
	if u.picker && u.prompt == "" {
		u.refresh()
	} else if !u.picker {
		u.refreshSidebar()
	}
	u.notice("Deleted session " + result.target.Name)
}
