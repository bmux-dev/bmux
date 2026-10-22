package server

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/format"
	"github.com/josephferrero/bmux/internal/protocol"
)

// quietPane is a pane whose program prints nothing, with its hook events
// recorded instead of run.
func quietPane(t *testing.T) (*pane, *[]string) {
	t.Helper()
	p, _ := scriptedPane(t, "sleep 30")
	var events []string
	p.mu.Lock()
	p.hook = func(event string) { events = append(events, event) }
	p.mu.Unlock()
	return p, &events
}

func mark(p *pane) (bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, _ := p.activityLocked(1)
	return a.Attention, a.Alert
}

func TestActivityAlertsMarkUnseenOutputOnce(t *testing.T) {
	p, events := quietPane(t)

	// Not monitored: output is no alert.
	p.apply([]byte("hello"))
	if on, _ := mark(p); on {
		t.Fatal("an unmonitored pane was marked")
	}

	p.mu.Lock()
	p.monitorActivity = true
	p.mu.Unlock()
	// Echo of typing is no activity.
	p.mu.Lock()
	p.lastInput = time.Now()
	p.mu.Unlock()
	p.apply([]byte("echo"))
	if on, _ := mark(p); on {
		t.Fatal("echo of input was taken for activity")
	}
	p.mu.Lock()
	p.lastInput = time.Time{}
	p.mu.Unlock()

	// Output the program printed by itself marks it, once.
	p.apply([]byte("build finished"))
	p.apply([]byte("more"))
	if on, alert := mark(p); !on || alert != "activity" {
		t.Fatalf("after output: attention=%v alert=%q", on, alert)
	}
	if !reflect.DeepEqual(*events, []string{"alert-activity"}) {
		t.Fatalf("hooks %v, want one alert-activity", *events)
	}

	// A pane in view is never marked.
	p.mu.Lock()
	p.attention = false
	p.mu.Unlock()
	p.viewed.Store(true)
	p.apply([]byte("seen"))
	if on, _ := mark(p); on {
		t.Fatal("a pane in view was marked")
	}
	p.viewed.Store(false)

	// A bell is labelled as a bell, not as activity.
	p.mu.Lock()
	p.monitorActivity = false
	p.mu.Unlock()
	p.apply([]byte("\a"))
	if on, alert := mark(p); !on || alert != "" {
		t.Fatalf("bell: attention=%v alert=%q", on, alert)
	}
}

func TestSilenceAlertsMarkOncePerQuietStretch(t *testing.T) {
	p, events := quietPane(t)
	start := time.Now()
	p.mu.Lock()
	p.monitorSilence, p.lastOutput = 2*time.Second, start
	p.checkSilenceLocked(start.Add(1900 * time.Millisecond))
	p.mu.Unlock()
	if on, _ := mark(p); on {
		t.Fatal("marked before the quiet time passed")
	}
	p.mu.Lock()
	p.checkSilenceLocked(start.Add(2 * time.Second))
	p.mu.Unlock()
	if on, alert := mark(p); !on || alert != "silence" {
		t.Fatalf("after 2s quiet: attention=%v alert=%q", on, alert)
	}

	// Looked at and left: the same quiet stretch isn't marked again.
	p.mu.Lock()
	p.attention = false
	p.checkSilenceLocked(start.Add(time.Minute))
	p.mu.Unlock()
	if on, _ := mark(p); on {
		t.Fatal("one quiet stretch was marked twice")
	}

	// Output starts a new stretch, which is marked when it's long enough.
	p.apply([]byte("next step"))
	p.mu.Lock()
	again := p.lastOutput
	p.checkSilenceLocked(again.Add(time.Second))
	p.mu.Unlock()
	if on, _ := mark(p); on {
		t.Fatal("output didn't restart the quiet time")
	}
	p.mu.Lock()
	p.checkSilenceLocked(again.Add(2 * time.Second))
	p.mu.Unlock()
	if on, _ := mark(p); !on {
		t.Fatal("the next quiet stretch wasn't marked")
	}
	if !reflect.DeepEqual(*events, []string{"alert-silence", "alert-silence"}) {
		t.Fatalf("hooks %v, want two alert-silence", *events)
	}

	// A pane in view when the time passes isn't marked, then or later.
	p.apply([]byte("x"))
	p.mu.Lock()
	p.attention = false
	quiet := p.lastOutput
	p.mu.Unlock()
	p.viewed.Store(true)
	p.mu.Lock()
	p.checkSilenceLocked(quiet.Add(3 * time.Second))
	p.mu.Unlock()
	p.viewed.Store(false)
	p.mu.Lock()
	p.checkSilenceLocked(quiet.Add(5 * time.Second))
	p.mu.Unlock()
	if on, _ := mark(p); on {
		t.Fatal("a quiet stretch that passed in view was marked afterwards")
	}
}

func TestMonitorSettingsCommandsAndFormatVariables(t *testing.T) {
	_, s := viewportSession(t)
	useConfig(t, New("/bin/sh"), "[alerts]\nsilence-after = \"45s\"\n")
	describe := func() map[string]string {
		t.Helper()
		panes, err := s.describe(paneTarget{pane: 1}, "pane")
		if err != nil || len(panes) != 1 {
			t.Fatalf("describe: %v %v", panes, err)
		}
		vars := format.Vars(panes[0])
		return map[string]string{"activity": vars["pane_monitor_activity"], "silence": vars["pane_monitor_silence"]}
	}
	if got := describe(); got["activity"] != "0" || got["silence"] != "0" {
		t.Fatalf("defaults: %v", got)
	}
	for _, tc := range []struct {
		kind, value string
		activity    string
		silence     string
	}{
		{"activity", "on", "1", "0"},
		{"silence", "on", "1", "45"}, // alerts.silence-after
		{"silence", "90", "1", "90"}, // whole seconds
		{"silence", "2m", "1", "120"},
		{"activity", "toggle", "0", "120"},
		{"silence", "toggle", "0", "0"},
		{"silence", "toggle", "0", "45"},
		{"silence", "off", "0", "0"},
	} {
		if _, err := s.monitor(paneTarget{pane: 1}, tc.kind, tc.value); err != nil {
			t.Fatalf("%s %s: %v", tc.kind, tc.value, err)
		}
		if got := describe(); got["activity"] != tc.activity || got["silence"] != tc.silence {
			t.Fatalf("after %s %s: %v, want activity %s silence %s", tc.kind, tc.value, got, tc.activity, tc.silence)
		}
	}
	for _, bad := range []struct{ kind, value, want string }{
		{"silence", "500ms", "between 1s and 1h"},
		{"silence", "2h", "between 1s and 1h"},
		{"silence", "soon", "duration"},
		{"activity", "maybe", "on, off or toggle"},
		{"bell", "on", "activity or silence"},
	} {
		if _, err := s.monitor(paneTarget{pane: 1}, bad.kind, bad.value); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%s %s: %v, want an error mentioning %q", bad.kind, bad.value, err, bad.want)
		}
	}
	if _, err := s.monitor(paneTarget{pane: 9}, "activity", "on"); err == nil {
		t.Fatal("a missing pane was accepted")
	}

	// The key toggles say the result in the status line.
	if err := s.input(protocol.Request{Op: "monitor", Kind: "activity"}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	message := s.message
	s.mu.Unlock()
	if !strings.Contains(message, "marked when it prints") {
		t.Fatalf("status line %q", message)
	}
}
