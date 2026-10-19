package client

import (
	"os"
	"os/exec"
	"runtime"
)

// Alert sounds play on the machine running the attached client, like the
// clipboard. Custom files from the config file play with afplay on macOS and
// paplay or aplay on Linux; otherwise macOS uses its built-in system sounds.
// Without a player, the outer terminal's bell rings instead.
var alertSounds = map[bool]string{
	false: "/System/Library/Sounds/Glass.aiff", // an agent is ready
	true:  "/System/Library/Sounds/Funk.aiff",  // an agent is blocked
}

func (u *ui) playAlert(blocked bool) {
	sound := u.settings.Sound
	if !sound.Enabled {
		return
	}
	path := sound.Ready
	if blocked {
		path = sound.Blocked
	}
	if path == "" && runtime.GOOS == "darwin" {
		path = alertSounds[blocked]
	}
	if fileExists(path) {
		for _, player := range soundPlayers() {
			cmd := exec.Command(player, path)
			if cmd.Start() == nil {
				go func() { _ = cmd.Wait() }()
				return
			}
		}
	}
	_ = u.screen.Beep()
}

func soundPlayers() []string {
	if runtime.GOOS == "darwin" {
		return []string{"afplay"}
	}
	return []string{"paplay", "aplay"}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
