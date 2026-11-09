package server

import (
	"sync"
	"time"
)

// Round up so even exact deadlines cannot fit 31 frames in a half-open second.
const frameInterval = (time.Second + 29) / 30

// frameChanges broadcasts coalesced hints, not state. Its lock is independent of
// session/pane state: producers may hold those locks, but consumers release this
// lock before taking a snapshot. A slow attachment never blocks a producer.
type frameChanges struct {
	mu          sync.Mutex
	subscribers map[chan struct{}]struct{}
}

func (c *frameChanges) subscribe() chan struct{} {
	wake := make(chan struct{}, 1)
	c.mu.Lock()
	if c.subscribers == nil {
		c.subscribers = make(map[chan struct{}]struct{})
	}
	c.subscribers[wake] = struct{}{}
	c.mu.Unlock()
	return wake
}

func (c *frameChanges) unsubscribe(wake chan struct{}) {
	c.mu.Lock()
	delete(c.subscribers, wake)
	c.mu.Unlock()
	// Don't close wake: the attachment owns its lifetime, not the producers.
}

func (c *frameChanges) notify() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for wake := range c.subscribers {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

// framePublication is driven by changes, with one fixed trailing deadline.
// Explicit timestamps keep the idle-latency and sustained-rate policy testable
// without wall-clock sleeps. Only a real frame consumes the publication budget.
type framePublication struct {
	dirty       bool
	nextAllowed time.Time
}

func (p *framePublication) delay(now time.Time) (time.Duration, bool) {
	return max(time.Duration(0), p.nextAllowed.Sub(now)), p.dirty
}

func (p *framePublication) sent(now time.Time) {
	p.nextAllowed = now.Add(frameInterval)
}

// Caller holds session.mu. Mirror revisionLocked's active-window membership,
// including panes hidden by zoom. Pane callbacks cannot acquire session.mu.
func (s *session) refreshFramePanesLocked() {
	w := s.activeLocked()
	for id, p := range s.panes {
		p.frameActive.Store(w != nil && w.root != nil && w.root.contains(id))
	}
}

// Caller holds pane.mu. Input admission alone is not a display change: waking
// on a key would spend the cooldown on the old screen before its echo arrives.
func (s *pane) notifyFrameLocked() {
	if s.frameActive.Load() && s.frameChanged != nil {
		s.frameChanged()
	}
}
