package protocol

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Call performs a bounded one-shot local RPC. It has no dependency on terminal
// UI, images, clipboard helpers, or session attachment state.
func Call(socket string, r Request) (Response, error) {
	c, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return Response{}, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	r.Version = Version
	if err = json.NewEncoder(c).Encode(r); err != nil {
		return Response{}, err
	}
	var response Response
	if err = json.NewDecoder(c).Decode(&response); err != nil {
		return response, err
	}
	if response.Version != Version {
		return response, fmt.Errorf("bmux protocol version mismatch")
	}
	if response.Error != "" {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}
