package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Native clipboard access belongs to the attached client, never the server.
// Through SSH, request OSC 52 via the terminal instead of a remote desktop's clipboard.
func copyNative(text string) (bool, error) {
	return copyNativeContext(context.Background(), text)
}

func copyNativeContext(parent context.Context, text string) (bool, error) {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false, nil
	}
	var candidates [][]string
	if runtime.GOOS == "darwin" {
		candidates = [][]string{{"pbcopy"}}
	} else {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			candidates = append(candidates, []string{"wl-copy"})
		}
		if os.Getenv("DISPLAY") != "" {
			candidates = append(candidates, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
		}
	}
	for _, args := range candidates {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		return true, runClipboardHelper(parent, path, args[1:], text, 2*time.Second)
	}
	return false, nil
}

const clipboardDiagnosticBytes = 16 << 10

// cappedClipboardOutput keeps diagnostics bounded while still draining all
// helper output. Only exec's stderr copier accesses it, and Wait joins it.
type cappedClipboardOutput struct {
	data      []byte
	truncated bool
}

func (w *cappedClipboardOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := clipboardDiagnosticBytes - len(w.data)
	if len(p) > remaining {
		p = p[:remaining]
		w.truncated = true
	}
	w.data = append(w.data, p...)
	return n, nil
}

func (w *cappedClipboardOutput) String() string {
	text := strings.TrimSpace(string(w.data))
	if w.truncated {
		text += " [diagnostics truncated]"
	}
	return text
}

func runClipboardHelper(parent context.Context, path string, args []string, text string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(text)
	// Nil stdout goes directly to the null device, not an inherited Go pipe.
	// Clipboard tools may detach an owner which must survive successful copying.
	// Bound stderr draining without killing that owner or its process group.
	cmd.WaitDelay = 100 * time.Millisecond
	var output cappedClipboardOutput
	cmd.Stderr = &output
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) && ctx.Err() == nil {
		// The helper itself exited successfully; only an inherited descriptor
		// remained open. Closing our diagnostic pipe is not a clipboard failure.
		return nil
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return fmt.Errorf("%s: %w %s", path, err, output.String())
	}
	return nil
}
