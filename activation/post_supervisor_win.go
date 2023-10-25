//go:build windows

package activation

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DefaultPostServiceName is the default name of the post service executable.
const DefaultPostServiceName = "service.exe"

// process is used to retrieve process information from the handle in an unsafe way.
type process struct {
	Pid    int
	Handle uintptr
}

// ProcessExitGroup holds references to processes that should be closed when the main process exits.
type ProcessExitGroup windows.Handle

// NewProcessExitGroup returns a new ProcessExitGroup.
func NewProcessExitGroup() (ProcessExitGroup, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info))); err != nil {
		return 0, err
	}

	return ProcessExitGroup(handle), nil
}

// Dispose closes the ProcessExitGroup.
func (g ProcessExitGroup) Dispose() error {
	return windows.CloseHandle(windows.Handle(g))
}

// StartCommand starts the given command and adds it to the ProcessExitGroup.
func (g ProcessExitGroup) StartCommand(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start post service: %w", err)
	}
	return windows.AssignProcessToJobObject(
		windows.Handle(g),
		windows.Handle((*process)(unsafe.Pointer(cmd.Process)).Handle),
	)
}
