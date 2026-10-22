package main

import (
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestSendArgumentsKeepOrderAndNeedAPane(t *testing.T) {
	r, err := sendRequest([]string{"--text", "use Postgres", "api:3", "--key=enter", "--if-seq", "57", "--key", "C-c"})
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.Input{{Paste: "use Postgres"}, {Code: uv.KeyEnter}, {Code: 'c', Mod: int(uv.ModCtrl)}}
	if r.Name != "api" || r.PaneID != 3 || r.IfSeq != 57 || !reflect.DeepEqual(r.Input, want) {
		t.Fatalf("parsed %+v", r)
	}
	for _, bad := range [][]string{
		{"api", "--key", "enter"},       // no pane: would reach whatever is focused
		{"api:2.", "--key", "enter"},    // a window, not a pane
		{"api:3"},                       // nothing to send
		{"api:x", "--key", "enter"},     // not a pane number
		{"api:3", "--key", "nosuchkey"}, // unknown key
		{"api:3", "web:1", "--key", "enter"},
		{"api:3", "--if-seq", "0", "--key", "enter"},
		{"api:3", "--text"},
	} {
		if _, err := sendRequest(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if r, err := sendRequest([]string{"api:2.1", "--key", "enter"}); err != nil || r.Window != 2 || r.PaneIndex != 1 || r.PaneID != 0 {
		t.Fatalf("window.index target: %+v %v", r, err)
	}
}

func TestKeyNamesMatchAttachedKeyPresses(t *testing.T) {
	for name, want := range map[string]protocol.Input{
		"enter": {Code: uv.KeyEnter}, "Down": {Code: uv.KeyDown}, "esc": {Code: uv.KeyEscape},
		"C-c": {Code: 'c', Mod: int(uv.ModCtrl)}, "A-x": {Code: 'x', Mod: int(uv.ModAlt)},
		"F5": {Code: uv.KeyF5}, "y": {Code: 'y'}, "space": {Code: ' '}, "S-tab": {Code: uv.KeyTab, Mod: int(uv.ModShift)},
	} {
		got, err := parseKey(name)
		if err != nil || got != want {
			t.Errorf("%s: %+v %v, want %+v", name, got, err, want)
		}
	}
}

func TestWaitAgentWalksChangesInOrderAndSkipsItself(t *testing.T) {
	panes := []agentPane{
		{Target: "ops:1", Session: "ops", Pane: 1, State: protocol.AgentIdle, Seq: 90, Self: true},
		{Target: "api:1", Session: "api", Pane: 1, State: protocol.AgentBlocked, Seq: 57},
		{Target: "web:2", Session: "web", Pane: 2, State: protocol.AgentIdle, Seq: 61},
		{Target: "web:3", Session: "web", Pane: 3, State: protocol.AgentWorking, Seq: 70},
	}
	w := waitFor{states: []string{protocol.AgentBlocked, protocol.AgentIdle}}
	var order []string
	for {
		p, ok := w.match(panes)
		if !ok {
			break
		}
		order = append(order, p.Target)
		w.since = p.Seq
	}
	if strings.Join(order, " ") != "api:1 web:2" {
		t.Fatalf("walked %v", order)
	}
	w = waitFor{states: []string{protocol.AgentBlocked, protocol.AgentIdle}, targets: []target{{Session: "web"}}}
	if p, _ := w.match(panes); p.Target != "web:2" {
		t.Fatalf("session target matched %+v", p)
	}
	w = waitFor{states: []string{protocol.AgentIdle}, targets: []target{{Session: "api", Pane: 1}}}
	if p, ok := w.match(panes); ok {
		t.Fatalf("api:1 is blocked, not idle, yet matched %+v", p)
	}
}

func TestTargetsNameSessionsPanesAndWindows(t *testing.T) {
	t.Setenv("BMUX_SESSION", "here")
	for text, want := range map[string]target{
		"api":     {Session: "api"},
		"api:":    {Session: "api"},
		"api:7":   {Session: "api", Pane: 7}, // pane numbers as bmux agents prints them
		"api:2.":  {Session: "api", Window: 2},
		"api:2.1": {Session: "api", Window: 2, Index: 1},
		":3":      {Session: "here", Pane: 3},
		":2.1":    {Session: "here", Window: 2, Index: 1},
		":":       {Session: "here"},
	} {
		if got, err := parseTarget(text); err != nil || got != want {
			t.Errorf("%q: %+v %v, want %+v", text, got, err, want)
		}
	}
	for _, bad := range []string{"api:0", "api:x", "api:-1", "api:+2", "api:.1", "api:2.0", "api:2.x", "api:2.1.3", "bad name:1"} {
		if _, err := parseTarget(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	t.Setenv("BMUX_SESSION", "")
	if _, err := parseTarget(":2.1"); err == nil || !strings.Contains(err.Error(), "isn't a bmux pane") {
		t.Fatalf("a relative target outside bmux: %v", err)
	}

	// wait-agent narrows to a window, or one position in it.
	panes := []agentPane{
		{Target: "api:1", Session: "api", Pane: 1, Window: 1, Index: 1},
		{Target: "api:4", Session: "api", Pane: 4, Window: 2, Index: 1},
		{Target: "api:5", Session: "api", Pane: 5, Window: 2, Index: 2},
		{Target: "web:5", Session: "web", Pane: 5, Window: 2, Index: 2},
	}
	var got []string
	for _, p := range panes {
		if (target{Session: "api", Window: 2}).matches(p) {
			got = append(got, p.Target)
		}
	}
	if strings.Join(got, " ") != "api:4 api:5" {
		t.Fatalf("window 2 matched %v", got)
	}
	if !(target{Session: "api", Window: 2, Index: 2}).matches(panes[2]) || (target{Session: "api", Window: 2, Index: 2}).matches(panes[1]) {
		t.Fatal("api:2.2 matched the wrong pane")
	}
}

func TestNotifyTypesOneChangeAtATimeOnlyWhileTheSupervisorIsIdle(t *testing.T) {
	n := &notifier{w: waitFor{states: []string{protocol.AgentBlocked, protocol.AgentIdle}}, target: "me:1", pending: map[string]agentPane{}}
	me := agentPane{Target: "me:1", State: protocol.AgentWorking, Seq: 10}
	list := func(others ...agentPane) []agentPane { return append([]agentPane{me}, others...) }

	// At start, only waits that still need the user count; old idle turns don't.
	n.observe(list(agentPane{Target: "api:1", State: protocol.AgentBlocked, Seq: 5}, agentPane{Target: "web:1", State: protocol.AgentIdle, Seq: 6}))
	if _, _, ok := n.next(list()); ok {
		t.Fatal("typed into a busy supervisor")
	}
	// Later changes queue while the supervisor works; a pane's newer change
	// replaces its older one, and one that moved on to work drops out.
	n.observe(list(agentPane{Target: "api:1", State: protocol.AgentBlocked, Seq: 5}, agentPane{Target: "web:1", State: protocol.AgentBlocked, Seq: 12}, agentPane{Target: "ops:1", State: protocol.AgentIdle, Seq: 13}))
	n.observe(list(agentPane{Target: "api:1", State: protocol.AgentBlocked, Seq: 5}, agentPane{Target: "web:1", State: protocol.AgentBlocked, Seq: 12}, agentPane{Target: "ops:1", State: protocol.AgentWorking, Seq: 14}))

	me = agentPane{Target: "me:1", State: protocol.AgentIdle, Seq: 15}
	var told []string
	for {
		panes := list(agentPane{Target: "api:1", State: protocol.AgentBlocked, Seq: 5}, agentPane{Target: "web:1", State: protocol.AgentBlocked, Seq: 12}, agentPane{Target: "ops:1", State: protocol.AgentWorking, Seq: 14})
		n.observe(panes)
		p, guard, ok := n.next(panes)
		if !ok {
			break
		}
		if guard != me.Seq {
			t.Fatalf("guarded with seq %d, supervisor is at %d", guard, me.Seq)
		}
		told = append(told, p.Target)
		delete(n.pending, p.Target)
		n.typedAt = guard
		// Nothing more until the supervisor has reacted and come back idle.
		if _, _, again := n.next(panes); again {
			t.Fatal("typed twice before the supervisor reacted")
		}
		me.Seq += 2
	}
	if strings.Join(told, " ") != "api:1 web:1" {
		t.Fatalf("told about %v", told)
	}
	// The supervisor's own changes never count.
	n.observe(list())
	if len(n.pending) != 0 {
		t.Fatalf("pending after the supervisor changed: %v", n.pending)
	}
}
