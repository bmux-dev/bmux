package client

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

const (
	// The terminal keeps what this client sends until told otherwise; past
	// this much, the least recently shown images are deleted. They come back
	// from the server when they next appear.
	maxSentImageBytes = 64 << 20
	// transmitBudget bounds the image bytes written per redraw, so a large
	// image never stalls input for long. An image over it is sent alone.
	transmitBudget = 1 << 20
	// transmitChunk is the base64 size of one chunk of a transmission, as
	// the Kitty protocol limits it.
	transmitChunk = 4096
	// maxImageBytes bounds an image fetched from the server.
	maxImageBytes = 32 << 20
	// retryFetch is how long a failed fetch waits before another try.
	retryFetch = 2 * time.Second

	syncBegin   = "\x1b[?2026h"
	syncEnd     = "\x1b[?2026l"
	placeholder = "\U0010EEEE"
)

// graphicsEnv is how the client reaches its terminal's graphics: what the
// startup probe found and how to write to the terminal behind tcell's back.
type graphicsEnv struct {
	probe terminalProbe
	// cellPixels is one cell's size now; 0, 0 when unknown.
	cellPixels func() (width, height int)
	// write sends bytes to the terminal; nil when there is no terminal to
	// draw images on.
	write func([]byte) error
}

type imageResult struct {
	id         uint32
	generation int
	data       []byte
	err        error
}

type sentImage struct {
	cols, rows, size int
	used             uint64
}

// imageSender puts the server's images on this client's terminal in the best
// form it supports. Each image is sent once per connection: fetched from the
// server as PNG, transmitted with the Kitty protocol under its server-wide id,
// and given a virtual placement for the placeholder cells to show. All methods
// run on the UI goroutine; only fetches run elsewhere.
type imageSender struct {
	write   func([]byte) error
	fetch   func(id uint32) ([]byte, error)
	results chan<- imageResult
	done    <-chan struct{}

	sent     map[uint32]*sentImage // on the terminal
	ready    map[uint32][]byte     // fetched, not yet sent
	loading  map[uint32]bool
	failedAt map[uint32]time.Time
	bytes    int
	maxBytes int
	tick     uint64
	// generation counts resets; a fetch started before one is stale, since
	// the server's ids may mean other images now.
	generation int
}

func newImageSender(write func([]byte) error, fetch func(uint32) ([]byte, error), results chan<- imageResult, done <-chan struct{}) *imageSender {
	return &imageSender{write: write, fetch: fetch, results: results, done: done, maxBytes: maxSentImageBytes,
		sent: make(map[uint32]*sentImage), ready: make(map[uint32][]byte), loading: make(map[uint32]bool), failedAt: make(map[uint32]time.Time)}
}

// want notes the images on screen and starts fetching those it hasn't got.
func (s *imageSender) want(images []protocol.Image) {
	s.tick++
	shown := make(map[uint32]bool, len(images))
	for _, img := range images {
		id := uint32(img.ID)
		shown[id] = true
		if sent := s.sent[id]; sent != nil {
			sent.used = s.tick
			continue
		}
		if _, ok := s.ready[id]; ok || s.loading[id] {
			continue
		}
		if at, failed := s.failedAt[id]; failed && time.Since(at) < retryFetch {
			continue
		}
		delete(s.failedAt, id)
		s.loading[id] = true
		generation := s.generation
		go func() {
			data, err := s.fetch(id)
			select {
			case s.results <- imageResult{id, generation, data, err}:
			case <-s.done:
			}
		}()
	}
	for id := range s.failedAt {
		if !shown[id] {
			delete(s.failedAt, id)
		}
	}
}

// accept takes a finished fetch.
func (s *imageSender) accept(r imageResult) {
	if r.generation != s.generation {
		return
	}
	delete(s.loading, r.id)
	if r.err != nil || len(r.data) == 0 {
		s.failedAt[r.id] = time.Now()
		return
	}
	s.ready[r.id] = r.data
}

// flush sends fetched images that are on screen, re-places any whose grid
// changed, and deletes the least recently shown past the size limit. more
// reports images still waiting for a later redraw.
func (s *imageSender) flush(images []protocol.Image) (more bool, err error) {
	var out bytes.Buffer
	budget := transmitBudget
	shown := make(map[uint32]bool, len(images))
	for _, img := range images {
		id := uint32(img.ID)
		shown[id] = true
		if img.Cols < 1 || img.Rows < 1 {
			continue
		}
		if sent := s.sent[id]; sent != nil {
			if sent.cols != img.Cols || sent.rows != img.Rows {
				appendPlacement(&out, id, img.Cols, img.Rows)
				sent.cols, sent.rows = img.Cols, img.Rows
			}
			continue
		}
		data, ok := s.ready[id]
		if !ok {
			continue
		}
		if out.Len() > 0 && len(data) > budget {
			more = true
			continue
		}
		appendTransmit(&out, id, data)
		appendPlacement(&out, id, img.Cols, img.Rows)
		budget -= len(data)
		s.sent[id] = &sentImage{cols: img.Cols, rows: img.Rows, size: len(data), used: s.tick}
		s.bytes += len(data)
		delete(s.ready, id)
	}
	for id := range s.ready {
		if !shown[id] {
			delete(s.ready, id)
		}
	}
	for s.bytes > s.maxBytes {
		var oldest uint32
		for id, sent := range s.sent {
			if !shown[id] && (oldest == 0 || sent.used < s.sent[oldest].used) {
				oldest = id
			}
		}
		if oldest == 0 {
			break
		}
		s.forget(&out, oldest)
	}
	return more, s.emit(&out)
}

func (s *imageSender) forget(out *bytes.Buffer, id uint32) {
	s.bytes -= s.sent[id].size
	delete(s.sent, id)
	fmt.Fprintf(out, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

// reset deletes everything this client put on the terminal and forgets it, for
// a lost connection (the server's ids start over) or exit.
func (s *imageSender) reset() error {
	var out bytes.Buffer
	for id := range s.sent {
		s.forget(&out, id)
	}
	clear(s.ready)
	clear(s.failedAt)
	clear(s.loading)
	s.generation++
	return s.emit(&out)
}

// emit writes the commands inside synchronized output, so the terminal
// applies them with the next frame instead of between redraws.
func (s *imageSender) emit(out *bytes.Buffer) error {
	if out.Len() == 0 || s.write == nil {
		return nil
	}
	return s.write(append(append([]byte(syncBegin), out.Bytes()...), syncEnd...))
}

// appendTransmit adds a chunked transmission of PNG bytes. It names no file or
// shared memory: the pixels travel in the sequence itself.
func appendTransmit(out *bytes.Buffer, id uint32, png []byte) {
	encoded := base64.StdEncoding.EncodeToString(png)
	for first := true; len(encoded) > 0; first = false {
		chunk := encoded[:min(len(encoded), transmitChunk)]
		encoded = encoded[len(chunk):]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(out, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\", id, more, chunk)
		} else {
			fmt.Fprintf(out, "\x1b_Gm=%d,q=2;%s\x1b\\", more, chunk)
		}
	}
}

func appendPlacement(out *bytes.Buffer, id uint32, cols, rows int) {
	fmt.Fprintf(out, "\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, rows)
}

// fetchImage downloads an image's PNG from the server, chunk by chunk.
func fetchImage(socket string, id uint32) ([]byte, error) {
	var data []byte
	for {
		response, err := Request(socket, protocol.Request{Op: "image-data", ImageID: id, Offset: len(data)})
		if err != nil {
			return nil, err
		}
		chunk := response.ImageChunk
		if chunk == nil || chunk.Offset != len(data) || chunk.Total > maxImageBytes || len(chunk.Data) == 0 {
			return nil, fmt.Errorf("unexpected image chunk")
		}
		data = append(data, chunk.Data...)
		if len(data) >= chunk.Total {
			return data, nil
		}
	}
}

// isPlaceholder reports whether a cell is part of an image.
func isPlaceholder(c protocol.Cell) bool { return strings.HasPrefix(c.Text, placeholder) }

// placeholderID is the image id a placeholder cell carries in its foreground.
func placeholderID(c protocol.Cell) (uint32, bool) {
	var id uint32
	if len(c.FG) != 7 || c.FG[0] != '#' {
		return 0, false
	}
	if _, err := fmt.Sscanf(c.FG[1:], "%06x", &id); err != nil {
		return 0, false
	}
	return id, true
}

// imageMarkers is what stands in for an image's cells in a terminal that can't
// show it: blanks, and "[image W×H]" (pixels) in the image's first row where
// that fits the run of cells.
type imageMarkers map[point]string

func (m imageMarkers) at(x, y int) string {
	if text, ok := m[point{x, y}]; ok {
		return text
	}
	return " "
}

// placeholderMarkers lays out the markers for a frame, nil if it has no images.
func placeholderMarkers(f *protocol.TerminalFrame) imageMarkers {
	if f == nil || len(f.Images) == 0 {
		return nil
	}
	sizes := make(map[uint32]protocol.Image, len(f.Images))
	for _, img := range f.Images {
		sizes[uint32(img.ID)] = img
	}
	markers := make(imageMarkers)
	labelled := make(map[uint32]bool)
	for y, row := range f.Rows {
		for x := 0; x < len(row); {
			id, ok := placeholderID(row[x])
			if !ok || !isPlaceholder(row[x]) {
				x++
				continue
			}
			end := x + 1
			for end < len(row) && isPlaceholder(row[end]) {
				if other, ok := placeholderID(row[end]); !ok || other != id {
					break
				}
				end++
			}
			if img, known := sizes[id]; known && !labelled[id] {
				labelled[id] = true
				label := []rune(fmt.Sprintf("[image %d×%d]", img.Width, img.Height))
				if len(label) > end-x {
					label = []rune("[image]")
				}
				if len(label) <= end-x {
					for i, r := range label {
						markers[point{x + i, y}] = string(r)
					}
				}
			}
			x = end
		}
	}
	return markers
}

// exactColorOff reports that tcell won't send exact colors: with NO_COLOR it
// sends none at all, and TCELL_TRUECOLOR=disable approximates them with the
// palette. Either would corrupt the image id placeholder cells carry in their
// foreground color, so images stay off.
func exactColorOff() bool {
	return os.Getenv("NO_COLOR") != "" || os.Getenv("TCELL_TRUECOLOR") == "disable"
}
