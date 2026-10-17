package client

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func Request(socket string, r protocol.Request) (protocol.Response, error) {
	return protocol.Call(socket, r)
}

type connection struct {
	net.Conn
	enc *json.Encoder
	dec *json.Decoder
}

func attach(socket, name string, w, h int) (*connection, error) {
	c, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return nil, err
	}
	conn := &connection{Conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}
	if err := conn.send(protocol.Request{Version: protocol.Version, Op: "attach", Name: name, Width: w, Height: h}); err != nil {
		c.Close()
		return nil, err
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var response protocol.Response
	err = conn.dec.Decode(&response)
	_ = c.SetReadDeadline(time.Time{})
	if err == nil && response.Error != "" {
		err = fmt.Errorf("%s", response.Error)
	}
	if err == nil && response.Version != protocol.Version {
		err = fmt.Errorf("bmux protocol version mismatch")
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return conn, nil
}

func (c *connection) send(r protocol.Request) error {
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	return protocol.EncodeRequest(c.enc, r)
}
