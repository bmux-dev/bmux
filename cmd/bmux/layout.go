package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/daemon"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/server"
)

// runLayout saves a session's windows as its folder's layout file, or trusts
// a folder's layout file so its commands run.
func runLayout(socket string, args []string) error {
	usage := fmt.Errorf("usage: bmux layout save [SESSION] | bmux layout trust [DIR]")
	if len(args) == 0 || len(args) > 2 {
		return usage
	}
	trustPath, err := daemon.LayoutTrustPath()
	if err != nil {
		return err
	}
	switch args[0] {
	case "save":
		r := protocol.Request{Op: "layout"}
		if len(args) == 2 {
			r.Name = args[1]
		} else if os.Getenv("BMUX_SESSION") != "" {
			// Inside a pane: this pane's session, even after a rename.
			r.Name = os.Getenv("BMUX_SESSION")
			r.SessionID, _ = strconv.ParseUint(os.Getenv("BMUX_SESSION_ID"), 10, 64)
		} else {
			return fmt.Errorf("name the session to save: bmux layout save SESSION")
		}
		response, err := client.Request(socket, r)
		if err != nil {
			return err
		}
		file := filepath.Join(response.Dir, server.LayoutFileName)
		if err := writeFileAtomic(file, []byte(response.Text)); err != nil {
			return err
		}
		// You wrote it, so its commands may run.
		if _, err := server.TrustLayout(trustPath, response.Dir); err != nil {
			return err
		}
		fmt.Println("Saved", file)
	case "trust":
		dir := "."
		if len(args) == 2 {
			dir = args[1]
		}
		file, err := server.TrustLayout(trustPath, dir)
		if err != nil {
			return err
		}
		fmt.Println("Trusted", file, "as it is now; editing it untrusts it")
	default:
		return usage
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bmux-layout-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0644)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
