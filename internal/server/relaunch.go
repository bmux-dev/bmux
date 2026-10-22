package server

import (
	"os"
	"path/filepath"
	"unicode"
)

// Relaunching starts allowlisted programs (restore.relaunch) again in
// restored panes, with the arguments they had, like tmux-resurrect's
// @resurrect-processes. Only a foreground job's leader counts, the command
// the shell started, and only when everything else in the job descends from
// it: man's pager or a command run from Vim does, but the other side of a
// pipe doesn't, so `tail -f log | grep error` restores as a shell rather
// than as an unfiltered tail. Arguments are read only for allowlisted
// programs; other command lines may hold secrets and are never read or saved.

type groupMember struct{ pid, ppid int }

// descendsFromLeader: every member is group's leader or below it.
func descendsFromLeader(group int, members []groupMember) bool {
	parent := make(map[int]int, len(members))
	leader := false
	for _, m := range members {
		parent[m.pid] = m.ppid
		leader = leader || m.pid == group
	}
	if !leader {
		return false
	}
	for _, m := range members {
		pid := m.pid
		for steps := 0; pid != group; steps++ {
			next, ok := parent[pid]
			// A parent outside the group or a cycle: not the leader's.
			if !ok || steps > len(members) {
				return false
			}
			pid = next
		}
	}
	return true
}

// relaunchCommand is the command line of the pane's foreground job when its
// leader is allowlisted, else nil. It queries the OS: hold no locks.
func (s *pane) relaunchCommand(allowed map[string]bool) []string {
	if len(allowed) == 0 {
		return nil
	}
	group, err := foregroundPTYGroup(s.pty)
	if err != nil || group <= 0 || group == s.cmd.Process.Pid {
		return nil
	}
	comm, exe, ok := jobLeader(group)
	if !ok || !allowed[comm] && (exe == "" || !allowed[filepath.Base(exe)]) {
		return nil
	}
	if members, err := groupMembers(group); err != nil || !descendsFromLeader(group, members) {
		return nil
	}
	argv, err := processArgv(group)
	if err != nil || !relaunchable(argv, allowed) {
		return nil
	}
	return argv
}

// relaunchable: the program is allowlisted by its own name and nothing in
// the line could act as a keystroke when typed into the shell.
func relaunchable(argv []string, allowed map[string]bool) bool {
	if len(argv) == 0 || !allowed[filepath.Base(argv[0])] {
		return false
	}
	for _, arg := range argv {
		for _, r := range arg {
			if unicode.IsControl(r) {
				return false
			}
		}
	}
	return true
}

// relaunchArgv is the line to type in a restored pane in dir: argv, except
// that Vim or Neovim started without arguments loads the directory's
// Session.vim (:mksession) when there is one, as resurrect does.
func relaunchArgv(argv []string, dir string) []string {
	if len(argv) == 1 {
		switch filepath.Base(argv[0]) {
		case "vim", "nvim":
			if info, err := os.Stat(filepath.Join(dir, "Session.vim")); err == nil && info.Mode().IsRegular() {
				return []string{argv[0], "-S"}
			}
		}
	}
	return argv
}
