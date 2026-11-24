package server

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func attachTestClient(t *testing.T, addr, name string, width, height int) (net.Conn, *json.Encoder, <-chan *protocol.Frame) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	if err := enc.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: name, Width: width, Height: height}); err != nil {
		t.Fatal(err)
	}
	var r protocol.Response
	if err := dec.Decode(&r); err != nil || r.Error != "" {
		t.Fatalf("attach: %+v %v", r, err)
	}
	frames := make(chan *protocol.Frame, 256)
	go func() {
		defer close(frames)
		for {
			var r protocol.Response
			if dec.Decode(&r) != nil {
				return
			}
			if r.Frame != nil {
				frames <- r.Frame
			}
		}
	}()
	return c, enc, frames
}

func waitFrameMatching(t *testing.T, frames <-chan *protocol.Frame, timeout time.Duration, pred func(*protocol.Frame) bool) *protocol.Frame {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("attachment closed before matching frame")
			}
			if pred(f) {
				return f
			}
		case <-timer.C:
			t.Fatal("timed out waiting for matching frame")
		}
	}
}

func TestEventPushImmediateAfterIdle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- srv.Serve(l) }()
	defer func() {
		srv.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	// Use a small 20x4 viewport so JSON serialization overhead under -race
	// doesn't mask event-driven wakeup latency.
	if r := srv.request(protocol.Request{Op: "create", Name: "push", Dir: t.TempDir(), Width: 20, Height: 4}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s := srv.sessions["push"]
	_, _, frames := attachTestClient(t, l.Addr().String(), "push", 20, 4)
	// Initial frame arrives immediately without waiting on a 33ms ticker.
	waitFrameMatching(t, frames, time.Second, func(f *protocol.Frame) bool { return len(f.Panes) == 1 })

	// Measure several idle -> dirty wakeups and take the best latency so a
	// single scheduler pause under -race doesn't flake while still proving we
	// wake immediately (<2ms) instead of waiting on a 30 Hz polling ticker.
	best := time.Hour
	for i := range 5 {
		// Wait past frameBurstWindow so the next dirty event flushes immediately.
		time.Sleep(frameBurstWindow + 15*time.Millisecond)
		// Drain any stray shell startup frames.
		for len(frames) > 0 {
			<-frames
		}
		marker := fmt.Sprintf("WAKE_%d", i)
		start := time.Now()
		s.panes[1].apply([]byte(marker + "\r\n"))
		waitFrameMatching(t, frames, time.Second, func(f *protocol.Frame) bool {
			return len(f.Panes) > 0 && strings.Contains(frameText(f.Panes[0].Terminal), marker)
		})
		if elapsed := time.Since(start); elapsed < best {
			best = elapsed
		}
	}
	if best > 2*time.Millisecond {
		t.Fatalf("best idle wakeup latency = %v, want <= 2ms (event-driven, not 30 Hz polling)", best)
	}
}

func TestEventPushCoalescesBurst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- srv.Serve(l) }()
	defer func() {
		srv.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	if r := srv.request(protocol.Request{Op: "create", Name: "burst", Dir: t.TempDir(), Width: 80, Height: 24}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s := srv.sessions["burst"]
	_, _, frames := attachTestClient(t, l.Addr().String(), "burst", 80, 24)
	waitFrameMatching(t, frames, time.Second, func(f *protocol.Frame) bool { return len(f.Panes) == 1 })

	const total = 200
	for i := range total {
		s.panes[1].apply([]byte(fmt.Sprintf("LINE_%03d\r\n", i)))
	}
	received := 0
	waitFrameMatching(t, frames, 2*time.Second, func(f *protocol.Frame) bool {
		received++
		return len(f.Panes) > 0 && strings.Contains(frameText(f.Panes[0].Terminal), "LINE_199")
	})
	if received >= total/2 {
		t.Fatalf("burst of %d updates sent %d frames; expected adaptive coalescing", total, received)
	}
}

func TestEventPushScrollDisplayMessageRenameAndExit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- srv.Serve(l) }()
	defer func() {
		srv.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	if r := srv.request(protocol.Request{Op: "create", Name: "ops", Dir: t.TempDir(), Width: 80, Height: 8}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s := srv.sessions["ops"]
	// Populate scrollback so scroll changes the viewport.
	for i := range 20 {
		s.panes[1].apply([]byte(fmt.Sprintf("hist-%d\r\n", i)))
	}
	_, enc, frames := attachTestClient(t, l.Addr().String(), "ops", 80, 8)
	waitFrameMatching(t, frames, time.Second, func(f *protocol.Frame) bool { return len(f.Panes) == 1 })

	// Scroll pushes a new frame with non-zero offset.
	if err := enc.Encode(protocol.Request{Op: "scroll", Delta: 5}); err != nil {
		t.Fatal(err)
	}
	waitFrameMatching(t, frames, time.Second, func(f *protocol.Frame) bool {
		return len(f.Panes) == 1 && f.Panes[0].Terminal.Offset > 0
	})

	// display-message pushes immediately to idle client.
	if r := srv.request(protocol.Request{Op: "display-message", Name: "ops", Text: "hello status"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	waitFrameMatching(t, frames, time.Second, func(f *protocol.Frame) bool { return f.Message == "hello status" })

	// rename pushes updated session name immediately to idle client.
	if r := srv.request(protocol.Request{Op: "rename", Name: "ops", NewName: "ops2"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	waitFrameMatching(t, frames, time.Second, func(f *protocol.Frame) bool { return f.Name == "ops2" })

	// Session kill pushes the final 0-pane frame promptly.
	if r := srv.request(protocol.Request{Op: "kill", Name: "ops2"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	waitFrameMatching(t, frames, 2*time.Second, func(f *protocol.Frame) bool { return len(f.Panes) == 0 })
}
