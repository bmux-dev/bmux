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

// foregroundGroupCommand mirrors the Darwin contract: comm, the executable
// path, and for script interpreters only argv[1] are candidates.
func foregroundGroupCommand(group int, match func(string) string) (string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		dir := "/proc/" + entry.Name()
		data, err := os.ReadFile(dir + "/stat")
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
		if pgid, err := strconv.Atoi(fields[2]); err != nil || pgid != group {
			continue
		}
		if found := match(string(data[start+1 : end])); found != "" {
			return found, nil
		}
		exe, err := os.Readlink(dir + "/exe")
		if err != nil {
			continue
		}
		if found := match(exe); found != "" {
			return found, nil
		}
		if !isScriptInterpreter(exe) {
			continue
		}
		if cmdline, err := os.ReadFile(dir + "/cmdline"); err == nil {
			if args := strings.SplitN(string(cmdline), "\x00", 3); len(args) > 1 {
				if found := match(args[1]); found != "" {
					return found, nil
				}
			}
		}
	}
	return "", nil
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
