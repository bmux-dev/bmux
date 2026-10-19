package client

import (
	"context"
	"errors"
	"sync"
)

type clipboardResult struct {
	Seq       uint64 // the copy's sequence; 0 for an app's OSC 52 write
	Text      string // captured text for OSC 52 fallback when Available is false
	Available bool
	Err       error
}

type clipboardJob struct {
	seq  uint64
	text string
}

// clipboardWorker serializes copy intent off the UI goroutine. Submit never
// blocks; the UI must show its rejection error. Results retain sequence IDs so
// old completions need not overwrite the latest notice. Consume Results in the
// UI select and perform OSC 52 fallback there, never from this worker.
type clipboardWorker struct {
	Results <-chan clipboardResult
	jobs    chan clipboardJob
	results chan clipboardResult
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	bytes   int
}

func newClipboardWorker() *clipboardWorker {
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan clipboardResult, 1)
	w := &clipboardWorker{Results: results, results: results, jobs: make(chan clipboardJob, 8), ctx: ctx, cancel: cancel}
	go w.run()
	return w
}

func (w *clipboardWorker) Submit(seq uint64, text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ctx.Err() != nil {
		return errors.New("Clipboard copy rejected: client closed")
	}
	if len(text) > (32<<20)-w.bytes {
		return errors.New("Clipboard copy rejected: pending text exceeds 32 MiB limit")
	}
	select {
	case w.jobs <- clipboardJob{seq: seq, text: text}:
		w.bytes += len(text)
		return nil
	default:
		return errors.New("Clipboard copy rejected: helper queue is full")
	}
}

func (w *clipboardWorker) Close() { w.cancel() }

func (w *clipboardWorker) run() {
	defer close(w.results)
	for {
		select {
		case <-w.ctx.Done():
			return
		case job := <-w.jobs:
			if w.ctx.Err() != nil {
				return
			}
			available, err := copyNativeContext(w.ctx, job.text)
			result := clipboardResult{Seq: job.seq, Available: available, Err: err}
			if !available {
				result.Text = job.text
			}
			select {
			case w.results <- result:
			case <-w.ctx.Done():
				return
			}
			w.mu.Lock()
			w.bytes -= len(job.text)
			w.mu.Unlock()
		}
	}
}
