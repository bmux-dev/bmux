package client

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestMonitorKeysToggleTheFocusedPane(t *testing.T) {
	u, requests := navigationUI(t)
	u.applySettings(rebound(t, `[keys.prefix]
"M" = "monitor-activity"
"S" = "monitor-silence"
`))
	for key, kind := range map[rune]string{'M': "activity", 'S': "silence"} {
		u.key(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
		u.key(tcell.NewEventKey(tcell.KeyRune, key, tcell.ModNone))
		if r := <-requests; r.Op != "monitor" || r.Kind != kind || r.PaneID != u.focused {
			t.Fatalf("Ctrl-a %c: %+v", key, r)
		}
	}
}

func TestSidebarLabelsAlertsByKind(t *testing.T) {
	u, _ := sidebarUI(t)
	// Panes without an agent whose attention came from a bell, an activity
	// alert and a silence alert.
	u.sessions = []protocol.Session{{Name: "work", Panes: 3, Activity: []protocol.PaneActivity{
		{Pane: 1, Attention: true},
		{Pane: 2, Attention: true, Alert: "activity"},
		{Pane: 3, Attention: true, Alert: "silence"},
	}}}
	u.draw()
	text := pickerScreenText(u)
	var labels []string
	for _, line := range strings.Split(text, "\n") {
		cut := string([]rune(line)[:min(len([]rune(line)), u.sidebarCols()-1)])
		if f := strings.Fields(cut); len(f) == 3 && f[1] == "terminal" {
			labels = append(labels, f[2])
		}
	}
	if strings.Join(labels, " ") != "bell activity silent" {
		t.Fatalf("sidebar labels %v, want bell activity silent:\n%s", labels, text)
	}
}
