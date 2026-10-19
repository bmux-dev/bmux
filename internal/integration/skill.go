package integration

import (
	"os"
	"path/filepath"
)

// The bmux skill tells an agent, before it has to explore, that it can list,
// read, answer and wait on other panes, and points it at `bmux guide` for
// the details. It ships with the integration of every agent that loads
// SKILL.md files from its config directory.

func skillPath(dir string) string { return filepath.Join(dir, "skills", "bmux", "SKILL.md") }

// withSkill adds the skill to an integration. A missing or older skill makes
// an installed integration count as needing an update.
func withSkill(a Agent) Agent {
	install, uninstall, installed := a.install, a.uninstall, a.installed
	a.install = func(dir string) error {
		if err := install(dir); err != nil {
			return err
		}
		return writeFile(skillPath(dir), []byte(skillText(a.Name, a.Version)), 0o644)
	}
	a.uninstall = func(dir string) error {
		if err := uninstall(dir); err != nil {
			return err
		}
		if err := removeFile(skillPath(dir), a.Name); err != nil {
			return err
		}
		_ = os.Remove(filepath.Dir(skillPath(dir))) // only when nothing else is in it
		return nil
	}
	a.installed = func(dir string) (int, error) {
		version, err := installed(dir)
		if err != nil || version == 0 {
			return version, err
		}
		skill, err := fileVersion(skillPath(dir), a.Name)
		return min(version, max(skill, 1)), err
	}
	return a
}

func skillText(name string, version int) string {
	return `---
name: bmux
description: See, read, answer and wait on other terminal panes and coding agents running in bmux. Use inside bmux (BMUX_SESSION is set) when asked about other panes, sessions or agents, what they are doing or asking, or to answer them or be told when they need the user.
---
<!--
` + headerLines("", name, version) + `-->

# bmux

You run inside bmux, a terminal multiplexer. Other panes may run shells or
coding agents. Run ` + "`bmux guide`" + ` once for the full reference; in short:

- ` + "`bmux agents --json`" + `: every agent pane as a SESSION:PANE target, with its
  state (working, blocked, idle) and seq.
- ` + "`bmux capture --json TARGET`" + `: the pane's screen, to read what it asks.
- ` + "`bmux send TARGET --if-seq SEQ --key down --key enter`" + `: answer it. Refused
  as stale if the agent moved on since that seq; capture again.
- ` + "`bmux wait-agent --notify $BMUX_SESSION:$BMUX_PANE`" + `, started in a new
  session of its own (see ` + "`bmux guide`" + `), types a ` + "`[bmux]`" + ` line into your
  pane whenever another agent is blocked or idle, only while you're idle.
  Never run wait-agent yourself, not even as a background job: it holds your
  turn, and the user can't talk to you.

Answer agents only with what the user decided, read permission prompts back
before approving them, and never type into a shell pane unasked, except the
notifier session you start for yourself.
`
}
