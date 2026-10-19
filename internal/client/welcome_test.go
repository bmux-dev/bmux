package client

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/integration"
)

func TestWelcomeScreenShowsOnceAndLeadsToIntegrations(t *testing.T) {
	u, requests := sidebarUI(t)
	u.screen.SetSize(110, 24)
	u.configPath = filepath.Join(t.TempDir(), "config.toml")
	u.integrations.env = &integration.Env{Home: t.TempDir(), Getenv: func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	u.applySettings(config.Default())
	frame := u.workspace
	u.applyFrame(frame) // the first frame of the first attachment
	u.draw()
	text := screenText(u)
	for _, want := range []string{"Welcome to bmux", "Ctrl-a", "Ctrl-a ? lists them", "Ctrl-h/j/k/l", "Next: install optional agent integrations", "Enter, → or l continues"} {
		if !strings.Contains(text, want) {
			t.Fatalf("welcome screen doesn't show %q:\n%s", want, text)
		}
	}
	u.key(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	noRequest(t, requests)
	u.key(tcell.NewEventKey(tcell.KeyRune, 'l', tcell.ModNone))
	if u.welcome || !u.integrations.open {
		t.Fatal("continuing didn't open the integrations panel")
	}
	if cfg, err := config.Load(u.configPath, func(string) string { return "" }); err != nil || cfg.Onboarding {
		t.Fatalf("the welcome screen wasn't remembered: onboarding=%v %v", cfg.Onboarding, err)
	}
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	u.applyFrame(frame)
	if u.welcome {
		t.Fatal("welcome screen shown twice")
	}

	// A new client reading onboarding = false never shows it.
	next, _ := sidebarUI(t)
	settings, _ := config.Load(u.configPath, func(string) string { return "" })
	next.applySettings(settings)
	next.applyFrame(frame)
	if next.welcome {
		t.Fatal("welcome screen shown after it was seen")
	}
}

func TestEscSkipsTheWelcomeScreen(t *testing.T) {
	u, _ := sidebarUI(t)
	u.configPath = filepath.Join(t.TempDir(), "config.toml")
	u.applySettings(config.Default())
	u.applyFrame(u.workspace)
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.welcome || u.integrations.open {
		t.Fatal("Esc didn't skip straight back to the session")
	}
	if cfg, _ := config.Load(u.configPath, func(string) string { return "" }); cfg.Onboarding {
		t.Fatal("skipping wasn't remembered")
	}
}
