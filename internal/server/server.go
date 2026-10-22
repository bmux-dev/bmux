// Package server owns persistent PTYs and terminal state, independently of clients.
package server

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

type Server struct {
	// mu protects only registry/admission state. Release it before session/pane
	// state or OS work. operations serializes mutations, but Close/ping never need it.
	mu          sync.Mutex
	operations  sync.Mutex // registry mutations, never required by Close or ping
	sessions    map[string]*session
	connections map[net.Conn]bool
	listener    net.Listener
	shell       string
	closed      bool
	wg          sync.WaitGroup
	// persistMu serializes snapshot writes; see persist.go.
	persistMu    sync.Mutex
	persistPath  string
	persistStop  chan struct{}
	persistLast  []byte
	persistFinal bool
	configPath   string // guarded by persistMu
	// layoutTrustPath is the store of trusted layout files; see layoutfile.go.
	layoutTrustPath atomic.Pointer[string]
}

func New(shell string) *Server {
	return &Server{sessions: make(map[string]*session), connections: make(map[net.Conn]bool), shell: shell}
}

func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = l.Close()
		return nil
	}
	s.listener = l
	s.mu.Unlock()
	for {
		conn, err := l.Accept()
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			return err
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return nil
		}
		if len(s.connections) >= 64 {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		s.connections[conn] = true
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			defer func() { s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
			s.handle(conn)
		}()
	}
}

func (s *Server) Close() {
	// Save before anything stops: shutdown must not erase what is restored.
	s.finishPersistence()
	s.mu.Lock()
	s.closed = true
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for c := range s.connections {
		_ = c.Close()
	}
	items := make([]*session, 0, len(s.sessions))
	for _, item := range s.sessions {
		items = append(items, item)
	}
	s.mu.Unlock()
	deadline := time.Now().Add(paneShutdownBudget)
	var stops sync.WaitGroup
	for _, item := range items {
		stops.Add(1)
		go func(item *session) { defer stops.Done(); item.stopUntil(deadline) }(item)
	}
	stops.Wait()
	s.wg.Wait()
}

func validateName(name string) error {
	return protocol.ValidateSessionName(name)
}

func (s *Server) request(r protocol.Request) protocol.Response {
	result := protocol.Response{Version: protocol.Version}
	if r.Op == "create" || r.Op == "rename" || r.Op == "kill" {
		return s.mutate(r)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		result.Error = "server is shutting down"
		return result
	}
	for name, item := range s.sessions {
		select {
		case <-item.done:
			delete(s.sessions, name)
		default:
		}
	}
	item := s.sessions[r.Name]
	if (r.Op == "focus-pane" || r.Op == "zoomed" || r.Op == "agent-report" || r.Op == "capture-pane" || r.Op == "send-input" || r.Op == "layout-command" || r.Op == "describe" || r.Op == "display-message" || r.Op == "layout" || r.Op == "history-text" || r.Op == "search-history" || r.Op == "jump-prompt") && r.SessionID != 0 {
		// Never fall back to the name: after a rename it may belong to another session.
		item = nil
		for _, candidate := range s.sessions {
			if candidate.id == r.SessionID {
				item = candidate
			}
		}
	}
	type namedSession struct {
		name string
		item *session
	}
	items := make([]namedSession, 0, len(s.sessions))
	for name, item := range s.sessions {
		items = append(items, namedSession{name, item})
	}
	s.mu.Unlock()
	switch r.Op {
	case "ping":
	case "preview":
		if item == nil {
			result.Error = "session not found"
			break
		}
		if r.Kind == "overview" {
			item.mu.Lock()
			generation := protocol.SessionGeneration(item.info.Created)
			item.mu.Unlock()
			result.Frame, result.Revision = item.snapshot(make(map[int]int), make(map[int]int), r.Revision, r.Revision == 0 || r.Generation != generation)
			if result.Frame == nil {
				result.NotModified = true
			} else {
				result.Frame.PreviewMode = "overview"
			}
		} else {
			result.Frame = item.preview(r.Width, r.Height)
		}
	case "list":
		for _, entry := range items {
			if status := entry.item.status(); status.Panes > 0 {
				status.Name = entry.name
				result.Sessions = append(result.Sessions, status)
			}
		}
		sort.Slice(result.Sessions, func(i, j int) bool { return result.Sessions[i].Used.After(result.Sessions[j].Used) })
	case "agent-report":
		// Same pane-environment trust model as focus-pane: a process inside
		// the pane reports itself. Reports never change recent-use order.
		if item == nil {
			result.Error = "session not found"
			break
		}
		if err := item.reportAgent(r.PaneID, r.Kind, r.Text, r.Args); err != nil {
			result.Error = err.Error()
		}
	case "reload-config":
		if err := s.ReloadSettings(); err != nil {
			result.Error = err.Error()
		}
	case "update-installed":
		// bmux update replaced this server's binary; clients show that a
		// restart switches to it.
		if err := s.updateInstalled(r.Text); err != nil {
			result.Error = err.Error()
		}
	case "environment":
		// Panes start from this environment; bmux restart passes it on, so
		// a restart from inside a pane never leaks that pane's variables.
		result.Env = os.Environ()
	case "pane-command":
		// Kind "window" asks about every pane in window Window (0 for the
		// active one), before closing it.
		if item == nil {
			result.Error = "session not found"
			break
		}
		var command string
		var err error
		if r.Kind == "window" {
			command, err = item.windowCommand(r.Window)
		} else {
			command, err = item.paneCommand(r.PaneID)
		}
		if err != nil {
			result.Error = err.Error()
		}
		result.Command = command
	case "layout":
		// The session's windows as a layout file for its directory, for bmux
		// layout save to write there.
		if item == nil {
			result.Error = "session not found"
			break
		}
		item.mu.Lock()
		dir := item.info.Dir
		item.mu.Unlock()
		data, err := item.layout(dir)
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.Text, result.Dir = string(data), dir
	case "jump-prompt":
		if item == nil {
			result.Error = "session not found"
			break
		}
		row, err := item.jumpPrompt(r.PaneID, r.Range, r.Direction)
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.Range = &row
	case "search-history":
		if item == nil {
			result.Error = "session not found"
			break
		}
		match, err := item.searchHistory(r.PaneID, r.Range, r.Text, r.Direction)
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.Range = &match
	case "history-text":
		if item == nil {
			result.Error = "session not found"
			break
		}
		text, err := item.historyText(r.PaneID, r.Range)
		if err != nil {
			result.Error = err.Error()
		}
		result.Text = text
	case "zoomed":
		// Same pane-environment trust model as focus-pane, but read-only: it
		// never changes focus or the session's recent-use order.
		if item == nil {
			result.Error = "session not found"
			break
		}
		zoomed, err := item.paneZoomed(r.PaneID)
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.Zoomed = zoomed
	case "capture-pane":
		if item == nil {
			result.Error = "session not found"
			break
		}
		a, text, err := item.capturePane(targetOf(r))
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.Pane, result.Text = &a, text
	case "send-input":
		if item == nil {
			result.Error = "session not found"
			break
		}
		a, err := item.sendInput(targetOf(r), r.IfSeq, r.Input)
		if err != nil {
			result.Error = err.Error()
		}
		if a.Pane != 0 {
			result.Pane = &a
		}
	case "layout-command":
		if item == nil {
			result.Error = "session not found"
			break
		}
		a, err := item.layoutCommand(r)
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.Pane = &a
	case "describe":
		// Kind is the scope: pane, window, session, or all sessions.
		if r.Kind == "all" {
			sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
			for _, named := range items {
				panes, err := named.item.describe(paneTarget{}, "session")
				if err == nil {
					result.Panes = append(result.Panes, panes...)
				}
			}
			break
		}
		if item == nil {
			result.Error = "session not found"
			break
		}
		panes, err := item.describe(targetOf(r), r.Kind)
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.Panes = panes
	case "display-message":
		// Shown in the status line of every client attached to the session.
		if item == nil {
			result.Error = "session not found"
			break
		}
		item.showMessage(r.Text)
	default:
		if r.Op == "focus-pane" {
			// Standalone CLI focus changes (bmux focus), e.g. Neovim
			// handing off at an editor edge. Same trust model as kill.
			if item == nil {
				result.Error = "session not found"
				break
			}
			item.mu.Lock()
			item.info.Used = time.Now()
			item.mu.Unlock()
			if err := item.input(r); err != nil {
				result.Error = err.Error()
			}
			break
		}
		result.Error = "unknown operation"
	}
	return result
}

func (s *Server) handle(conn net.Conn) {
	reader, enc := protocol.NewRequestReader(conn, 5*time.Second), json.NewEncoder(conn)
	r, err := reader.Read()
	if err != nil {
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		_ = enc.Encode(protocol.Response{Version: protocol.Version, Error: err.Error()})
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	// Shutdown is accepted from any protocol version so a newer binary can
	// always stop this server for an upgrade; Close saves sessions first.
	if r.Op == "shutdown" {
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		_ = enc.Encode(protocol.Response{Version: protocol.Version})
		go s.Close()
		return
	}
	if r.Version != protocol.Version {
		_ = enc.Encode(protocol.Response{Version: protocol.Version, Error: "protocol version mismatch; use the same bmux binary for client and server"})
		return
	}
	if r.Op == "watch-viewport" || r.Op == "watch-preview" {
		s.watchPreview(conn, r, reader, enc)
		return
	}
	if r.Op != "attach" {
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_ = enc.Encode(s.request(r))
		return
	}
	s.mu.Lock()
	item := s.sessions[r.Name]
	s.mu.Unlock()
	if item != nil {
		item.mu.Lock()
		item.info.Clients++
		item.info.Used = time.Now()
		item.mu.Unlock()
	}
	if item == nil {
		_ = enc.Encode(protocol.Response{Version: protocol.Version, Error: "session not found"})
		return
	}
	item.hook("client-attached", 0)
	defer func() {
		item.mu.Lock()
		item.info.Clients--
		item.mu.Unlock()
		item.hook("client-detached", 0)
	}()
	if err := enc.Encode(protocol.Response{Version: protocol.Version}); err != nil {
		return
	}
	_ = item.input(protocol.Request{Op: "resize", Width: r.Width, Height: r.Height})
	requests := make(chan protocol.Request, 1)
	readErrors := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(requests)
		for {
			next, err := reader.Read()
			if err != nil {
				readErrors <- err
				return
			}
			select {
			case requests <- next:
			case <-done:
				return
			}
		}
	}()
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	offsets, histories := make(map[int]int), make(map[int]int)
	var revision uint64
	ack := 0
	force := true
	viewing := false
	noticed := false // the session's notice goes out with this attachment's first frame
	// Copies made before this attachment belong to whoever was attached then.
	clipboardSeen := clipboardWrites.Load()
	defer func() {
		if viewing {
			item.setViewing(false)
		}
	}()
	var paste protocol.PasteAssembly
	for {
		select {
		case err := <-readErrors:
			_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
			_ = enc.Encode(protocol.Response{Version: protocol.Version, Error: err.Error()})
			return
		case next, ok := <-requests:
			if !ok {
				select {
				case err := <-readErrors:
					_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
					_ = enc.Encode(protocol.Response{Version: protocol.Version, Error: err.Error()})
				default:
				}
				return
			}
			if next.Op == "detach" {
				return
			}
			ack = max(ack, next.Seq)
			complete, ready, pasteErr := paste.Accept(next)
			if pasteErr != nil {
				_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				_ = enc.Encode(protocol.Response{Version: protocol.Version, Ack: ack, Error: pasteErr.Error()})
				return
			}
			if !ready {
				continue
			}
			next = complete
			if next.Op == "focus" {
				// The client's terminal focus; panes see it through viewing.
				if next.Focused != viewing {
					viewing = next.Focused
					item.setViewing(viewing)
				}
				continue
			}
			if next.Op == "scroll" {
				item.mu.Lock()
				id := next.PaneID
				if id == 0 {
					id = item.focusedLocked()
				}
				exists := item.panes[id] != nil
				item.mu.Unlock()
				if !exists {
					continue
				}
				if next.Delta == 0 {
					offsets[id] = 0
				} else {
					offsets[id] += next.Delta
				}
				force = true
			} else if err := item.input(next); err != nil {
				_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if enc.Encode(protocol.Response{Version: protocol.Version, Ack: ack, Error: err.Error()}) != nil {
					return
				}
			}
		case <-ticker.C:
			frame, rev := item.snapshot(offsets, histories, revision, force)
			if frame == nil {
				continue
			}
			revision, force = rev, false
			frame.Clipboard = item.clipboardSince(&clipboardSeen)
			if !noticed {
				frame.Notice, noticed = item.takeNotice(), true
			}
			_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if enc.Encode(protocol.Response{Version: protocol.Version, Ack: ack, Frame: frame}) != nil {
				return
			}
			if len(frame.Panes) == 0 {
				return
			}
		}
	}
}
