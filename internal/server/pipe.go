package server

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/josephferrero/bmux/internal/format"
	"github.com/josephferrero/bmux/internal/protocol"
)

// A pane pipe copies everything a pane prints into a command's stdin, like
// tmux's pipe-pane. The copy never slows the pane: output waits in a
// bounded queue and is dropped when the command falls that far behind.
//
// pane.pipe is set and cleared under pane.mu, and only a pipe that has been
// cleared from its pane is stopped, so write never runs after stop.

const pipeQueue = 1024 // output chunks waiting for the command

// pipeGrace is how long a stopped pipe's command gets to exit after end of
// file before its process group is ended; tests shorten it.
var pipeGrace = 5 * time.Second

type panePipe struct {
	command string
	chunks  chan []byte
	exited  chan struct{}
	once    sync.Once
	cmd     *exec.Cmd
	grace   time.Duration // pipeGrace when it started
}

// startPipe runs command for info's pane with /bin/sh: its #{...}
// variables, directory and BMUX_* environment. attach makes the pipe the
// pane's before anything can stop it; onExit runs once the command exits by
// itself or after stop, and must clear the pipe from its pane.
func startPipe(command string, info protocol.PaneInfo, attach, onExit func(*panePipe)) (*panePipe, error) {
	cmd := exec.Command("/bin/sh", "-c", format.Expand(command, format.Vars(info)))
	cmd.Dir = hookDir(info)
	cmd.Env = append(shellEnv(os.Environ(), info.SessionID, info.Session, info.Pane), "BMUX_PIPE=1")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr // the server log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &panePipe{command: command, chunks: make(chan []byte, pipeQueue), exited: make(chan struct{}), cmd: cmd, grace: pipeGrace}
	attach(p)
	go func() {
		for chunk := range p.chunks {
			if _, err := stdin.Write(chunk); err != nil {
				break // the command stopped reading
			}
		}
		_ = stdin.Close()
		for range p.chunks {
		}
	}()
	go func() {
		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "bmux: pipe-pane %q: %v\n", command, err)
		}
		close(p.exited)
		onExit(p)
	}()
	return p, nil
}

// write queues a copy of output, or drops it when the queue is full.
// Caller holds pane.mu and p is still the pane's pipe.
func (p *panePipe) write(output []byte) {
	select {
	case p.chunks <- append([]byte(nil), output...):
	default:
	}
}

// stop ends the pipe: the command reads end of file, and its process group
// is ended if it hasn't exited a few seconds later. p must already be
// cleared from its pane.
func (p *panePipe) stop() {
	p.once.Do(func() {
		close(p.chunks)
		go func() {
			select {
			case <-p.exited:
			case <-time.After(p.grace):
				_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
			}
		}()
	})
}

// pipePane starts piping pane t's output to command, or stops when command
// is "". With onlyIfNone (-o), a pane already piped stops instead of
// switching commands.
func (s *session) pipePane(t paneTarget, command string, onlyIfNone bool) error {
	s.mu.Lock()
	id, err := s.resolveLocked(t)
	p := s.panes[id]
	s.mu.Unlock()
	if err != nil {
		return err
	}
	p.mu.Lock()
	old := p.pipe
	p.pipe = nil
	p.mu.Unlock()
	if old != nil {
		old.stop()
		if onlyIfNone {
			return nil
		}
	}
	if command == "" {
		return nil
	}
	panes, err := s.describe(paneTarget{pane: id}, "pane")
	if err != nil || len(panes) != 1 {
		return fmt.Errorf("pane not found")
	}
	attach := func(pipe *panePipe) {
		p.mu.Lock()
		previous := p.pipe
		p.pipe = pipe
		p.mu.Unlock()
		if previous != nil {
			previous.stop() // started meanwhile by another pipe-pane
		}
	}
	_, err = startPipe(command, panes[0], attach, func(done *panePipe) {
		p.mu.Lock()
		if p.pipe == done {
			p.pipe = nil
		}
		p.mu.Unlock()
		done.stop()
	})
	return err
}
