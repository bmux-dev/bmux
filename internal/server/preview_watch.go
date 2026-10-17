package server

import (
	"encoding/json"
	"net"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func (s *Server) watchPreview(conn net.Conn, r protocol.Request, reader *protocol.RequestReader, enc *json.Encoder) {
	s.mu.Lock()
	item := s.sessions[r.Name]
	closed := s.closed
	s.mu.Unlock()
	respond := func(result protocol.Response) bool {
		result.Version = protocol.Version
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		return enc.Encode(result) == nil
	}
	if item == nil || closed {
		respond(protocol.Response{Error: "session not found"})
		return
	}
	item.mu.Lock()
	generation := protocol.SessionGeneration(item.info.Created)
	item.mu.Unlock()
	if r.Generation != "" && r.Generation != generation {
		respond(protocol.Response{Error: "session changed"})
		return
	}
	// This reader exists only to detect disconnect/cancellation. Preview sockets
	// accept no app input, attachment, focus changes or recency mutations.
	done := make(chan struct{})
	go func() { defer close(done); _, _ = reader.Read() }()
	defer func() { _ = conn.Close(); <-done }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var revision uint64
	send := func() bool {
		f, rev := item.previewSnapshot(r.Width, r.Height, revision, revision == 0)
		revision = rev
		if f == nil {
			return respond(protocol.Response{NotModified: true, Revision: rev})
		}
		return respond(protocol.Response{Frame: f, Revision: rev}) && len(f.Panes) > 0
	}
	// Copy the existing source grid immediately. No PTY resize, application
	// repaint, initial refresh tick, or settling timer is needed for a viewport.
	if !send() {
		return
	}
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}
