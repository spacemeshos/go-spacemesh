//go:build !windows

package activation

import (
	"os"
	"os/exec"
	"syscall"
)

// DefaultPostServiceName is the default name of the post service executable.
const DefaultPostServiceName = "service"

// ProcessExitGroup holds references to processes that should be closed when the main process exits.
type ProcessExitGroup struct{}

// NewProcessExitGroup returns a new ProcessExitGroup.
func NewProcessExitGroup() (ProcessExitGroup, error) {
	return ProcessExitGroup{}, nil
}

// Dispose closes the ProcessExitGroup.
func (g ProcessExitGroup) Dispose() error {
	return nil
}

// StartCommand starts the given command and adds it to the ProcessExitGroup.
func (g ProcessExitGroup) StartCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: os.Getpid()}
	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
}
