package client

import "github.com/josephferrero/bmux/internal/protocol"

// A PTY reports cursor visibility and style, not the outer terminal's blink
// phase. Only source updates change the preview cursor.
func sourceCursorVisible(f *protocol.TerminalFrame) bool {
	return f != nil && f.CursorVisible && !f.Exited
}
