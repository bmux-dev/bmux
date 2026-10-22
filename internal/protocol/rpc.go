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
		return response, MismatchError{Server: response.Version}
	}
	if response.Error != "" {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}

// MismatchError is a reply from a server speaking another protocol version:
// this bmux and the running server are different releases, as after bmux
// update and before bmux restart. Server is the server's version, 0 if it
// sent none.
type MismatchError struct{ Server int }

func (e MismatchError) Error() string {
	if e.Server < Version {
		return fmt.Sprintf("the running bmux server is an older version (protocol %d; this bmux speaks %d). Run bmux restart to switch it to this version: sessions come back, programs running in panes end", e.Server, Version)
	}
	return fmt.Sprintf("the running bmux server is a newer version (protocol %d; this bmux speaks %d). Use the bmux that started it, or run bmux restart to switch the server to this one", e.Server, Version)
}
