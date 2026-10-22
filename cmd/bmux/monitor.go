package main

import (
	"fmt"

	"github.com/josephferrero/bmux/internal/client"
)

// runMonitor turns a pane's activity or silence alerts on or off, like
// tmux's monitor-activity and monitor-silence, or with no setting prints the
// pane's current ones.
func runMonitor(socket string, args []string) error {
	const usage = "usage: bmux monitor [-t TARGET] [activity on|off | silence DURATION|on|off]"
	a, err := parseLayoutArgs(args, usage, []string{"-t"}, nil)
	if err != nil {
		return err
	}
	switch len(a.words) {
	case 0:
		info, err := shellPane(socket, a.values["-t"])
		if err != nil {
			return err
		}
		activity, silence := "off", "off"
		if info.MonitorActivity {
			activity = "on"
		}
		if info.MonitorSilence > 0 {
			silence = fmt.Sprintf("%ds", info.MonitorSilence)
		}
		fmt.Printf("activity %s, silence %s\n", activity, silence)
		return nil
	case 2:
	default:
		return fmt.Errorf("%s", usage)
	}
	kind, value := a.words[0], a.words[1]
	if kind != "activity" && kind != "silence" {
		return fmt.Errorf("%s", usage)
	}
	r, err := layoutRequest("", a.values["-t"])
	if err != nil {
		return err
	}
	r.Op, r.Kind, r.Text = "monitor", kind, value
	_, err = client.Request(socket, r)
	return err
}
