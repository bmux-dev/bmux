//go:build darwin

package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func darwinIdentity(p unix.KinfoProc) (processIdentity, error) {
	pid := int(p.Proc.P_pid)
	sid, err := unix.Getsid(pid)
	return processIdentity{pid: pid, sid: sid, birth: fmt.Sprintf("%d:%d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec), zombie: p.Proc.P_stat == 5}, err
}

// foregroundGroupCommand returns the first non-empty match for a live member
// of the foreground group. Candidates are the short comm name, the full
// executable path, and for script interpreters only the script path (argv[1]).
// Nothing else from the argument or environment area is read or retained.
func foregroundGroupCommand(group int, match func(string) string) (string, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", group)
	if err != nil {
		return "", err
	}
	var lookupErr error
	for _, p := range processes {
		// Exclude stopped and zombie jobs even if a wrapper shares their group.
		if int(p.Eproc.Pgid) != group || p.Proc.P_stat == 4 || p.Proc.P_stat == 5 {
			continue
		}
		// p_comm is NUL-terminated; bytes after the NUL can be left over from
		// a longer name the process had before exec.
		name, _, _ := strings.Cut(string(p.Proc.P_comm[:]), "\x00")
		if found := match(name); found != "" {
			return found, nil
		}
		// Darwin's short comm field can contain a truncated absolute path.
		exe, script, err := darwinCommandPaths(int(p.Proc.P_pid))
		if err != nil {
			lookupErr = err
			continue
		}
		if found := match(exe); found != "" {
			return found, nil
		}
		if script != "" {
			if found := match(script); found != "" {
				return found, nil
			}
		}
	}
	return "", lookupErr
}

// PROCARGS2 is argc, the executable path, NUL padding, then argv.
func darwinCommandPaths(pid int) (exe, script string, err error) {
	args, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", "", err
	}
	if len(args) < 5 {
		return "", "", fmt.Errorf("missing foreground executable path")
	}
	argc := int(binary.LittleEndian.Uint32(args[:4]))
	rest := args[4:]
	end := bytes.IndexByte(rest, 0)
	if end <= 0 {
		return "", "", fmt.Errorf("missing foreground executable path")
	}
	exe, rest = string(rest[:end]), rest[end:]
	if argc < 2 || !isScriptInterpreter(exe) {
		return exe, "", nil
	}
	rest = bytes.TrimLeft(rest, "\x00")
	for i := range 2 {
		end = bytes.IndexByte(rest, 0)
		if end < 0 {
			return exe, "", nil
		}
		if i == 1 {
			script = string(rest[:end])
		}
		rest = rest[end+1:]
	}
	return exe, script, nil
}

// proc_pidinfo(PROC_PIDVNODEPATHINFO) returns struct proc_vnodepathinfo: the
// current directory's vnode_info (152 bytes) then its MAXPATHLEN path, then
// the same for the root directory. libproc is a thin wrapper over this call.
const (
	procInfoCallPIDInfo  = 2
	procPIDVnodePathInfo = 9
	vnodeInfoBytes       = 152
	maxPathBytes         = 1024
	vnodePathInfoBytes   = 2 * (vnodeInfoBytes + maxPathBytes)
)

func processCwd(pid int) (string, error) {
	buf := make([]byte, vnodePathInfoBytes)
	n, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDVnodePathInfo, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if errno != 0 {
		return "", errno
	}
	if int(n) < vnodeInfoBytes+maxPathBytes {
		return "", fmt.Errorf("short process directory information")
	}
	path := buf[vnodeInfoBytes : vnodeInfoBytes+maxPathBytes]
	end := bytes.IndexByte(path, 0)
	if end <= 0 {
		return "", fmt.Errorf("process has no current directory")
	}
	return string(path[:end]), nil
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
