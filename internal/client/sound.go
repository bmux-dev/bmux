package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/josephferrero/bmux/internal/config"
)

// Alert sounds play on the machine running the attached client, like the
// clipboard. Custom files from the config file play with afplay on macOS and
// pw-play, paplay or aplay on Linux. By default macOS uses its system sounds
// and Linux the freedesktop sound theme most desktops install. Over SSH, or
// without a sound or a player, the outer terminal's bell rings instead, so
// the sound reaches the machine you're sitting at.
var alertSounds = map[bool]string{
	false: "/System/Library/Sounds/Glass.aiff", // an agent is ready
	true:  "/System/Library/Sounds/Funk.aiff",  // an agent is blocked
}

var freedesktopSounds = map[bool]string{
	false: "complete.oga",
	true:  "dialog-warning.oga",
}

func (u *ui) playAlert(blocked bool) {
	if !u.settings.Sound.Enabled {
		return
	}
	if player, path := alertCommand(u.settings.Sound, blocked, runtime.GOOS, os.Getenv, exec.LookPath); player != "" {
		cmd := exec.Command(player, path)
		if cmd.Start() == nil {
			go func() { _ = cmd.Wait() }()
			return
		}
	}
	_ = u.screen.Beep()
}

// alertCommand picks the player and sound file for an alert, or "" to ring
// the terminal bell instead.
func alertCommand(sound config.Sound, blocked bool, goos string, getenv func(string) string, lookPath func(string) (string, error)) (string, string) {
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return "", "" // a player here would sound on the remote machine
	}
	path := sound.Ready
	if blocked {
		path = sound.Blocked
	}
	if path == "" {
		path = defaultSound(blocked, goos, getenv)
	}
	if !fileExists(path) {
		return "", ""
	}
	players := []string{"pw-play", "paplay", "aplay"}
	if goos == "darwin" {
		players = []string{"afplay"}
	}
	for _, player := range players {
		if player == "aplay" && !strings.EqualFold(filepath.Ext(path), ".wav") {
			continue // aplay only decodes WAV
		}
		if found, err := lookPath(player); err == nil {
			return found, path
		}
	}
	return "", ""
}

func defaultSound(blocked bool, goos string, getenv func(string) string) string {
	if goos == "darwin" {
		return alertSounds[blocked]
	}
	var dirs []string
	if home := getenv("XDG_DATA_HOME"); home != "" {
		dirs = append(dirs, home)
	} else if home := getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share"))
	}
	system := getenv("XDG_DATA_DIRS")
	if system == "" {
		system = "/usr/local/share:/usr/share"
	}
	dirs = append(dirs, filepath.SplitList(system)...)
	for _, dir := range dirs {
		if path := filepath.Join(dir, "sounds", "freedesktop", "stereo", freedesktopSounds[blocked]); fileExists(path) {
			return path
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
