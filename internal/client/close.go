package client

import (
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

// beginClosePane closes the focused pane (Ctrl-a x). A pane where only its
// shell is running closes at once; if a program is in front of the shell, or
// bmux can't tell, the status line asks first and only a lowercase y closes.
func (u *ui) beginClosePane() {
	pane := u.focused
	if pane == 0 || u.conn == nil {
		return
	}
	r, err := Request(u.socket, protocol.Request{Op: "pane-command", Name: u.name, PaneID: pane})
	if err == nil && r.Command == "" {
		u.send(protocol.Request{Op: "close-pane", PaneID: pane})
		return
	}
	u.closeConfirm, u.closeCommand = pane, r.Command
	if err != nil || u.closeCommand == "" {
		u.closeCommand = "a program"
	}
}

// answerClose answers the pending close-pane or close-window question.
func (u *ui) answerClose(e *tcell.EventKey) {
	pane, window := u.closeConfirm, u.closeWindow
	u.closeConfirm, u.closeWindow, u.closeCommand = 0, 0, ""
	if e.Key() != tcell.KeyRune || e.Rune() != 'y' || e.Modifiers() != tcell.ModNone {
		return
	}
	if window != 0 {
		u.send(protocol.Request{Op: "close-window", Window: window})
	} else {
		u.send(protocol.Request{Op: "close-pane", PaneID: pane})
	}
}
