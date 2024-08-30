package vm

import (
	"fmt"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
)

type Host struct {
	vm *athcon.VM
}

// Load the VM from the shared library and returns an instance of a Host.
// It is the caller's responsibility to call Destroy when it
// is no longer needed.
func NewHost(path string) (*Host, error) {
	vm, err := athcon.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading Athena VM: %w", err)
	}
	return &Host{vm}, nil
}

func (h *Host) Destroy() {
	h.vm.Destroy()
}
