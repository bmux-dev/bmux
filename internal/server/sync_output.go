package server

import (
	"bytes"
	"time"
)

// Synchronized updates (DEC mode 2026): applications such as TUI agents wrap
// each redraw in begin/end markers so terminals never show it half drawn.
// Frames read the emulator at any moment, so syncOutput holds a pending
// update's bytes and releases it whole. Every byte still reaches the emulator.
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
	// Like other terminals, give up on an update that never ends.
	syncTimeout  = time.Second
	syncMaxBytes = 4 << 20
	// A begin marker split across reads is held briefly, then released as-is.
	syncPartialTimeout = 10 * time.Millisecond
)

type syncOutput struct {
	held    []byte
	syncing bool
	since   time.Time
}

// feed accepts PTY output and returns the bytes ready for the emulator.
func (o *syncOutput) feed(data []byte, now time.Time) []byte {
	if len(o.held) == 0 {
		o.since = now
	}
	buf := append(o.held, data...)
	var ready []byte
	for {
		if o.syncing {
			i := bytes.Index(buf, []byte(syncEnd))
			if i < 0 {
				if len(buf) > syncMaxBytes {
					ready, buf, o.syncing = append(ready, buf...), nil, false
				}
				break
			}
			end := i + len(syncEnd)
			ready, buf, o.syncing = append(ready, buf[:end]...), buf[end:], false
			o.since = now
			continue
		}
		i := bytes.Index(buf, []byte(syncBegin))
		if i < 0 {
			keep := partialSuffix(buf, syncBegin)
			ready, buf = append(ready, buf[:len(buf)-keep]...), buf[len(buf)-keep:]
			break
		}
		begin := i + len(syncBegin)
		ready, buf, o.syncing = append(ready, buf[:begin]...), buf[begin:], true
		o.since = now
	}
	o.held = append(o.held[:0:0], buf...)
	return ready
}

// deadline is when held bytes must be released without an end marker, or
// the zero time when nothing is held.
func (o *syncOutput) deadline() time.Time {
	switch {
	case o.syncing:
		return o.since.Add(syncTimeout)
	case len(o.held) > 0:
		return o.since.Add(syncPartialTimeout)
	}
	return time.Time{}
}

// flush releases everything held and abandons the pending update.
func (o *syncOutput) flush() []byte {
	held := o.held
	o.held, o.syncing = nil, false
	return held
}

// partialSuffix is the length of the longest suffix of data that begins marker.
func partialSuffix(data []byte, marker string) int {
	for n := min(len(data), len(marker)-1); n > 0; n-- {
		if bytes.Equal(data[len(data)-n:], []byte(marker[:n])) {
			return n
		}
	}
	return 0
}
