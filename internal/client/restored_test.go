package client

import (
	"strings"
	"testing"
)

func TestRestoredSessionsAreMarkedInThePickerAndSidebar(t *testing.T) {
	u := pickerUI(t)
	u.sessions[1].Restored = true // web
	u.drawPicker(100, 24)
	text := pickerScreenText(u)
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.Contains(line, "+ web:"):
			if !strings.Contains(line, "1 panes (restored)") {
				t.Fatalf("restored picker row: %q", line)
			}
		case strings.Contains(line, "+ api:"), strings.Contains(line, "+ logs:"):
			if strings.Contains(line, "restored") {
				t.Fatalf("unrestored picker row marked: %q", line)
			}
		}
	}
	if !strings.Contains(text, "+ web:") {
		t.Fatalf("web missing from the picker:\n%s", text)
	}

	s, _ := sidebarUI(t)
	for i := range s.sessions {
		s.sessions[i].Restored = s.sessions[i].Name == "web"
	}
	s.draw()
	// Creation order: api, web, db, under the SESSIONS heading.
	for i, want := range []struct{ name, right string }{{"api", ""}, {"web", "restored"}, {"db", ""}} {
		y := paneOriginY + 1 + i
		line := string([]rune(pickerScreenLine(s, y))[:s.sidebarCols()-1])
		fields := strings.Fields(string([]rune(line)[3:])) // after the icon column
		if len(fields) == 0 || fields[0] != want.name || strings.Join(fields[1:], " ") != want.right {
			t.Fatalf("sidebar row %d = %q, want %s … %q", i, line, want.name, want.right)
		}
	}
	x := s.sidebarCols() - 1 - len("restored ")
	if _, style, _ := s.screen.Get(x, paneOriginY+2); style != sidebarMutedStyle {
		t.Fatalf("restored label at x=%d isn't muted", x)
	}
}
