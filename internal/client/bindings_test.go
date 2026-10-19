package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

func rebound(t *testing.T, toml string) config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(toml), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(path, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestKeyEventsMatchConfigKeyNames(t *testing.T) {
	for _, tc := range []struct {
		event *tcell.EventKey
		name  string
	}{
		{tcell.NewEventKey(tcell.KeyCtrlH, 0, tcell.ModCtrl), "C-h"},
		{tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModCtrl), "C-h"},
		{tcell.NewEventKey(tcell.KeyRune, 'H', tcell.ModCtrl|tcell.ModShift), "C-h"},
		{tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone), "backspace"},
		{tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), "tab"},
		{tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), "S-tab"},
		{tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), "ret"},
		{tcell.NewEventKey(tcell.KeyRune, '|', tcell.ModShift), "|"},
		{tcell.NewEventKey(tcell.KeyRune, '-', tcell.ModNone), "minus"},
		{tcell.NewEventKey(tcell.KeyRune, 'A', tcell.ModShift), "A"},
		{tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModAlt), "A-x"},
		{tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModShift), "S-pageup"},
		{tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone), "F5"},
		{tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), "space"},
	} {
		got := keyName(tc.event)
		if got != tc.name {
			t.Errorf("%v: %q, want %q", tc.event.Name(), got, tc.name)
		}
		// Whatever a user writes for this key in the config must match.
		if canonical, err := config.KeyName(tc.name); err != nil || canonical != got {
			t.Errorf("config.KeyName(%q) = %q %v", tc.name, canonical, err)
		}
	}
}

func TestReboundKeys(t *testing.T) {
	u, requests := navigationUI(t)
	u.applySettings(rebound(t, `[keys.root]
"C-l" = "none"
[keys.prefix]
"|" = "none"
"v" = "split-right"
"H" = "resize-left"
"h" = "none"
"a" = "send-prefix"
`))
	press := func(key tcell.Key, r rune, mods tcell.ModMask) { u.key(tcell.NewEventKey(key, r, mods)) }

	// Ctrl-l reaches the shell; Ctrl-h still navigates.
	press(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	if r := <-requests; r.Op != "key" || r.Code != 'l' || r.Mod != int(uv.ModCtrl) {
		t.Fatalf("unbound Ctrl-l: %+v", r)
	}
	press(tcell.KeyCtrlH, 0, tcell.ModCtrl)
	nextNavigationRequest(t, requests, "navigate-pane", "left")

	press(tcell.KeyCtrlA, 0, tcell.ModCtrl)
	press(tcell.KeyRune, 'v', tcell.ModNone)
	nextNavigationRequest(t, requests, "split", "horizontal")
	press(tcell.KeyCtrlA, 0, tcell.ModCtrl)
	press(tcell.KeyRune, '|', tcell.ModNone)
	if !strings.HasPrefix(u.message, "Ctrl-a: ? keys · v / - split") || strings.Contains(u.message, "|") {
		t.Fatalf("unbound key didn't show the current bindings: %q", u.message)
	}
	// A rebound resize key repeats without the prefix; the old one is free.
	now := time.Now()
	u.keyAt(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl), now)
	u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'H', tcell.ModShift), now)
	nextNavigationRequest(t, requests, "resize-pane", "left")
	u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'H', tcell.ModShift), now.Add(500*time.Millisecond))
	nextNavigationRequest(t, requests, "resize-pane", "left")
	u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone), now.Add(600*time.Millisecond))
	if r := <-requests; r.Op != "key" || r.Code != 'h' {
		t.Fatalf("plain h during the repeat window: %+v", r)
	}
	// send-prefix on another key still sends the prefix itself.
	press(tcell.KeyCtrlA, 0, tcell.ModCtrl)
	press(tcell.KeyRune, 'a', tcell.ModNone)
	if r := <-requests; r.Op != "key" || r.Code != 'a' || r.Mod != int(uv.ModCtrl) {
		t.Fatalf("send-prefix: %+v", r)
	}
}

func TestStatusLineHelpFollowsBindings(t *testing.T) {
	u, _ := sidebarUI(t)
	u.applySettings(config.Default())
	u.draw()
	if footer := pickerScreenLine(u, 19); !strings.Contains(footer, "Ctrl-a s sessions · Ctrl-h/j/k/l navigate · Ctrl-a d detach") {
		t.Fatalf("default footer: %q", footer)
	}
	if got := u.prefixHelp(); got != "Ctrl-a: ? keys · | / - split · x close · arrows focus · hjkl resize · m zoom · ( ) sessions · b sidebar · r reload · s picker" {
		t.Fatalf("default prefix help: %q", got)
	}
	u.applySettings(rebound(t, `[keys.root]
"C-h" = "none"
"A-h" = "navigate-left"
"A-j" = "navigate-down"
"A-k" = "navigate-up"
"A-l" = "navigate-right"
[keys.prefix]
"s" = "none"
"w" = "sessions"
"d" = "none"
`))
	u.draw()
	if footer := pickerScreenLine(u, 19); !strings.Contains(footer, "Ctrl-a w sessions · Alt-h/j/k/l navigate") || strings.Contains(footer, "detach") {
		t.Fatalf("rebound footer: %q", footer)
	}
}
