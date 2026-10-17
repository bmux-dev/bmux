package server

import (
	"fmt"
	"io"
	"sync"

	"github.com/josephferrero/bmux/internal/protocol"
)

// MaxPendingInputBytes admits the maximum logical paste plus terminal markers
// into an empty queue. It includes reservations, queued and in-flight user bytes.
const MaxPendingInputBytes = protocol.MaxPasteBytes + protocol.PasteMarkerBytes
const maxPendingReplyBytes = 64 << 10

type inputChunk struct {
	data []byte
	user bool
}

// beginUserInput, finishUserInput and fenceGeneratedInput run under pane.mu.
// The drain needs only the short queue lock, never the state lock, and never
// waits for the independent PTY writer. All accounting is guarded by mu.
type inputBridge struct {
	mu                   sync.Mutex
	queue                []inputChunk
	admittingUserInput   bool
	remainingReservation int
	// While open, pendingUser = remainingReservation + queued user bytes +
	// the writer's in-flight user chunk. Generated replies have a separate
	// budget and can be dropped; while open, accepted user bytes are lossless.
	pendingUser, pendingReply int
	droppedReplies            uint64
	closed                    bool
	err                       error
	wake                      chan struct{}
	done                      chan struct{}
	drainDone                 chan struct{}
	writerDone                chan struct{}
}

func newInputBridge(reader io.Reader, writer io.Writer) *inputBridge {
	b := &inputBridge{wake: make(chan struct{}, 1), done: make(chan struct{}), drainDone: make(chan struct{}), writerDone: make(chan struct{})}
	go b.drain(reader)
	go b.write(writer)
	return b
}

// beginUserInput reserves the whole encoded intent before touching x/vt.
// Saturation rejects the entire intent without changing admission/accounting.
func (b *inputBridge) beginUserInput(size int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("pane input closed: %v", b.err)
	}
	if size > MaxPendingInputBytes-b.pendingUser {
		return fmt.Errorf("pane input queue full; entire request rejected (limit %d bytes)", MaxPendingInputBytes)
	}
	b.admittingUserInput, b.remainingReservation = true, size
	b.pendingUser += size
	return nil
}

// fenceGeneratedInput waits for the drain, not the independent PTY writer.
// Fence generated replies before beginning user admission so they cannot consume
// its reservation. The same fence ends admission before classifying more replies.
func (b *inputBridge) fenceGeneratedInput(writer io.Writer) error {
	// A zero-length io.Pipe write requires a Read. When it returns the
	// single drain has processed every preceding nonempty Read. The pending
	// zero Read carries no bytes, so changing classification is now safe.
	_, err := writer.Write(nil)
	return err
}

// finishUserInput releases unused reservation, including on cancellation.
// Queued/in-flight bytes remain charged until written; cancellation instead
// discards the queue and the IO owner interrupts IO. Accepted bytes may then be
// undelivered, and accounting is no longer used for admission.
func (b *inputBridge) finishUserInput(writer io.Writer) error {
	err := b.fenceGeneratedInput(writer)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pendingUser -= b.remainingReservation
	b.remainingReservation, b.admittingUserInput = 0, false
	if b.closed {
		return fmt.Errorf("pane input interrupted; accepted bytes may be undelivered: %v", b.err)
	}
	return err
}

func (b *inputBridge) drain(reader io.Reader) {
	defer close(b.drainDone)
	buf := make([]byte, 32768)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			b.mu.Lock()
			if !b.closed {
				if b.admittingUserInput {
					if n > b.remainingReservation {
						panic("emulator input exceeded admission reservation")
					}
					b.remainingReservation -= n
					b.append(buf[:n], true)
				} else if n <= maxPendingReplyBytes-b.pendingReply {
					b.pendingReply += n
					b.append(buf[:n], false)
				} else {
					b.droppedReplies += uint64(n)
				}
			}
			b.mu.Unlock()
			b.notify()
		}
		if err != nil {
			return
		}
	}
}

// Coalesce small keys/replies so the byte ceiling also has modest metadata
// overhead. Never mutate a chunk already removed by the independent writer.
func (b *inputBridge) append(data []byte, user bool) {
	if len(b.queue) > 0 {
		last := &b.queue[len(b.queue)-1]
		if last.user == user && len(last.data)+len(data) <= 32768 {
			last.data = append(last.data, data...)
			return
		}
	}
	b.queue = append(b.queue, inputChunk{append([]byte(nil), data...), user})
}

func (b *inputBridge) notify() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// close cancels admission and releases queued storage without waiting for IO.
// The owner must also close the input pipe and unblock the independent writer.
func (b *inputBridge) close(err error) {
	b.mu.Lock()
	if !b.closed {
		b.closed, b.err = true, err
		b.queue = nil
		close(b.done)
	}
	b.mu.Unlock()
	b.notify()
}

func (b *inputBridge) write(writer io.Writer) {
	defer close(b.writerDone)
	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		if len(b.queue) == 0 {
			b.mu.Unlock()
			select {
			case <-b.wake:
			case <-b.done:
				return
			}
			continue
		}
		chunk := b.queue[0]
		b.queue[0] = inputChunk{}
		b.queue = b.queue[1:]
		b.mu.Unlock()
		data := chunk.data
		for len(data) > 0 {
			n, err := writer.Write(data)
			data = data[n:]
			if err != nil {
				b.close(err)
				return
			}
			if n == 0 {
				b.close(io.ErrShortWrite)
				return
			}
		}
		b.mu.Lock()
		if chunk.user {
			b.pendingUser -= len(chunk.data)
		} else {
			b.pendingReply -= len(chunk.data)
		}
		b.mu.Unlock()
	}
}
