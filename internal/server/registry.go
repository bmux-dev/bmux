package server

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func (s *Server) mutate(r protocol.Request) protocol.Response {
	s.operations.Lock()
	defer s.operations.Unlock()
	result := protocol.Response{Version: protocol.Version}
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
	if r.Op != "create" {
		if item == nil {
			s.mu.Unlock()
			result.Error = "session not found"
			return result
		}
		// Created is immutable for this session; only captured actions send this
		// token. Explicit CLI name actions intentionally target the current name.
		if r.Generation != "" && r.Generation != protocol.SessionGeneration(item.info.Created) {
			s.mu.Unlock()
			result.Error = "session changed; captured action cancelled"
			return result
		}
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	switch r.Op {
	case "create":
		if err := validateName(r.Name); err != nil {
			result.Error = err.Error()
			return result
		}
		s.mu.Lock()
		exists := s.sessions[r.Name] != nil
		s.mu.Unlock()
		if exists {
			result.Error = "session already exists"
			return result
		}
		dir, err := filepath.Abs(r.Dir)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		stat, err := os.Stat(dir)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		if !stat.IsDir() {
			result.Error = "working directory is not a directory"
			return result
		}
		created, err := s.createSession(r.Name, dir, r.Width, r.Height)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			created.stop()
			result.Error = "server is shutting down"
			return result
		}
		s.adoptLocked(r.Name, created)
		s.mu.Unlock()
	case "rename":
		if err := validateName(r.NewName); err != nil {
			result.Error = err.Error()
			return result
		}
		s.mu.Lock()
		if s.sessions[r.NewName] != nil {
			s.mu.Unlock()
			result.Error = "session already exists"
			return result
		}
		if s.sessions[r.Name] != item {
			s.mu.Unlock()
			result.Error = "session changed; action cancelled"
			return result
		}
		delete(s.sessions, r.Name)
		s.sessions[r.NewName] = item
		s.mu.Unlock()
		item.mu.Lock()
		item.info.Name = r.NewName
		item.revision++
		item.mu.Unlock()
	case "kill":
		s.mu.Lock()
		if s.sessions[r.Name] == item {
			delete(s.sessions, r.Name)
		}
		s.mu.Unlock()
		item.stopUntil(time.Now().Add(paneShutdownBudget))
	default:
		result.Error = fmt.Sprintf("unknown mutation %q", r.Op)
	}
	return result
}

// adoptLocked registers a session and removes it once its last pane is gone.
// Caller holds Server.mu.
func (s *Server) adoptLocked(name string, created *session) {
	s.sessions[name] = created
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-created.done
		s.mu.Lock()
		defer s.mu.Unlock()
		for name, current := range s.sessions {
			if current == created {
				delete(s.sessions, name)
				break
			}
		}
	}()
}
