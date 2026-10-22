package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestLayoutCommandArguments(t *testing.T) {
	dir := t.TempDir()
	r, err := splitRequest([]string{"api:2.1", "-h", "-c", dir, "-d", "--", "npm", "run", "dev"})
	if err != nil {
		t.Fatal(err)
	}
	want := protocol.Request{Op: "layout-command", Kind: "split", Name: "api", Window: 2, PaneIndex: 1,
		Direction: "horizontal", Dir: dir, Detach: true, Args: []string{"npm", "run", "dev"}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("split:\n got %+v\nwant %+v", r, want)
	}
	if r, err := splitRequest([]string{"api:3"}); err != nil || r.Direction != "vertical" {
		t.Fatalf("split defaults to stacked like tmux: %+v %v", r, err)
	}

	// No target: the calling pane, by session ID so a rename doesn't matter.
	t.Setenv("BMUX_SESSION", "work")
	t.Setenv("BMUX_SESSION_ID", "7")
	t.Setenv("BMUX_PANE", "4")
	r, err = newWindowRequest([]string{"-n", "logs", "--", "tail -f log"})
	if err != nil || r.Name != "work" || r.SessionID != 7 || r.PaneID != 4 || r.NewName != "logs" || !reflect.DeepEqual(r.Args, []string{"tail -f log"}) {
		t.Fatalf("new-window in a pane: %+v %v", r, err)
	}
	r, err = resizeRequest([]string{"right", "12"})
	if err != nil || r.PaneID != 4 || r.Direction != "right" || r.Delta != 12 {
		t.Fatalf("resize in a pane: %+v %v", r, err)
	}
	r, err = resizeRequest([]string{":2.1", "up"})
	if err != nil || r.Name != "work" || r.Window != 2 || r.PaneIndex != 1 || r.Direction != "up" || r.Delta != 0 {
		t.Fatalf("resize a target: %+v %v", r, err)
	}
	r, err = renameWindowRequest([]string{"api:2.", "build"})
	if err != nil || r.Name != "api" || r.Window != 2 || r.NewName != "build" {
		t.Fatalf("rename-window: %+v %v", r, err)
	}
	if r, err := targetOnlyRequest("zoom", nil, false); err != nil || r.PaneID != 4 {
		t.Fatalf("zoom in a pane: %+v %v", r, err)
	}
	if _, err := targetOnlyRequest("kill-pane", nil, true); err == nil || !strings.Contains(err.Error(), "needs a TARGET") {
		t.Fatalf("kill-pane without a target closed the calling pane: %v", err)
	}

	for name, build := range map[string]func() (protocol.Request, error){
		"-h and -v":          func() (protocol.Request, error) { return splitRequest([]string{"-h", "-v"}) },
		"two targets":        func() (protocol.Request, error) { return splitRequest([]string{"a:1", "b:1"}) },
		"unknown flag":       func() (protocol.Request, error) { return splitRequest([]string{"-x"}) },
		"-c without a value": func() (protocol.Request, error) { return splitRequest([]string{"-c"}) },
		"-c not a directory": func() (protocol.Request, error) { return splitRequest([]string{"-c", dir + "/missing"}) },
		"-- with nothing":    func() (protocol.Request, error) { return splitRequest([]string{"--"}) },
		"resize direction":   func() (protocol.Request, error) { return resizeRequest([]string{"sideways"}) },
		"resize cells":       func() (protocol.Request, error) { return resizeRequest([]string{"left", "0"}) },
		"rename no name":     func() (protocol.Request, error) { return renameWindowRequest(nil) },
		"select no target":   func() (protocol.Request, error) { return targetOnlyRequest("select", nil, true) },
	} {
		if _, err := build(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	t.Setenv("BMUX_SESSION", "")
	if _, err := splitRequest(nil); err == nil || !strings.Contains(err.Error(), "not inside a bmux pane") {
		t.Fatalf("split outside bmux without a target: %v", err)
	}
}
