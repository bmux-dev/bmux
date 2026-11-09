package client

import (
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Frozen baseline attach request: deliberately no frame_encoding field. The
// legacy server fixture decodes a real JSON request into this old shape.
type legacyAttachRequest struct {
	Version  int                      `json:"version"`
	Op       string                   `json:"op"`
	Name     string                   `json:"name,omitempty"`
	Width    int                      `json:"width,omitempty"`
	Height   int                      `json:"height,omitempty"`
	Graphics *protocol.Graphics       `json:"graphics,omitempty"`
	Colors   *protocol.TerminalColors `json:"colors,omitempty"`
}

func fixtureSocket(t *testing.T, serve func(net.Conn) error) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		c, err := listener.Accept()
		if err == nil {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(3 * time.Second))
			err = serve(c)
		}
		done <- err
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(4 * time.Second):
			t.Error("fixture reader leaked")
		}
	})
	return path
}

func TestAttachCompactNegotiationMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, env, selected string
		legacy, reject      bool
	}{
		{name: "default-off"},
		{name: "unknown-env-is-off", env: "unknown"},
		{name: "old-server-fallback", env: protocol.CellPaletteRunsV1, legacy: true},
		{name: "new-server-enabled", env: protocol.CellPaletteRunsV1, selected: protocol.CellPaletteRunsV1},
		{name: "unrequested-selection", selected: protocol.CellPaletteRunsV1, reject: true},
		{name: "unknown-selection", env: protocol.CellPaletteRunsV1, selected: "unknown", reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BMUX_EXPERIMENT_FRAME_ENCODING", tc.env)
			expected := &protocol.Frame{Name: "fixture", Width: 40, Height: 2, Clipboard: "copy", Notice: "notice", Message: "message", MessageSeq: 7,
				Panes: []protocol.PaneFrame{{ID: 1, Terminal: &protocol.TerminalFrame{Name: "pane", Rows: [][]protocol.Cell{make([]protocol.Cell, 40), make([]protocol.Cell, 40)}}}}}
			path := fixtureSocket(t, func(c net.Conn) error {
				var request protocol.Request
				decoder := json.NewDecoder(c)
				if tc.legacy {
					var old legacyAttachRequest
					if err := decoder.Decode(&old); err != nil {
						return err
					}
					request.Version, request.Op, request.Name = old.Version, old.Op, old.Name
					if old.Width != 40 || old.Height != 2 {
						t.Error("old server lost geometry")
					}
				} else {
					if err := decoder.Decode(&request); err != nil {
						return err
					}
					want := ""
					if tc.env == protocol.CellPaletteRunsV1 {
						want = tc.env
					}
					if request.FrameEncoding != want {
						t.Errorf("requested encoding %q, want %q", request.FrameEncoding, want)
					}
				}
				if request.Version != 10 || request.Op != "attach" || request.Name != "fixture" {
					t.Error("attachment request changed")
				}
				encoder := json.NewEncoder(c)
				if err := encoder.Encode(protocol.Response{Version: 10, FrameEncoding: tc.selected}); err != nil {
					return err
				}
				if tc.reject {
					return nil
				}
				frame := expected
				if tc.selected != "" {
					frame = protocol.CompactFrame(frame)
				}
				if err := encoder.Encode(protocol.Response{Version: 10, Ack: 5, Frame: frame}); err != nil {
					return err
				}
				return encoder.Encode(protocol.Response{Version: 10, Error: "fixture end"})
			})
			conn, err := attach(path, "fixture", 40, 2, nil, nil)
			if tc.reject {
				if err == nil {
					conn.Close()
					t.Fatal("invalid selection accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if conn.frameEncoding != tc.selected {
				t.Fatal("selection not retained per connection")
			}
			var response protocol.Response
			if err := conn.readResponse(&response); err != nil {
				t.Fatal(err)
			}
			if response.Ack != 5 || !reflect.DeepEqual(response.Frame, expected) {
				t.Fatal("reader changed frame or transient metadata")
			}
			if err := conn.readResponse(&response); err != nil || response.Error != "fixture end" {
				t.Fatalf("end response: %#v, %v", response, err)
			}
		})
	}
}

func TestConnectionRejectsUnnegotiatedCompactFrame(t *testing.T) {
	t.Setenv("BMUX_EXPERIMENT_FRAME_ENCODING", "")
	path := fixtureSocket(t, func(c net.Conn) error {
		var request legacyAttachRequest
		if err := json.NewDecoder(c).Decode(&request); err != nil {
			return err
		}
		encoder := json.NewEncoder(c)
		if err := encoder.Encode(protocol.Response{Version: 10}); err != nil {
			return err
		}
		frame := &protocol.Frame{Panes: []protocol.PaneFrame{{Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{make([]protocol.Cell, 40)}}}}}
		return encoder.Encode(protocol.Response{Version: 10, Frame: protocol.CompactFrame(frame)})
	})
	conn, err := attach(path, "fixture", 40, 2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var response protocol.Response
	if err := conn.readResponse(&response); err == nil {
		t.Fatal("unnegotiated compact frame accepted")
	}
}
