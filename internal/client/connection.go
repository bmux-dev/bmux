package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func Request(socket string, r protocol.Request) (protocol.Response, error) {
	return protocol.Call(socket, r)
}

type connection struct {
	net.Conn
	enc           *json.Encoder
	dec           *json.Decoder
	frameEncoding string
}

func attach(socket, name string, w, h int, graphics *protocol.Graphics, colors *protocol.TerminalColors) (*connection, error) {
	c, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return nil, err
	}
	conn := &connection{Conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}
	requestedEncoding := ""
	if os.Getenv("BMUX_EXPERIMENT_FRAME_ENCODING") == protocol.CellPaletteRunsV1 {
		requestedEncoding = protocol.CellPaletteRunsV1
	}
	if err := conn.send(protocol.Request{Version: protocol.Version, Op: "attach", Name: name, Width: w, Height: h, Graphics: graphics, Colors: colors, FrameEncoding: requestedEncoding}); err != nil {
		c.Close()
		return nil, err
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var response protocol.Response
	err = conn.dec.Decode(&response)
	_ = c.SetReadDeadline(time.Time{})
	// The version first: an older server's own mismatch error doesn't say
	// what to do.
	if err == nil && response.Version != protocol.Version {
		err = protocol.MismatchError{Server: response.Version}
	}
	if err == nil && response.Error != "" {
		err = fmt.Errorf("%s", response.Error)
	}
	if err == nil && response.FrameEncoding != "" && (response.FrameEncoding != requestedEncoding || response.FrameEncoding != protocol.CellPaletteRunsV1) {
		err = fmt.Errorf("unexpected frame encoding %q", response.FrameEncoding)
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	conn.frameEncoding = response.FrameEncoding
	return conn, nil
}

func (c *connection) readResponse(response *protocol.Response) error {
	return protocol.DecodeResponse(c.dec, c.frameEncoding, response)
}

func (c *connection) send(r protocol.Request) error {
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	return protocol.EncodeRequest(c.enc, r)
}
