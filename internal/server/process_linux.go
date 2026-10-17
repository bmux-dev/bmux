//go:build linux

package server

import (
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"strings"
)

func inspectProcess(pid int) (processIdentity, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return processIdentity{}, err
	}
	// comm may contain spaces and parentheses; fields after its last ')' start
	// with state (field 3), session is field 6 and starttime is field 22.
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return processIdentity{}, unix.ESRCH
	}
	f := strings.Fields(string(data[i+1:]))
	if len(f) < 20 {
		return processIdentity{}, unix.ESRCH
	}
	sid, err := strconv.Atoi(f[3])
	return processIdentity{pid: pid, sid: sid, birth: f[19], zombie: f[0] == "Z"}, err
}

func foregroundEditorGroup(group int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		} // Processes may exit during enumeration.
		start, end := strings.IndexByte(string(data), '('), strings.LastIndexByte(string(data), ')')
		if start < 0 || end <= start {
			continue
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 3 || fields[0] == "Z" || fields[0] == "T" || fields[0] == "t" {
			continue
		}
		pgid, err := strconv.Atoi(fields[2])
		if err == nil && pgid == group && isEditorCommand(string(data[start+1:end])) {
			return true, nil
		}
	}
	return false, nil
}

func listProcesses() ([]processIdentity, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var result []processIdentity
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			if p, err := inspectProcess(pid); err == nil {
				result = append(result, p)
			}
		}
	}
	return result, nil
}
