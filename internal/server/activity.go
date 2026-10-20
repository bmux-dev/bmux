package server

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Pane activity never interprets screen text. Agents are recognized by the
// foreground process of their pane; their state comes from `bmux agent`
// reports when an integration sends them, otherwise from output timing: an
// agent that keeps drawing (spinners, streamed text) is working, and one that
// goes quiet is waiting on you. BEL and OSC 9/777 notifications ask for
// attention from any program.

const maxAgentLabelBytes = 40

// agentSeqs numbers agent state changes across every pane of the server, so
// a seq someone saw earlier can only ever match the same pane and state.
var agentSeqs atomic.Uint64

const (
	agentSampleInterval = time.Second
	// Output this soon after input (typing, focus, resize) is a response to
	// that input, such as echo or a redraw, not the agent working on its own.
	inputEchoWindow = 750 * time.Millisecond
)

// Consecutive samples with and without output before the state changes come
// from agents.working-after and agents.waiting-after (2 seconds each by
// default), so one stray redraw never looks like a finished turn.

// Executable or script names of coding agents, mapped to their labels.
var agentCommands = map[string]string{
	"aider": "aider", "amp": "amp", "claude": "claude", "codex": "codex",
	"copilot": "copilot", "crush": "crush", "cursor-agent": "cursor", "droid": "droid",
	"gemini": "gemini", "goose": "goose", "grok": "grok", "kimi": "kimi",
	"kiro-cli": "kiro", "omp": "omp", "opencode": "opencode", "pi": "pi", "qwen": "qwen",
}

func agentMatch(command string) string {
	if name, ok := currentSettings().agents[filepath.Base(command)]; ok {
		return name
	}
	// Claude Code's native installer runs .../claude/versions/<version>.
	if dir := filepath.Dir(command); filepath.Base(dir) == "versions" && filepath.Base(filepath.Dir(dir)) == "claude" {
		return "claude"
	}
	return ""
}

// Agents written as scripts appear as their interpreter; only for these is
// the script path (argv[1]) inspected.
func isScriptInterpreter(exe string) bool {
	base := filepath.Base(exe)
	return base == "node" || base == "bun" || base == "deno" || strings.HasPrefix(base, "python")
}

func (s *pane) foregroundAgent() (string, error) {
	group, err := foregroundPTYGroup(s.pty)
	if err != nil {
		return "", err
	}
	if group <= 0 {
		return "", fmt.Errorf("PTY has no foreground process group")
	}
	return foregroundGroupCommand(group, agentMatch)
}

// Caller holds pane.mu. Records autonomous output for the agent sampler.
func (s *pane) noteOutputLocked(now time.Time) {
	if now.Sub(s.lastInput) > inputEchoWindow {
		s.outputSeen = true
	}
}

// observeAgentLocked applies one sample of the pane's foreground agent.
// Caller holds pane.mu.
func (s *pane) observeAgentLocked(detected string) {
	defer s.trackAgentLocked(s.agentViewLocked())
	if detected != s.detected {
		// The recognized agent left the foreground: its reports are stale.
		if detected == "" && s.detected != "" {
			s.agent, s.agentState, s.resume = "", "", nil
		}
		s.detected = detected
		s.busy = false
		s.activeSamples, s.quietSamples = 0, 0
	}
	output := s.outputSeen
	s.outputSeen = false
	if s.detected == "" {
		return
	}
	if output {
		s.activeSamples, s.quietSamples = s.activeSamples+1, 0
	} else {
		s.activeSamples, s.quietSamples = 0, s.quietSamples+1
	}
	settings := currentSettings()
	switch {
	case !s.busy && s.activeSamples >= settings.busySamples:
		s.busy = true
		if s.agentState == "" {
			s.attention = false
		}
	case s.busy && s.quietSamples >= settings.quietSamples:
		s.busy = false
		if s.agentState == "" {
			s.noteAttentionLocked()
		}
	}
}

// watchAgents samples foreground processes outside every state lock.
func (s *session) watchAgents() {
	ticker := time.NewTicker(agentSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		if s.stopping.Load() {
			return
		}
		s.sampleAgents()
	}
}

func (s *session) sampleAgents() {
	if !currentSettings().detect {
		return
	}
	s.mu.Lock()
	panes := make([]*pane, 0, len(s.panes))
	for _, p := range s.panes {
		panes = append(panes, p)
	}
	s.mu.Unlock()
	for _, p := range panes {
		detected, err := p.foregroundAgent()
		if err != nil {
			continue // keep the last sample; the shell may be starting or gone
		}
		p.mu.Lock()
		p.observeAgentLocked(detected)
		p.mu.Unlock()
	}
}

// Caller holds pane.mu. A pane someone is looking at never collects attention.
func (s *pane) noteAttentionLocked() {
	if !s.viewed.Load() {
		s.attention = true
	}
}

// trackNotifications marks unseen desktop-notification requests. OSC 9;4 is
// ConEmu progress reporting, not a notification. Handlers return false so the
// emulator keeps its own handling.
func (s *pane) trackNotifications() {
	s.term.RegisterOscHandler(9, func(data []byte) bool {
		_, arg, _ := strings.Cut(string(data), ";")
		if arg != "4" && !strings.HasPrefix(arg, "4;") {
			s.noteAttentionLocked()
		}
		return false
	})
	s.term.RegisterOscHandler(777, func(data []byte) bool {
		_, arg, _ := strings.Cut(string(data), ";")
		if arg == "notify" || strings.HasPrefix(arg, "notify;") {
			s.noteAttentionLocked()
		}
		return false
	})
}

func validateAgentLabel(label string) error {
	if len(label) > maxAgentLabelBytes {
		return fmt.Errorf("agent name must be at most %d bytes", maxAgentLabelBytes)
	}
	for _, r := range label {
		if !unicode.IsPrint(r) {
			return fmt.Errorf("agent name must be printable text")
		}
	}
	return nil
}

// Resume commands follow herdr's rules: a plain command name on PATH first,
// no control characters, at most 64 arguments and 8 KiB.
const (
	maxResumeArgs  = 64
	maxResumeBytes = 8 << 10
)

func validateResume(argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	if len(argv) > maxResumeArgs {
		return fmt.Errorf("resume command must have at most %d arguments", maxResumeArgs)
	}
	if argv[0] == "" || strings.ContainsRune(argv[0], '/') {
		return fmt.Errorf("resume command must start with a command name on PATH, not a path")
	}
	total := 0
	for _, arg := range argv {
		total += len(arg) + 1
		for _, r := range arg {
			if unicode.IsControl(r) {
				return fmt.Errorf("resume command must not contain control characters")
			}
		}
	}
	if total > maxResumeBytes {
		return fmt.Errorf("resume command must be at most %d bytes", maxResumeBytes)
	}
	return nil
}

// reportAgent applies a state report from the process running in this pane.
// A turn that finishes (working -> idle) or blocks while nobody is viewing the
// pane needs attention; starting new work means it no longer waits on anyone.
// A resume command, when given, replaces the one saved for restarts.
func (s *pane) reportAgent(state, label string, resume []string) error {
	label = strings.TrimSpace(label)
	if err := validateAgentLabel(label); err != nil {
		return err
	}
	if err := validateResume(resume); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.trackAgentLocked(s.agentViewLocked())
	switch state {
	case "clear":
		s.agent, s.agentState, s.attention, s.resume = "", "", false, nil
		return nil
	case protocol.AgentWorking, protocol.AgentBlocked, protocol.AgentIdle:
	default:
		return fmt.Errorf("agent state must be working, blocked, idle or clear")
	}
	if label != "" {
		s.agent = label
	} else if s.agent == "" {
		s.agent = "agent"
	}
	if len(resume) > 0 {
		s.resume = append([]string(nil), resume...)
	}
	finished := s.agentState == protocol.AgentWorking && state == protocol.AgentIdle
	s.agentState = state
	switch {
	case state == protocol.AgentWorking:
		s.attention = false
	case state == protocol.AgentBlocked || finished:
		s.noteAttentionLocked()
	}
	return nil
}

// agentViewLocked is the agent and state the pane shows: reported ones win
// over the sampled foreground agent and its output timing. Caller holds
// pane.mu.
func (s *pane) agentViewLocked() (agent, state string) {
	agent, state = s.agent, s.agentState
	if agent == "" {
		agent = s.detected
	}
	if state == "" && s.detected != "" {
		state = protocol.AgentIdle
		if s.busy {
			state = protocol.AgentWorking
		}
	}
	return agent, state
}

// trackAgentLocked gives the pane a new seq when its agent or state differs
// from the view captured before a change. Caller holds pane.mu.
func (s *pane) trackAgentLocked(agent, state string) {
	if now, nowState := s.agentViewLocked(); now != agent || nowState != state {
		s.agentSeq = agentSeqs.Add(1)
	}
}

// Caller holds pane.mu.
func (s *pane) activityLocked(id int) (protocol.PaneActivity, bool) {
	a := protocol.PaneActivity{Pane: id, Attention: s.attention, Seq: s.agentSeq}
	a.Agent, a.State = s.agentViewLocked()
	return a, a.Agent != "" || a.Attention
}

// Caller holds session.mu. Only the focused pane of the active window of a
// session with a terminal-focused client is viewed; becoming viewed clears its
// attention. Viewed is also the focus that apps see: a change sends focus in
// or out, which the emulator delivers only to apps that enabled focus events
// (?1004).
func (s *session) updateViewedLocked() {
	focused := s.focusedLocked()
	for id, p := range s.panes {
		viewed := id == focused && s.viewers > 0
		if p.viewed.Swap(viewed) != viewed && !s.stopping.Load() {
			_ = p.input(protocol.Request{Op: "focus", Focused: viewed})
		}
		if viewed {
			p.mu.Lock()
			p.attention = false
			p.mu.Unlock()
		}
	}
}

// setViewing records an attached client gaining or losing terminal focus.
func (s *session) setViewing(viewing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if viewing {
		s.viewers++
	} else {
		s.viewers = max(0, s.viewers-1)
	}
	s.updateViewedLocked()
}

func (s *session) reportAgent(paneID int, state, label string, resume []string) error {
	s.mu.Lock()
	if s.stopped || s.stopping.Load() {
		s.mu.Unlock()
		return fmt.Errorf("session has been terminated")
	}
	p := s.panes[paneID]
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("pane not found")
	}
	return p.reportAgent(state, label, resume)
}

// Caller holds session.mu. Panes are listed window by window, each in layout
// order, so background windows' agents still show.
func (s *session) activityLocked() []protocol.PaneActivity {
	var result []protocol.PaneActivity
	for _, w := range s.windows {
		var ids []int
		w.root.leaves(&ids)
		for _, id := range ids {
			p := s.panes[id]
			if p == nil {
				continue
			}
			p.mu.Lock()
			a, ok := p.activityLocked(id)
			p.mu.Unlock()
			if ok {
				result = append(result, a)
			}
		}
	}
	return result
}
