package registry

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

// New creates Registry instance.
func New() *Registry {
	return &Registry{templates: map[types.Address]core.Handler{}}
}

// Registry stores mapping from address to template handler.
type Registry struct {
	templates map[core.Address]core.Handler
}

// Get template handler for the address if it exists.
func (r *Registry) Get(address core.Address) core.Handler {
	return r.templates[address]
}

// Register handler for the address. Panics if address is already taken.
func (r *Registry) Register(address core.Address, handler core.Handler) {
	if _, exist := r.templates[address]; exist {
		panic(fmt.Sprintf("%x already register", address))
	}
	r.templates[address] = handler
}
