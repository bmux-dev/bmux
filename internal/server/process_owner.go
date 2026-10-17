package server

import (
	"golang.org/x/sys/unix"
	"time"
)

type processIdentity struct {
	pid, sid int
	birth    string
	zombie   bool
}
type processOwner struct {
	sid   int
	birth string
}

func newProcessOwner(pid int) processOwner {
	p, _ := inspectProcess(pid)
	return processOwner{sid: pid, birth: p.birth}
}

// Only members of our shell's OS session are owned. Ordinary job-control
// groups are included; processes that deliberately setsid/daemonize are not.
// No group-wide kill is used: each PID's session and birth identity are checked
// immediately before signalling, avoiding unrelated group members and stale
// enumeration. POSIX has no atomic identity-checked kill; the tiny check/kill
// race cannot be eliminated portably (Darwin has no Linux pidfd equivalent).
func (o processOwner) members() []processIdentity {
	if o.birth == "" {
		return nil
	}
	if leader, err := inspectProcess(o.sid); err == nil && leader.birth != o.birth {
		return nil
	}
	all, _ := listProcesses()
	var result []processIdentity
	for _, p := range all {
		if p.sid == o.sid && !p.zombie {
			result = append(result, p)
		}
	}
	return result
}

func (o processOwner) signal(sig unix.Signal) {
	for _, p := range o.members() {
		current, err := inspectProcess(p.pid)
		if err == nil && current.sid == o.sid && current.birth == p.birth {
			_ = unix.Kill(p.pid, sig)
		}
	}
}

const paneShutdownBudget = 1250 * time.Millisecond

func (s *pane) stopUntil(deadline time.Time) {
	s.stopOnce.Do(func() {
		defer close(s.stopDone)
		// Explicit HUP precedes closing the master, giving shell traps a chance
		// even when Close itself would not produce a controlling-terminal HUP.
		s.owner.signal(unix.SIGHUP)
		s.owner.signal(unix.SIGCONT)
		// Cancel without acquiring pane.mu (including io.Pipe blocked writes).
		s.closeIO()
		termAt, killAt := deadline.Add(-750*time.Millisecond), deadline.Add(-250*time.Millisecond)
		terminated, killed := false, false
		for {
			now := time.Now()
			if !terminated && !now.Before(termAt) {
				s.owner.signal(unix.SIGTERM)
				s.owner.signal(unix.SIGCONT)
				terminated = true
			}
			if !killed && !now.Before(killAt) {
				s.owner.signal(unix.SIGKILL)
				killed = true
			}
			select {
			case <-s.done:
				if len(s.owner.members()) == 0 {
					return
				}
			default:
			}
			if !now.Before(deadline) {
				s.owner.signal(unix.SIGKILL)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	select {
	case <-s.stopDone:
	case <-time.After(max(time.Duration(0), time.Until(deadline))):
	}
}
