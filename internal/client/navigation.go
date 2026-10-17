package client

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

const resizeRepeatTimeout = time.Second

func paneDirection(key rune) string {
	switch key {
	case 'h':
		return "left"
	case 'j':
		return "down"
	case 'k':
		return "up"
	case 'l':
		return "right"
	}
	return ""
}

func navigationDirection(e *tcell.EventKey) string {
	if e.Modifiers() & ^tcell.ModCtrl != 0 {
		return ""
	}
	switch e.Key() {
	case tcell.KeyCtrlH:
		return "left"
	case tcell.KeyCtrlJ:
		return "down"
	case tcell.KeyCtrlK:
		return "up"
	case tcell.KeyCtrlL:
		return "right"
	case tcell.KeyRune:
		if e.Modifiers() == tcell.ModCtrl {
			return paneDirection(e.Rune())
		}
	}
	return ""
}

func (u *ui) resizePane(direction string, now time.Time) {
	u.resizeUntil = now.Add(resizeRepeatTimeout)
	u.messageUntil = time.Time{}
	u.send(protocol.Request{Op: "resize-pane", Direction: direction})
}
