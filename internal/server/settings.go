package server

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/josephferrero/bmux/internal/config"
)

// serverSettings are the config file's server-side settings. One server runs
// per process, so they are process-wide and swapped atomically on reload;
// samplers and saves read the current value each time.
type serverSettings struct {
	detect                    bool
	agents                    map[string]string // program name -> agent label
	busySamples, quietSamples int
	restore                   bool
	resume                    map[string][]string // per-agent override; empty = none
}

var (
	activeSettings atomic.Pointer[serverSettings]
	// configRevision counts reloads; frames carry it so clients reload too.
	configRevision atomic.Uint64
)

func init() { activeSettings.Store(settingsFrom(config.Default())) }

func currentSettings() *serverSettings { return activeSettings.Load() }

func samplesFor(d time.Duration) int {
	return max(1, int((d+agentSampleInterval-1)/agentSampleInterval))
}

func settingsFrom(cfg config.Config) *serverSettings {
	agents := make(map[string]string, len(agentCommands)+len(cfg.Agents.Extra))
	for program, label := range agentCommands {
		agents[program] = label
	}
	for _, program := range cfg.Agents.Extra {
		agents[program] = program
	}
	return &serverSettings{
		detect:       cfg.Agents.Detect,
		agents:       agents,
		busySamples:  samplesFor(cfg.Agents.WorkingAfter.Duration),
		quietSamples: samplesFor(cfg.Agents.WaitingAfter.Duration),
		restore:      cfg.Restore.Enabled,
		resume:       cfg.Restore.Resume,
	}
}

// resumeFor is an agent's resume command: the config file's override when
// set (an empty list disables resuming it), else the built-in one.
func (s *serverSettings) resumeFor(agent string) ([]string, bool) {
	if argv, ok := s.resume[agent]; ok {
		return argv, len(argv) > 0
	}
	argv, ok := agentContinue[agent]
	return argv, ok
}

// LoadSettings reads the config file and applies its server-side settings.
// Invalid settings keep their defaults; the returned error describes them.
func (s *Server) LoadSettings(path string) error {
	cfg, err := config.Load(path, os.Getenv)
	activeSettings.Store(settingsFrom(cfg))
	s.persistMu.Lock()
	s.configPath = path
	s.persistMu.Unlock()
	return err
}

// ReloadSettings rereads the config file and tells attached clients to
// reload theirs, by bumping the revision their frames carry.
func (s *Server) ReloadSettings() error {
	s.persistMu.Lock()
	path := s.configPath
	s.persistMu.Unlock()
	if path == "" {
		return fmt.Errorf("this server has no config file")
	}
	err := s.LoadSettings(path)
	configRevision.Add(1)
	s.mu.Lock()
	items := make([]*session, 0, len(s.sessions))
	for _, item := range s.sessions {
		items = append(items, item)
	}
	s.mu.Unlock()
	for _, item := range items {
		item.mu.Lock()
		item.revision++
		item.mu.Unlock()
	}
	if !currentSettings().detect {
		for _, item := range items {
			item.forgetDetectedAgents()
		}
	}
	return err
}

// forgetDetectedAgents drops detection results once detection is turned off;
// reported agents stay.
func (s *session) forgetDetectedAgents() {
	s.mu.Lock()
	panes := make([]*pane, 0, len(s.panes))
	for _, p := range s.panes {
		panes = append(panes, p)
	}
	s.mu.Unlock()
	for _, p := range panes {
		p.mu.Lock()
		p.detected, p.busy, p.activeSamples, p.quietSamples = "", false, 0, 0
		p.mu.Unlock()
	}
}
