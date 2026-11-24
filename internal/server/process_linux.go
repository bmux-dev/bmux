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

func processCwd(pid int) (string, error) {
	return os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
}

// foregroundGroupCommand mirrors the Darwin contract: comm, the executable
// path, and for script interpreters only argv[1] are candidates.
//
// It inspects /proc/<group> (and its descendants via /proc/<pid>/task/*/children)
// directly in O(1) before falling back to full /proc enumeration for pipelines
// or orphaned groups whose leader has exited.
func foregroundGroupCommand(group int, match func(string) string) (string, error) {
	return foregroundGroupCommandIn("/proc", group, match)
}

func foregroundGroupCommandIn(root string, group int, match func(string) string) (string, error) {
	if group > 0 {
		found, live := matchProcEntry(root, group, group, match)
		if found != "" {
			return found, nil
		}
		if live {
			visited := map[int]struct{}{group: {}}
			queue := procChildren(root, group)
			for len(queue) > 0 {
				pid := queue[0]
				queue = queue[1:]
				if _, seen := visited[pid]; seen {
					continue
				}
				visited[pid] = struct{}{}
				found, childLive := matchProcEntry(root, pid, group, match)
				if found != "" {
					return found, nil
				}
				if childLive {
					queue = append(queue, procChildren(root, pid)...)
				}
			}
			return foregroundGroupCommandScan(root, group, visited, match)
		}
	}
	return foregroundGroupCommandScan(root, group, nil, match)
}

func foregroundGroupCommandScan(root string, group int, skip map[int]struct{}, match func(string) string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if _, seen := skip[pid]; seen {
			continue
		}
		if found, _ := matchProcEntry(root, pid, group, match); found != "" {
			return found, nil
		}
	}
	return "", nil
}

// matchProcEntry checks whether pid is a live (non-zombie, non-stopped) member
// of group and applies match to its comm, executable path, and (for script
// interpreters) argv[1].
func matchProcEntry(root string, pid, group int, match func(string) string) (string, bool) {
	dir := root + "/" + strconv.Itoa(pid)
	data, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return "", false
	} // Processes may exit during inspection.
	s := string(data)
	start, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if start < 0 || end <= start {
		return "", false
	}
	fields := strings.Fields(s[end+1:])
	if len(fields) < 3 || fields[0] == "Z" || fields[0] == "T" || fields[0] == "t" {
		return "", false
	}
	if pgid, err := strconv.Atoi(fields[2]); err != nil || pgid != group {
		return "", false
	}
	if found := match(s[start+1 : end]); found != "" {
		return found, true
	}
	exe, err := os.Readlink(dir + "/exe")
	if err != nil {
		return "", true
	}
	if found := match(exe); found != "" {
		return found, true
	}
	if !isScriptInterpreter(exe) {
		return "", true
	}
	if cmdline, err := os.ReadFile(dir + "/cmdline"); err == nil {
		if args := strings.SplitN(string(cmdline), "\x00", 3); len(args) > 1 {
			if found := match(args[1]); found != "" {
				return found, true
			}
		}
	}
	return "", true
}

func procChildren(root string, pid int) []int {
	taskDir := root + "/" + strconv.Itoa(pid) + "/task"
	tasks, err := os.ReadDir(taskDir)
	if err != nil {
		return nil
	}
	var children []int
	for _, t := range tasks {
		data, err := os.ReadFile(taskDir + "/" + t.Name() + "/children")
		if err != nil {
			continue
		}
		for _, f := range strings.Fields(string(data)) {
			if child, err := strconv.Atoi(f); err == nil && child > 0 {
				children = append(children, child)
			}
		}
	}
	return children
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
