package server

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/vt"
)

func awaitBridgeAccounting(t *testing.T, b *inputBridge, user, reply int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		b.mu.Lock()
		pendingUser, pendingReply := b.pendingUser, b.pendingReply
		b.mu.Unlock()
		if pendingUser == user && pendingReply == reply {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending user/reply = %d/%d, want %d/%d", pendingUser, pendingReply, user, reply)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBridgeMaximumUnbracketedPasteReleasesMarkerReservation(t *testing.T) {
	term := vt.NewEmulator(80, 24)
	w := &gatedWriter{gate: make(chan struct{})}
	b := newInputBridge(term, w)
	var release sync.Once
	t.Cleanup(func() {
		b.close(io.ErrClosedPipe)
		_ = term.InputPipe().(io.Closer).Close()
		release.Do(func() { close(w.gate) })
	})
	payload := strings.Repeat("u", protocol.MaxPasteBytes)
	if err := b.beginUserInput(MaxPendingInputBytes + 1); err == nil {
		t.Fatal("oversized encoded intent accepted into empty queue")
	}
	awaitBridgeAccounting(t, b, 0, 0)
	bounded(t, 2*time.Second, func() {
		if err := b.beginUserInput(MaxPendingInputBytes); err != nil {
			t.Error(err)
			return
		}
		term.Paste(payload)
		if err := b.finishUserInput(term.InputPipe()); err != nil {
			t.Error(err)
		}
	})
	awaitBridgeAccounting(t, b, protocol.MaxPasteBytes, 0)
	// With no terminal markers emitted, their conservative reservation is free.
	if err := b.beginUserInput(protocol.PasteMarkerBytes); err != nil {
		t.Fatal("unused marker reservation was not released:", err)
	}
	bounded(t, time.Second, func() {
		if err := b.finishUserInput(term.InputPipe()); err != nil {
			t.Error(err)
		}
	})
	release.Do(func() { close(w.gate) })
	awaitBridgeAccounting(t, b, 0, 0)
	w.mu.Lock()
	defer w.mu.Unlock()
	if !bytes.Equal(w.data.Bytes(), []byte(payload)) {
		t.Fatal("maximum unbracketed payload was changed")
	}
}

func TestBridgeGeneratedFenceAndMixedReplyAccounting(t *testing.T) {
	r, pipe := io.Pipe()
	w := &gatedWriter{gate: make(chan struct{})}
	b := newInputBridge(r, w)
	var release sync.Once
	t.Cleanup(func() {
		b.close(io.ErrClosedPipe)
		_ = pipe.Close()
		_ = r.Close()
		release.Do(func() { close(w.gate) })
	})
	before, user, after := "reply-before", "user-intent", "reply-after"
	bounded(t, time.Second, func() {
		if _, err := pipe.Write([]byte(before)); err != nil {
			t.Error(err)
		}
		if err := b.fenceGeneratedInput(pipe); err != nil {
			t.Error(err)
		}
	})
	awaitBridgeAccounting(t, b, 0, len(before))
	bounded(t, time.Second, func() {
		// A full user reservation still fits despite queued/in-flight replies.
		if err := b.beginUserInput(MaxPendingInputBytes); err != nil {
			t.Error(err)
			return
		}
		if _, err := pipe.Write([]byte(user)); err != nil {
			t.Error(err)
		}
		if err := b.finishUserInput(pipe); err != nil {
			t.Error(err)
		}
		if _, err := pipe.Write([]byte(after)); err != nil {
			t.Error(err)
		}
		if err := b.fenceGeneratedInput(pipe); err != nil {
			t.Error(err)
		}
	})
	awaitBridgeAccounting(t, b, len(user), len(before)+len(after))
	b.mu.Lock()
	if b.remainingReservation != 0 || b.admittingUserInput || b.droppedReplies != 0 {
		t.Error("mixed input retained admission state or dropped small replies")
	}
	b.mu.Unlock()
	release.Do(func() { close(w.gate) })
	awaitBridgeAccounting(t, b, 0, 0)
	w.mu.Lock()
	defer w.mu.Unlock()
	if got := w.data.String(); got != before+user+after {
		t.Fatalf("mixed input = %q, want %q", got, before+user+after)
	}
}

func TestBridgeCancellationReleasesUnusedReservation(t *testing.T) {
	r, pipe := io.Pipe()
	w := &gatedWriter{gate: make(chan struct{})}
	b := newInputBridge(r, w)
	var release sync.Once
	t.Cleanup(func() {
		b.close(io.ErrClosedPipe)
		_ = pipe.Close()
		_ = r.Close()
		release.Do(func() { close(w.gate) })
	})
	if err := b.beginUserInput(16); err != nil {
		t.Fatal(err)
	}
	bounded(t, time.Second, func() {
		if _, err := pipe.Write([]byte("abc")); err != nil {
			t.Error(err)
		}
		if err := b.fenceGeneratedInput(pipe); err != nil {
			t.Error(err)
		}
	})
	b.mu.Lock()
	if b.pendingUser != 16 || b.remainingReservation != 13 {
		t.Errorf("before cancellation: pending=%d remaining=%d", b.pendingUser, b.remainingReservation)
	}
	b.mu.Unlock()
	b.close(io.ErrClosedPipe)
	_ = pipe.Close()
	bounded(t, time.Second, func() {
		if err := b.finishUserInput(pipe); err == nil || !strings.Contains(err.Error(), "accepted bytes may be undelivered") {
			t.Errorf("cancellation error = %v", err)
		}
	})
	b.mu.Lock()
	if b.remainingReservation != 0 || b.admittingUserInput || b.pendingUser != 3 || len(b.queue) != 0 {
		t.Errorf("after cancellation: remaining=%d admitting=%v pending=%d queued=%d", b.remainingReservation, b.admittingUserInput, b.pendingUser, len(b.queue))
	}
	b.mu.Unlock()
	if err := b.beginUserInput(1); err == nil {
		t.Fatal("cancelled bridge accepted a new intent")
	}
	// Cancellation of the bridge does not wait for its independent writer.
	// The IO owner also unblocks the reader/writer to complete shutdown.
	release.Do(func() { close(w.gate) })
	for _, done := range []chan struct{}{b.drainDone, b.writerDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("cancelled bridge goroutine did not finish")
		}
	}
}
