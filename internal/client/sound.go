package client

import (
	"os"
	"os/exec"
	"runtime"
)

// Alert sounds play on the machine running the attached client, like the
// clipboard. macOS uses its built-in system sounds; elsewhere, and if those
// are missing, the outer terminal's bell. BMUX_SOUND=0 turns them off.
var alertSounds = map[bool]string{
	false: "/System/Library/Sounds/Glass.aiff", // an agent is ready
	true:  "/System/Library/Sounds/Funk.aiff",  // an agent is blocked
}

func (u *ui) playAlert(blocked bool) {
	if os.Getenv("BMUX_SOUND") == "0" {
		return
	}
	if runtime.GOOS == "darwin" {
		if path := alertSounds[blocked]; fileExists(path) {
			cmd := exec.Command("afplay", path)
			if cmd.Start() == nil {
				go func() { _ = cmd.Wait() }()
				return
			}
		}
	}
	_ = u.screen.Beep()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
