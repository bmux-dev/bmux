package server

import (
	"golang.org/x/sys/unix"
	"os"
	"time"
)

// Rewrap an O_NONBLOCK descriptor so os.NewFile registers it with Go's
// poller. Merely changing flags on an already-wrapped File is insufficient.
// Requiring deadlines proves Close can wake concurrent Read and Write.
func pollablePTY(f *os.File) (*os.File, error) {
	fd, err := unix.Dup(int(f.Fd()))
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	if err = unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	wrapped := os.NewFile(uintptr(fd), f.Name())
	if err = wrapped.SetDeadline(time.Time{}); err != nil {
		_ = wrapped.Close()
		return nil, err
	}
	_ = f.Close()
	return wrapped, nil
}

// resizePTY sets the window size; cellWidth and cellHeight, when known, fill
// the pixel fields programs read with TIOCGWINSZ.
func resizePTY(f *os.File, width, height, cellWidth, cellHeight int) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	err = conn.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{
			Col: uint16(width), Row: uint16(height),
			Xpixel: pixels(width, cellWidth), Ypixel: pixels(height, cellHeight),
		})
	})
	if err != nil {
		return err
	}
	return ioctlErr
}

// pty.Getsize, like pty.Setsize, calls File.Fd and disables poll integration.
func sizePTY(f *os.File) (rows, cols int, err error) {
	conn, err := f.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var size *unix.Winsize
	var ioctlErr error
	err = conn.Control(func(fd uintptr) { size, ioctlErr = unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ) })
	if err != nil {
		return 0, 0, err
	}
	if ioctlErr != nil {
		return 0, 0, ioctlErr
	}
	return int(size.Row), int(size.Col), nil
}

func foregroundPTYGroup(f *os.File) (int, error) {
	conn, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}
	var group int
	var ioctlErr error
	err = conn.Control(func(fd uintptr) { group, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) })
	if err != nil {
		return 0, err
	}
	return group, ioctlErr
}

func pixels(cells, cell int) uint16 {
	return uint16(max(0, min(cells*cell, 1<<16-1)))
}
