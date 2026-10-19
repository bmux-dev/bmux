package server

import (
	"encoding/base64"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// clipboardWrites orders OSC 52 writes across every pane. An attachment
// delivers only writes numbered after the value it read when it attached.
var clipboardWrites atomic.Uint64

// The pinned x/vt keeps at most 4 MiB of one OSC sequence and silently drops
// the rest, so a sequence that fills it may have been cut short. Copying part
// of a yank would be worse than not copying it.
const oscDataLimit = 4 << 20

// trackClipboard keeps the latest clipboard write (OSC 52) an app sends, for
// attached clients to copy; the server never touches a clipboard itself.
// Every selection target (c, p, s, 0-7) counts as the clipboard. Reads ("?")
// are never answered, so apps can't see what you copied elsewhere. Writes that
// clear, aren't valid base64 or UTF-8, or may have been truncated are dropped.
func (s *pane) trackClipboard() {
	s.term.RegisterOscHandler(52, func(data []byte) bool {
		if len(data) >= oscDataLimit {
			return true
		}
		_, rest, _ := strings.Cut(string(data), ";")
		_, payload, ok := strings.Cut(rest, ";")
		if !ok || payload == "" || payload == "?" {
			return true
		}
		text, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			text, err = base64.RawStdEncoding.DecodeString(payload)
		}
		if err != nil || len(text) == 0 || !utf8.Valid(text) {
			return true
		}
		s.clipboard, s.clipboardSeq = string(text), clipboardWrites.Add(1)
		return true
	})
}

// clipboardSince returns the newest clipboard write from any pane, shown or
// not, numbered after seen, and advances seen past it; "" when there is none.
func (s *session) clipboardSince(seen *uint64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, latest := "", *seen
	for _, p := range s.panes {
		p.mu.Lock()
		if p.clipboardSeq > latest {
			text, latest = p.clipboard, p.clipboardSeq
		}
		p.mu.Unlock()
	}
	*seen = latest
	return text
}
