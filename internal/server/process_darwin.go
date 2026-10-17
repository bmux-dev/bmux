//go:build darwin

package server

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

func darwinIdentity(p unix.KinfoProc) (processIdentity, error) {
	pid := int(p.Proc.P_pid)
	sid, err := unix.Getsid(pid)
	return processIdentity{pid: pid, sid: sid, birth: fmt.Sprintf("%d:%d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec), zombie: p.Proc.P_stat == 5}, err
}

func foregroundEditorGroup(group int) (bool, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", group)
	if err != nil {
		return false, err
	}
	var lookupErr error
	for _, p := range processes {
		// Exclude stopped and zombie jobs even if a wrapper shares their group.
		if int(p.Eproc.Pgid) != group || p.Proc.P_stat == 4 || p.Proc.P_stat == 5 {
			continue
		}
		name := strings.TrimRight(string(p.Proc.P_comm[:]), "\x00")
		if isEditorCommand(name) {
			return true, nil
		}
		// Darwin's short comm field can contain a truncated absolute path.
		// PROCARGS2 starts with argc followed by the full executable path; do
		// not inspect or log the later arguments/environment fields.
		args, err := unix.SysctlRaw("kern.procargs2", int(p.Proc.P_pid))
		if err != nil {
			lookupErr = err
			continue
		}
		if len(args) < 5 {
			lookupErr = fmt.Errorf("missing foreground executable path")
			continue
		}
		path := args[4:]
		end := bytes.IndexByte(path, 0)
		if end <= 0 {
			lookupErr = fmt.Errorf("missing foreground executable path")
			continue
		}
		if isEditorCommand(string(path[:end])) {
			return true, nil
		}
	}
	return false, lookupErr
}

func inspectProcess(pid int) (processIdentity, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return processIdentity{}, err
	}
	if int(p.Proc.P_pid) != pid {
		return processIdentity{}, unix.ESRCH
	}
	return darwinIdentity(*p)
}

func listProcesses() ([]processIdentity, error) {
	ps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	var result []processIdentity
	for _, p := range ps {
		if v, err := darwinIdentity(p); err == nil {
			result = append(result, v)
		}
	}
	return result, nil
}
