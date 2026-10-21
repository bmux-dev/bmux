// Package graphics stores the images programs draw in panes with the Kitty
// graphics protocol. The server owns the images, like it owns text: apps never
// write to the attached terminal, and each client re-sends what it needs.
package graphics

import (
	"slices"
	"sync"
)

// Limits caps stored image bytes, as PNG, per pane and across the server.
type Limits struct {
	Pane, Total int64
}

// DefaultLimits are the production caps.
var DefaultLimits = Limits{Pane: 16 << 20, Total: 128 << 20}

// Info describes a stored image without its pixels.
type Info struct {
	// ID is the server-wide id; the outer terminal sees only this one.
	ID            uint32
	Width, Height int // pixels
	// Cols and Rows are the virtual placement's cell grid; both 0 until the
	// app places the image.
	Cols, Rows int
}

type stored struct {
	Info
	appID uint32
	png   []byte
	used  uint64 // LRU tick, larger is newer
	owner *Pane
}

// Hooks connect a pane's images to the rest of the pane. None may take the
// pane's own lock; Referenced is called with the store lock held.
type Hooks struct {
	// Supported reports whether some attached client can display images.
	Supported func() bool
	// CellPixels is the attached client's cell size; 0, 0 when unknown.
	CellPixels func() (width, height int)
	// Referenced lists app image ids that cells still show. owned says the
	// caller already holds the pane's lock; otherwise the hook must take it
	// without blocking and return false when it can't, which counts every
	// image of that pane as referenced.
	Referenced func(owned bool) (map[uint32]struct{}, bool)
}

// Store holds every pane's images and enforces the server-wide cap.
type Store struct {
	limits Limits
	mu     sync.Mutex // lock order: a pane's lock, then mu
	byID   map[uint32]*stored
	total  int64
	nextID uint32
	clock  uint64
}

func NewStore(limits Limits) *Store {
	return &Store{limits: limits, byID: make(map[uint32]*stored)}
}

// Pane is one pane's image space: the ids its app uses, mapped to images in
// the shared store. Handle must be called from one goroutine at a time (the
// pane's emulator); everything else is safe from any goroutine.
type Pane struct {
	store   *Store
	hooks   Hooks
	pending *transfer // chunked transmission in progress
	images  map[uint32]*stored
	bytes   int64
	closed  bool
}

func (s *Store) NewPane(hooks Hooks) *Pane {
	return &Pane{store: s, hooks: hooks, images: make(map[uint32]*stored)}
}

// Close drops all of the pane's images; later commands do nothing.
func (p *Pane) Close() {
	s := p.store
	s.mu.Lock()
	defer s.mu.Unlock()
	p.closed = true
	for _, img := range p.images {
		s.remove(img)
	}
}

// Image returns the pane's image for an app id.
func (p *Pane) Image(appID uint32) (Info, bool) {
	s := p.store
	s.mu.Lock()
	defer s.mu.Unlock()
	img := p.images[appID]
	if img == nil {
		return Info{}, false
	}
	s.clock++
	img.used = s.clock
	return img.Info, true
}

// Data returns the PNG bytes of an image by server-wide id. The slice is
// shared; callers must not modify it.
func (s *Store) Data(id uint32) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	img := s.byID[id]
	if img == nil {
		return nil, false
	}
	return img.png, true
}

// Bytes is the total PNG bytes stored.
func (s *Store) Bytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

// Bytes is the PNG bytes this pane has stored.
func (p *Pane) Bytes() int64 {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	return p.bytes
}

// remove forgets an image. Caller holds s.mu. Safe to call twice.
func (s *Store) remove(img *stored) {
	if img.owner.images[img.appID] != img {
		return
	}
	delete(img.owner.images, img.appID)
	delete(s.byID, img.ID)
	img.owner.bytes -= int64(len(img.png))
	s.total -= int64(len(img.png))
}

// newID returns an unused server-wide id that fits a placeholder's 24-bit
// foreground color. Ids aren't reused while the image lives, and a long-lived
// server wraps around past the live ones.
func (s *Store) newID() uint32 {
	for {
		s.nextID = s.nextID%MaxAppID + 1
		if s.byID[s.nextID] == nil {
			return s.nextID
		}
	}
}

// put stores a pane's image under its app id, replacing any earlier one, and
// evicts others to fit. The error is Kitty error text.
func (p *Pane) put(appID uint32, l loaded) (Info, string) {
	s := p.store
	size := int64(len(l.png))
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.closed {
		return Info{}, "ENOENT:pane closed"
	}
	if old := p.images[appID]; old != nil {
		s.remove(old)
	}
	if size > s.limits.Pane || size > s.limits.Total {
		return Info{}, "ENOSPC:image exceeds the storage limit"
	}
	p.makeRoomLocked(size)
	s.clock++
	img := &stored{Info: Info{ID: s.newID(), Width: l.width, Height: l.height}, appID: appID, png: l.png, used: s.clock, owner: p}
	p.images[appID], s.byID[img.ID] = img, img
	p.bytes += size
	s.total += size
	return img.Info, ""
}

// makeRoomLocked evicts until size more bytes fit the pane cap, then the
// server cap. Images no cell shows go first, least recently used first; an
// image cells still show is evicted only when nothing else frees enough, and
// those cells then draw blank.
func (p *Pane) makeRoomLocked(size int64) {
	s := p.store
	own := func() bool { return p.bytes+size > s.limits.Pane }
	all := func() bool { return s.total+size > s.limits.Total }
	if own() {
		s.evictLocked(p, own, slices.Collect(mapValues(p.images)))
	}
	if all() {
		s.evictLocked(p, all, slices.Collect(mapValues(s.byID)))
	}
}

func mapValues[K comparable, V any](m map[K]V) func(yield func(V) bool) {
	return func(yield func(V) bool) {
		for _, v := range m {
			if !yield(v) {
				return
			}
		}
	}
}

func (s *Store) evictLocked(requester *Pane, over func() bool, candidates []*stored) {
	slices.SortFunc(candidates, func(a, b *stored) int {
		switch {
		case a.used < b.used:
			return -1
		case a.used > b.used:
			return 1
		}
		return int(a.ID) - int(b.ID)
	})
	refs := make(map[*Pane]map[uint32]struct{})
	referenced := func(img *stored) bool {
		set, scanned := refs[img.owner]
		if !scanned {
			set = nil
			if hook := img.owner.hooks.Referenced; hook == nil {
				set = map[uint32]struct{}{}
			} else if got, ok := hook(img.owner == requester); ok {
				set = got
			}
			refs[img.owner] = set
		}
		if set == nil {
			return true // unknown: assume shown
		}
		_, shown := set[img.appID]
		return shown
	}
	for _, img := range candidates {
		if !over() {
			return
		}
		if !referenced(img) {
			s.remove(img)
		}
	}
	for _, img := range candidates {
		if !over() {
			return
		}
		s.remove(img)
	}
}

// place records the image's virtual placement grid.
func (p *Pane) place(appID uint32, cols, rows int) (Info, bool) {
	s := p.store
	s.mu.Lock()
	defer s.mu.Unlock()
	img := p.images[appID]
	if img == nil {
		return Info{}, false
	}
	img.Cols, img.Rows = cols, rows
	s.clock++
	img.used = s.clock
	return img.Info, true
}

// unplace removes the placements of the images match accepts and, with free,
// the images too. A freed image's cells draw blank.
func (p *Pane) unplace(free bool, match func(appID uint32) bool) {
	s := p.store
	s.mu.Lock()
	defer s.mu.Unlock()
	for appID, img := range p.images {
		if !match(appID) {
			continue
		}
		if free {
			s.remove(img)
		} else {
			img.Cols, img.Rows = 0, 0
		}
	}
}
