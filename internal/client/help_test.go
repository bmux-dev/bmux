package client

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func screenText(u *ui) string {
	_, h := u.screen.Size()
	var text strings.Builder
	for y := range h {
		text.WriteString(pickerScreenLine(u, y))
		text.WriteByte('\n')
	}
	return text.String()
}

func TestKeyHelpListsBindingsInEffectAndKeepsKeysFromApps(t *testing.T) {
	u, requests := sidebarUI(t) // 100x20
	u.applySettings(rebound(t, `[keys.root]
"C-l" = "none"
[keys.prefix]
"v" = "split-right"
`))
	u.key(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	u.key(tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone))
	u.draw()
	text := screenText(u)
	for _, want := range []string{"Without the prefix", "Ctrl-h", "focus left, or Vim/Neovim's split", "After Ctrl-a", "Keys: j/k"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help doesn't show %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Ctrl-l") {
		t.Fatalf("help lists an unbound key:\n%s", text)
	}
	// More bindings than rows: scrolling reaches the end of the list.
	for range len(u.helpLines()) {
		u.key(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	}
	u.draw()
	if text := screenText(u); !strings.Contains(text, "Rebind them in") || strings.Contains(text, "Without the prefix") {
		t.Fatalf("help didn't scroll to its end:\n%s", text)
	}
	if lines := strings.Join(u.helpLines(), "\n"); !strings.Contains(lines, "Ctrl-a v / |") {
		t.Fatalf("help doesn't list both split keys:\n%s", lines)
	}
	// Any other key closes the list and is not typed into the pane.
	u.key(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	noRequest(t, requests)
	u.draw()
	if u.help || strings.Contains(screenText(u), "Without the prefix") {
		t.Fatal("help stayed open")
	}
}
