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
	// Cap buffer capacity retained across flushes so a multi-MB burst is not
	// pinned forever.
	syncRetainCap = 64 << 10
)

var (
	syncBeginBytes = []byte(syncBegin)
	syncEndBytes   = []byte(syncEnd)
)

type syncOutput struct {
	held    []byte
	ready   []byte
	scan    int // next offset in held to search for syncEnd while syncing
	syncing bool
	since   time.Time
}

// feed accepts PTY output and returns the bytes ready for the emulator.
//
// Callers must consume the returned slice synchronously before calling feed or
// flush again or reusing data's backing array (pane.readOutput satisfies this
// because pane.apply synchronously consumes output in term.Write and copies it
// in pipe.write before the next pty.Read).
func (o *syncOutput) feed(data []byte, now time.Time) []byte {
	if cap(o.ready) > syncRetainCap {
		o.ready = nil
	}
	if len(data) == 0 {
		return nil
	}
	// Fast path (the 99% normal PTY case): not syncing, nothing held, and no
	// syncBegin marker in data. Return data (or its non-partial prefix) zero-copy.
	if !o.syncing && len(o.held) == 0 {
		if cap(o.held) > syncRetainCap {
			o.held = nil
		}
		if bytes.Index(data, syncBeginBytes) < 0 {
			keep := partialSuffix(data, syncBeginBytes)
			if keep == 0 {
				return data
			}
			o.since = now
			o.held = append(o.held[:0], data[len(data)-keep:]...)
			return data[:len(data)-keep]
		}
	}

	if len(o.held) == 0 {
		o.since = now
	}
	var buf []byte
	if len(o.held) > 0 {
		o.held = append(o.held, data...)
		buf = o.held
	} else {
		buf = data
	}

	ready := o.ready[:0]
	for {
		if o.syncing {
			i := bytes.Index(buf[o.scan:], syncEndBytes)
			if i < 0 {
				if len(buf) > syncMaxBytes {
					ready, buf, o.syncing, o.scan = append(ready, buf...), nil, false, 0
				} else {
					o.scan = max(0, len(buf)-(len(syncEndBytes)-1))
				}
				break
			}
			end := o.scan + i + len(syncEndBytes)
			ready, buf, o.syncing, o.scan = append(ready, buf[:end]...), buf[end:], false, 0
			o.since = now
			continue
		}
		i := bytes.Index(buf, syncBeginBytes)
		if i < 0 {
			keep := partialSuffix(buf, syncBeginBytes)
			ready, buf = append(ready, buf[:len(buf)-keep]...), buf[len(buf)-keep:]
			break
		}
		begin := i + len(syncBeginBytes)
		ready, buf, o.syncing, o.scan = append(ready, buf[:begin]...), buf[begin:], true, 0
		o.since = now
	}

	switch {
	case len(buf) == 0:
		if cap(o.held) > syncRetainCap {
			o.held = nil
		} else {
			o.held = o.held[:0]
		}
		o.scan = 0
	case len(o.held) > 0 && &buf[0] == &o.held[0]:
		o.held = o.held[:len(buf)]
	case cap(o.held) > syncRetainCap && len(buf) <= syncRetainCap:
		o.held = append([]byte(nil), buf...)
	default:
		o.held = append(o.held[:0], buf...)
	}

	o.ready = ready
	if len(ready) == 0 {
		return nil
	}
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
// Like feed, callers must consume the returned slice before the next feed/flush.
func (o *syncOutput) flush() []byte {
	if cap(o.ready) > syncRetainCap {
		o.ready = nil
	}
	o.syncing = false
	o.scan = 0
	if len(o.held) == 0 {
		if cap(o.held) > syncRetainCap {
			o.held = nil
		}
		return nil
	}
	held := o.held
	if cap(o.held) > syncRetainCap {
		o.held = nil
	} else {
		o.held = o.held[:0]
	}
	return held
}

// partialSuffix is the length of the longest suffix of data that begins marker.
func partialSuffix(data, marker []byte) int {
	maxLen := min(len(data), len(marker)-1)
	if maxLen == 0 {
		return 0
	}
	tail := data[len(data)-maxLen:]
	if bytes.IndexByte(tail, marker[0]) < 0 {
		return 0
	}
	for n := maxLen; n > 0; n-- {
		if bytes.Equal(data[len(data)-n:], marker[:n]) {
			return n
		}
	}
	return 0
}
