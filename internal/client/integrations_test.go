package client

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/integration"
)

func TestIntegrationsPanelInstallsAvailableAgents(t *testing.T) {
	u, requests := sidebarUI(t)
	u.screen.SetSize(110, 24)
	u.applySettings(config.Default())
	home := t.TempDir()
	u.integrations.env = &integration.Env{Home: home, Getenv: func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if name == "claude" || name == "pi" {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		}}
	u.key(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	u.key(tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone))
	u.draw()
	text := screenText(u)
	for _, want := range []string{"Agent integrations", "+ Claude Code  available", "– Codex        not found", "+ pi           available", "Enter installs 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("panel doesn't show %q:\n%s", want, text)
		}
	}
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	u.draw()
	text = screenText(u)
	for _, want := range []string{"✓ Claude Code  installed", "✓ pi           installed", "Installed Claude Code, pi.", "Nothing to install"} {
		if !strings.Contains(text, want) {
			t.Fatalf("after Enter, panel doesn't show %q:\n%s", want, text)
		}
	}
	for _, file := range []string{".claude/hooks/bmux-agent-state.sh", ".pi/agent/extensions/bmux-agent-state.ts"} {
		if _, err := os.Stat(filepath.Join(home, file)); err != nil {
			t.Fatalf("not installed: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("installed an agent that isn't on PATH")
	}
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	noRequest(t, requests)
	if u.integrations.open {
		t.Fatal("Esc didn't close the panel")
	}
}
