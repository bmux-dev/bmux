package main

import (
	"testing"
)

func TestListScopesFollowTheTarget(t *testing.T) {
	t.Setenv("BMUX_SESSION", "work")
	t.Setenv("BMUX_SESSION_ID", "7")
	t.Setenv("BMUX_PANE", "4")
	scope := func(args ...string) (string, string, int, error) {
		a, err := parseLayoutArgs(args, "usage", []string{"-F"}, []string{"-a", "--json"})
		if err != nil {
			return "", "", 0, err
		}
		r, err := listScope(a, "usage", "window")
		return r.Kind, r.Name, r.Window, err
	}
	for _, c := range []struct {
		args          []string
		kind, session string
		window        int
	}{
		{nil, "window", "work", 0},             // the calling pane's window
		{[]string{"api"}, "session", "api", 0}, // a session: all its panes
		{[]string{"api:2."}, "window", "api", 2},
		{[]string{"api:3"}, "window", "api", 0}, // a pane: its window
		{[]string{"-a"}, "all", "", 0},
	} {
		kind, session, window, err := scope(c.args...)
		if err != nil || kind != c.kind || session != c.session || window != c.window {
			t.Errorf("%v: scope %q session %q window %d %v; want %q %q %d", c.args, kind, session, window, err, c.kind, c.session, c.window)
		}
	}
	if _, _, _, err := scope("-a", "api"); err == nil {
		t.Error("-a with a target was accepted")
	}
	t.Setenv("BMUX_SESSION", "")
	if _, _, _, err := scope(); err == nil {
		t.Error("no target outside bmux was accepted")
	}
}
