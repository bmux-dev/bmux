package server

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/update"
)

// updateCheckTimeout bounds one release check; a slow network never holds
// anything up, the hint just doesn't show.
const updateCheckTimeout = 5 * time.Second

// releases is what the server knows about newer bmux releases. One server
// runs per process, so like the settings it is process-wide; frames carry
// the resulting hint, so every attached client shows it.
var releases struct {
	mu        sync.Mutex
	current   string // this server's version; "" before WatchReleases
	latest    string // newest release seen by the check
	installed string // reported by bmux update; switching needs bmux restart
}

// updateHint is read while building frames.
var updateHint atomic.Pointer[protocol.UpdateHint]

// WatchReleases runs latest now and every interval until stop closes, while
// the config file's update.check is on. latest is update.Checker.Latest,
// which asks GitHub at most once a day and reads its cache otherwise.
func (s *Server) WatchReleases(current string, latest func(context.Context) (string, error), interval time.Duration, stop <-chan struct{}) {
	releases.mu.Lock()
	releases.current = current
	releases.mu.Unlock()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if currentSettings().updateCheck {
			ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
			version, err := latest(ctx)
			cancel()
			if err != nil {
				fmt.Fprintln(os.Stderr, "bmux: update check:", err)
			}
			releases.mu.Lock()
			releases.latest = version
			releases.mu.Unlock()
			s.publishUpdate()
		}
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

// updateInstalled records that bmux update replaced this server's binary.
func (s *Server) updateInstalled(version string) error {
	if !update.Valid(version) {
		return fmt.Errorf("%q is not a version", version)
	}
	releases.mu.Lock()
	releases.installed = version
	releases.mu.Unlock()
	s.publishUpdate()
	return nil
}

// publishUpdate recomputes the hint and, when it changed, sends attached
// clients new frames. An installed update wins over an available one.
func (s *Server) publishUpdate() {
	releases.mu.Lock()
	var hint *protocol.UpdateHint
	switch {
	case update.Newer(releases.installed, releases.current):
		hint = &protocol.UpdateHint{Version: releases.installed, Installed: true}
	case currentSettings().updateCheck && update.Newer(releases.latest, releases.current):
		hint = &protocol.UpdateHint{Version: releases.latest}
	}
	old := updateHint.Load()
	changed := (hint == nil) != (old == nil) || (hint != nil && *hint != *old)
	if changed {
		updateHint.Store(hint)
	}
	releases.mu.Unlock()
	if changed {
		s.bumpRevisions()
	}
}
