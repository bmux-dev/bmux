package client

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/josephferrero/bmux/internal/config"
)

func TestAlertSoundsUseTheFreedesktopThemeOnLinux(t *testing.T) {
	data := t.TempDir()
	theme := filepath.Join(data, "sounds", "freedesktop", "stereo")
	if err := os.MkdirAll(theme, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"complete.oga", "dialog-warning.oga"} {
		if err := os.WriteFile(filepath.Join(theme, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	custom := filepath.Join(t.TempDir(), "done.wav")
	if err := os.WriteFile(custom, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"XDG_DATA_DIRS": data}
	getenv := func(key string) string { return env[key] }
	players := func(available ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, p := range available {
				if p == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	for _, tc := range []struct {
		name      string
		sound     config.Sound
		blocked   bool
		available []string
		player    string
		file      string
	}{
		{"ready", config.Sound{}, false, []string{"pw-play", "paplay"}, "/usr/bin/pw-play", filepath.Join(theme, "complete.oga")},
		{"blocked", config.Sound{}, true, []string{"paplay"}, "/usr/bin/paplay", filepath.Join(theme, "dialog-warning.oga")},
		{"aplay can't decode the theme's Ogg files", config.Sound{}, false, []string{"aplay"}, "", ""},
		{"a custom WAV plays with aplay", config.Sound{Ready: custom}, false, []string{"aplay"}, "/usr/bin/aplay", custom},
		{"no player", config.Sound{}, false, nil, "", ""},
	} {
		player, file := alertCommand(tc.sound, tc.blocked, "linux", getenv, players(tc.available...))
		if player != tc.player || file != tc.file {
			t.Errorf("%s: %q %q, want %q %q", tc.name, player, file, tc.player, tc.file)
		}
	}
	// Without the theme, and over SSH, the terminal bell rings instead.
	env["XDG_DATA_DIRS"] = t.TempDir()
	if player, _ := alertCommand(config.Sound{}, false, "linux", getenv, players("pw-play")); player != "" {
		t.Errorf("played %s without a sound file", player)
	}
	env["XDG_DATA_DIRS"], env["SSH_CONNECTION"] = data, "10.0.0.1 22 10.0.0.2 22"
	if player, _ := alertCommand(config.Sound{}, false, "linux", getenv, players("pw-play")); player != "" {
		t.Errorf("played %s on the remote machine over SSH", player)
	}
}
