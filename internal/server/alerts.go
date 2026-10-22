package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/josephferrero/bmux/internal/config"
)

// Activity and silence alerts, like tmux's monitor-activity and
// monitor-silence, for panes that ask for them. A monitored pane nobody is
// looking at gets the attention mark a bell gives it (its sidebar row and its
// window's ! tab), labelled by kind, and runs the alert-activity or
// alert-silence hook. Looking at the pane clears the mark, as for a bell.

const (
	alertActivity = "activity"
	alertSilence  = "silence"
)

// noteAlertLocked raises attention for an alert unless the pane is in view
// or already marked. Caller holds pane.mu.
func (s *pane) noteAlertLocked(kind string) {
	if s.viewed.Load() || s.attention {
		return
	}
	s.attention, s.alert = true, kind
	if s.hook != nil {
		s.hook("alert-" + kind)
	}
}

// noteAlertOutputLocked records output for the alerts: any output starts a
// new quiet stretch, and output the program printed by itself (autonomous,
// not echo of input) is activity. Caller holds pane.mu.
func (s *pane) noteAlertOutputLocked(now time.Time, autonomous bool) {
	s.lastOutput, s.silenceAlerted = now, false
	if s.monitorActivity && autonomous {
		s.noteAlertLocked(alertActivity)
	}
}

// checkSilenceLocked marks a silence-monitored pane once it has printed
// nothing for its duration, once per quiet stretch. A pane in view when the
// time passes isn't marked later either. Caller holds pane.mu.
func (s *pane) checkSilenceLocked(now time.Time) {
	if s.monitorSilence == 0 || s.silenceAlerted || s.info.Exited || now.Sub(s.lastOutput) < s.monitorSilence {
		return
	}
	s.silenceAlerted = true
	s.noteAlertLocked(alertSilence)
}

// checkSilence runs checkSilenceLocked on every pane; the agent sampler's
// ticker drives it.
func (s *session) checkSilence(now time.Time) {
	s.mu.Lock()
	panes := make([]*pane, 0, len(s.panes))
	for _, p := range s.panes {
		panes = append(panes, p)
	}
	s.mu.Unlock()
	for _, p := range panes {
		p.mu.Lock()
		p.checkSilenceLocked(now)
		p.mu.Unlock()
	}
}

// parseSilence reads a silence duration: "on" for alerts.silence-after, "off"
// or "0" for none, a Go duration ("30s", "2m") or whole seconds ("30").
func parseSilence(value string) (time.Duration, error) {
	switch value = strings.TrimSpace(strings.ToLower(value)); value {
	case "on":
		return currentSettings().silenceAfter, nil
	case "off", "0":
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		seconds, numErr := strconv.Atoi(value)
		if numErr != nil {
			return 0, fmt.Errorf("silence %q must be on, off, or a duration such as 30s", value)
		}
		d = time.Duration(seconds) * time.Second
	}
	if d < config.MinSilenceAfter || d > config.MaxSilenceAfter {
		return 0, fmt.Errorf("silence %s must be between 1s and 1h", d)
	}
	return d, nil
}

// monitor changes what pane t is monitored for. kind is "activity" or
// "silence"; value is "on", "off", "toggle", or for silence a duration. It
// returns a sentence saying the result, for the status line.
func (s *session) monitor(t paneTarget, kind, value string) (string, error) {
	s.mu.Lock()
	id, err := s.resolveLocked(t)
	p := s.panes[id]
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	toggle := strings.EqualFold(strings.TrimSpace(value), "toggle")
	switch kind {
	case alertActivity:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "on":
			p.monitorActivity = true
		case "off":
			p.monitorActivity = false
		case "toggle":
			p.monitorActivity = !p.monitorActivity
		default:
			return "", fmt.Errorf("activity %q must be on, off or toggle", value)
		}
		if p.monitorActivity {
			return fmt.Sprintf("Pane %d: marked when it prints", id), nil
		}
		return fmt.Sprintf("Pane %d: no longer watched for activity", id), nil
	case alertSilence:
		var d time.Duration
		if toggle {
			if p.monitorSilence == 0 {
				d = currentSettings().silenceAfter
			}
		} else if d, err = parseSilence(value); err != nil {
			return "", err
		}
		// The quiet stretch starts now, so an already quiet pane is marked
		// d from now rather than at once.
		p.monitorSilence, p.lastOutput, p.silenceAlerted = d, time.Now(), false
		if d > 0 {
			return fmt.Sprintf("Pane %d: marked after %s without output", id, d), nil
		}
		return fmt.Sprintf("Pane %d: no longer watched for silence", id), nil
	}
	return "", fmt.Errorf("monitor %q: use activity or silence", kind)
}
