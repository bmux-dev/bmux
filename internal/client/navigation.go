package client

import (
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

const resizeRepeatTimeout = time.Second

func (u *ui) resizePane(direction string, now time.Time) {
	u.resizeUntil = now.Add(resizeRepeatTimeout)
	u.messageUntil = time.Time{}
	u.send(protocol.Request{Op: "resize-pane", Direction: direction})
}
