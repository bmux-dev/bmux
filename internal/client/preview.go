package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

const previewRefreshInterval = 100 * time.Millisecond

type previewResult struct {
	epoch       int
	name        string
	frame       *protocol.Frame
	err         error
	revision    uint64
	notModified bool
	ended       bool
}

// Cancel readers on picker transitions; preview streams never own PTY sizing.
func (u *ui) stopPreview() {
	if u.previewCancel != nil {
		u.previewCancel()
		u.previewCancel = nil
	}
	u.previewBusy = false
	u.previewEpoch++
}

func (u *ui) requestPreview() {
	if !u.picker || u.prompt != "" || u.quitting || u.screen == nil {
		u.stopPreview()
		return
	}
	s, selected := u.selectedSession()
	name := ""
	if selected {
		name = s.Name
	}
	generation := ""
	if selected {
		generation = protocol.SessionGeneration(s.Created)
	}
	w, h := u.screen.Size()
	_, area := pickerAreas(w, h)
	size := [2]int{area.w, area.h}
	if name != u.previewName || generation != u.previewGeneration {
		u.stopPreview()
		u.previewName = name
		u.previewGeneration = generation
		u.preview = nil
		u.previewError = ""
		u.previewRevision = 0
	} else if size != u.previewSize {
		// Reject frames laid out for the previous panel geometry.
		u.stopPreview()
		u.preview = nil
		u.previewError = ""
		u.previewRevision = 0
	}
	u.previewSize = size
	if u.deleteBusy && selected && s.Name == u.deletePending.Name && s.Created.Equal(u.deletePending.Created) {
		u.stopPreview()
		return
	}
	if u.previewUnsupported {
		u.previewError = "Update the server to enable previews"
		return
	}
	if !selected || u.previewBusy || u.previews == nil {
		return
	}
	if area.w == 0 || area.h == 0 {
		u.stopPreview()
		return
	}
	u.previewBusy = true
	// The server sends the first viewport immediately, without resizing apps.
	u.preview = nil
	epoch, socket, results, done := u.previewEpoch, u.socket, u.previews, u.done
	ctx, cancel := context.WithCancel(context.Background())
	u.previewCancel = cancel
	go func() {
		defer cancel()
		deliver := func(result previewResult) bool {
			select {
			case results <- result:
				return true
			case <-ctx.Done():
				return false
			case <-done:
				return false
			}
		}
		err := watchPreview(ctx, socket, protocol.Request{Op: "watch-viewport", Name: name, Width: area.w, Height: area.h, Generation: generation}, func(r protocol.Response) bool {
			return deliver(previewResult{epoch: epoch, name: name, frame: r.Frame, revision: r.Revision, notModified: r.NotModified})
		})
		if ctx.Err() == nil {
			deliver(previewResult{epoch: epoch, name: name, err: err, ended: true})
		}
	}()
}

func previewFitsPanel(frame *protocol.Frame, area pickerArea) bool {
	if frame == nil {
		return false
	}
	if frame.PreviewMode != "terminal-viewport" || len(frame.Panes) == 0 {
		return true
	}
	return frame.Width == max(2, min(area.w, protocol.MaxPreviewWidth)) && frame.Height == max(1, min(area.h, protocol.MaxPreviewHeight))
}

func watchPreview(ctx context.Context, socket string, request protocol.Request, receive func(protocol.Response) bool) error {
	dialer := net.Dialer{Timeout: time.Second}
	c, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	request.Version = protocol.Version
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	if err := protocol.EncodeRequest(json.NewEncoder(c), request); err != nil {
		return err
	}
	dec := json.NewDecoder(c)
	for {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		var r protocol.Response
		if err := dec.Decode(&r); err != nil {
			return err
		}
		if r.Version != protocol.Version {
			return fmt.Errorf("bmux protocol version mismatch")
		}
		if r.Error != "" {
			return fmt.Errorf("%s", r.Error)
		}
		if !receive(r) {
			return nil
		}
	}
}

func (u *ui) acceptPreview(result previewResult) {
	if !u.picker || result.epoch != u.previewEpoch || result.name != u.previewName {
		return
	}
	if result.ended {
		u.stopPreview()
	}
	if result.err != nil {
		if result.err.Error() == "session not found" {
			u.preview = nil
			u.previewError = "Session closed"
		} else if u.preview != nil {
			u.previewError = "Preview stale: " + result.err.Error()
		} else {
			u.previewError = "Preview unavailable: " + result.err.Error()
		}
		u.previewRevision = 0
		if result.err.Error() == "unknown operation" {
			u.preview = nil
			u.previewUnsupported = true
			u.previewError = "Update the server to enable previews"
		}
		u.notice(u.previewError)
		return
	}
	u.previewRevision = result.revision
	u.previewError = ""
	if result.notModified {
		return
	}
	if result.frame == nil || (result.frame.PreviewMode != "terminal-viewport" && result.frame.PreviewMode != "overview") {
		u.stopPreview()
		u.preview = nil
		u.previewUnsupported = true
		u.previewError = "Update the server for native terminal viewports"
		return
	}
	u.preview = result.frame
	u.previewError = ""
}
