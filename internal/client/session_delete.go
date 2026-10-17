package client

import "github.com/josephferrero/bmux/internal/protocol"

type deleteResult struct {
	target protocol.Session
	err    error
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
	}
	u.notice("Deleted session " + result.target.Name)
}
