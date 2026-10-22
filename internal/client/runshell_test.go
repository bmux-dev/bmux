package client

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestShellCommandKeysAskTheServerToRunThem(t *testing.T) {
	u, requests := navigationUI(t)
	u.applySettings(rebound(t, `[keys.prefix]
"g" = "run-shell bmux split -h -- lazygit"
[keys.root]
"F5" = "run-shell make test"
`))
	u.key(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	u.key(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	if r := <-requests; r.Op != "run-shell" || r.PaneID != u.focused || r.Text != "bmux split -h -- lazygit" {
		t.Fatalf("Ctrl-a g: %+v", r)
	}
	u.key(tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone))
	if r := <-requests; r.Op != "run-shell" || r.Text != "make test" {
		t.Fatalf("F5: %+v", r)
	}
	help := strings.Join(u.helpLines(), "\n")
	if !strings.Contains(help, "F5") || !strings.Contains(help, "run: make test") || !strings.Contains(help, "Ctrl-a g") || !strings.Contains(help, "run: bmux split -h -- lazygit") {
		t.Fatalf("the key list doesn't show the command keys:\n%s", help)
	}
}
