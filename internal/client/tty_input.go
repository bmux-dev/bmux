package client

import (
	"errors"
	"io"
	"time"
)

var errEscapeTimeout = errors.New("escape prefix timeout")

type terminalRead struct {
	data []byte
	err  error
}

// terminalInput permits a lone Escape to reach tcell even while the physical
// tty blocks waiting for its next byte. Only one bounded read is ahead. Drain
// cancels the consumer and wakes the underlying tty before the pump is joined.
type terminalInput struct {
	reads   chan terminalRead
	stopped <-chan struct{}
	done    chan struct{}
	pending []byte
	err     error
}

func newTerminalInput(reader io.Reader, stopped <-chan struct{}) *terminalInput {
	r := &terminalInput{reads: make(chan terminalRead, 1), stopped: stopped, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for {
			select {
			case <-stopped:
				return
			default:
			}
			buf := make([]byte, 4096)
			n, err := reader.Read(buf)
			select {
			case r.reads <- terminalRead{data: buf[:n], err: err}:
			case <-stopped:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return r
}

func (r *terminalInput) Read(p []byte) (int, error) { return r.read(p, 0) }

func (r *terminalInput) read(p []byte, timeout time.Duration) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 && r.err == nil {
		var deadline <-chan time.Time
		if timeout > 0 {
			timer := time.NewTimer(timeout)
			defer timer.Stop()
			deadline = timer.C
		}
		select {
		case result := <-r.reads:
			r.pending, r.err = result.data, result.err
		case <-r.stopped:
			return 0, io.EOF
		case <-deadline:
			return 0, errEscapeTimeout
		}
	}
	if len(r.pending) > 0 {
		n := copy(p, r.pending)
		r.pending = r.pending[n:]
		return n, nil
	}
	return 0, r.err
}
