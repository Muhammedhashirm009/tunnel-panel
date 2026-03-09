package terminal

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
)

// Session holds a PTY session
type Session struct {
	PTY *os.File
	Cmd *exec.Cmd
}

// SpawnShell forks /bin/bash into a PTY and returns the session
func SpawnShell() (*Session, error) {
	cmd := exec.Command("/bin/bash", "-l")
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HISTCONTROL=ignoredups",
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	return &Session{
		PTY: ptmx,
		Cmd: cmd,
	}, nil
}

// Resize resizes the PTY to the given dimensions
func (s *Session) Resize(rows, cols uint16) error {
	ws := &winsize{
		Row: rows,
		Col: cols,
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		s.PTY.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(ws)),
	)
	if errno != 0 {
		return errno
	}
	return nil
}

// Close terminates the PTY session
func (s *Session) Close() {
	s.PTY.Close()
	if s.Cmd.Process != nil {
		s.Cmd.Process.Kill()
	}
}

// winsize matches sys/ioctl.h struct winsize
type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}
