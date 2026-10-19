package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

func TestConfiguredPrefixReplacesCtrlA(t *testing.T) {
	u, requests := sidebarUI(t)
	settings := config.Default()
	settings.Prefix = "ctrl-b"
	u.applySettings(settings)

	u.key(tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModCtrl))
	if !u.prefix {
		t.Fatal("Ctrl-b did not start a command")
	}
	u.key(tcell.NewEventKey(tcell.KeyRune, '|', tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "split" {
		t.Fatalf("Ctrl-b | : %+v", r)
	}
	// Ctrl-a is an ordinary key now, and the prefix twice sends it through.
	u.key(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	if r := nextRequest(t, requests); r.Op != "key" || r.Code != 'a' || u.prefix {
		t.Fatalf("Ctrl-a with a Ctrl-b prefix: %+v", r)
	}
	u.key(tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModCtrl))
	u.key(tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModCtrl))
	if r := nextRequest(t, requests); r.Op != "key" || r.Code != 'b' {
		t.Fatalf("Ctrl-b Ctrl-b: %+v", r)
	}
	u.prefix = true
	u.draw()
	if footer := pickerScreenLine(u, 19); !strings.HasPrefix(strings.TrimSpace(strings.SplitN(footer, "|", 3)[2]), "Ctrl-b:") {
		t.Fatalf("prefix help still names Ctrl-a: %q", footer)
	}
	u.prefix = false
	u.draw()
	if footer := pickerScreenLine(u, 19); !strings.Contains(footer, "Ctrl-b s sessions") || strings.Contains(footer, "Ctrl-a") {
		t.Fatalf("footer: %q", footer)
	}
}

func TestReloadAppliesSidebarThemeAndSoundSettings(t *testing.T) {
	u, requests := sidebarUI(t)
	u.applySettings(config.Default()) // as Run does at startup
	t.Cleanup(func() { applyTheme(themeFor(config.Default().Theme)) })
	u.configPath = filepath.Join(t.TempDir(), "config.toml")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(u.configPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`[sidebar]
width = 30
[theme.agents]
waiting = "#00ff00"
[sound]
enabled = false
`)
	// The first frame of an attachment only records the server's revision.
	u.configKnown = false
	frame := *u.workspace
	frame.Config = 4
	u.applyFrame(&frame)
	if u.sidebarCols() != 24 {
		t.Fatal("reloaded on the first frame")
	}
	frame.Config = 5
	u.applyFrame(&frame)
	if u.sidebarCols() != 30 {
		t.Fatal("a newer server config revision didn't reload the client")
	}
	if r := nextRequest(t, requests); r.Op != "resize" || r.Width != 100-30 {
		t.Fatalf("width change didn't resize panes: %+v", r)
	}
	if chrome.done != tcell.GetColor("#00ff00") {
		t.Fatal("waiting color not applied")
	}
	if u.settings.Sound.Enabled {
		t.Fatal("sound setting not applied")
	}

	write("[sidebar]\nshow = false\n")
	u.reloadSettings()
	if !u.sidebarHidden || u.paneOriginX(100) != 0 {
		t.Fatal("show = false didn't hide the sidebar")
	}
	if r := nextRequest(t, requests); r.Op != "resize" || r.Width != 100 {
		t.Fatalf("hiding didn't resize panes: %+v", r)
	}

	write("prefix = \"ctrl-j\"\n[sidebar]\nshow = false\n")
	u.reloadSettings()
	if u.prefixName() != "Ctrl-a" || !strings.Contains(u.message, "prefix") {
		t.Fatalf("an invalid prefix wasn't reported or replaced: %q %q", u.prefixName(), u.message)
	}
	noRequest(t, requests)
}
