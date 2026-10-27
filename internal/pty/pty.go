package pty

import (
	"errors"

	gopty "github.com/aymanbagabas/go-pty"
)

// Size is the terminal window size in character cells.
type Size struct {
	Rows uint16
	Cols uint16
}

// Spec describes the managed process to start on a pseudo terminal.
type Spec struct {
	Command string
	Args    []string
	Dir     string
	Size    Size
}

// Exit reports how the managed process ended.
type Exit struct {
	Code   int
	Signal string
}

// Process is one managed process attached to a pseudo terminal.
type Process interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(Size) error
	PID() int
	Wait() (Exit, error)
	Terminate() error
	Close() error
}

type process struct {
	terminal gopty.Pty
	cmd      *gopty.Cmd
}

// Start launches the process on a new pseudo terminal. The caller owns the
// returned process and must call Wait and Close.
func Start(spec Spec) (Process, error) {
	if spec.Command == "" {
		return nil, errors.New("pty: empty command")
	}
	terminal, err := gopty.New()
	if err != nil {
		return nil, err
	}
	if spec.Size.Rows > 0 && spec.Size.Cols > 0 {
		if err := terminal.Resize(int(spec.Size.Cols), int(spec.Size.Rows)); err != nil {
			_ = terminal.Close()
			return nil, err
		}
	}
	cmd := terminal.Command(spec.Command, spec.Args...)
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	if err := cmd.Start(); err != nil {
		_ = terminal.Close()
		return nil, err
	}
	return &process{terminal: terminal, cmd: cmd}, nil
}

func (p *process) Read(buffer []byte) (int, error)  { return p.terminal.Read(buffer) }
func (p *process) Write(buffer []byte) (int, error) { return p.terminal.Write(buffer) }
func (p *process) Resize(size Size) error {
	return p.terminal.Resize(int(size.Cols), int(size.Rows))
}
func (p *process) PID() int {
	if p.cmd.Process == nil {
		return -1
	}
	return p.cmd.Process.Pid
}

func (p *process) Wait() (Exit, error) {
	err := p.cmd.Wait()
	if state := p.cmd.ProcessState; state != nil {
		exit := Exit{Code: state.ExitCode()}
		if signaled, ok := state.Sys().(interface {
			Signaled() bool
			String() string
		}); ok && signaled.Signaled() {
			exit.Signal = signaled.String()
		}
		return exit, nil
	}
	return Exit{Code: -1}, err
}

func (p *process) Terminate() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *process) Close() error { return p.terminal.Close() }
