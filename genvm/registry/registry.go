package registry

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

var reg = registry{templates: map[core.Address]core.Handler{}}

// Get template handler for the address if it exists.
func Get(address core.Address) core.Handler {
	return reg.get(address)
}

// Register handler for the address. Panics if address is already taken.
func Register(address core.Address, api core.Handler) {
	reg.register(address, api)
}

type registry struct {
	templates map[core.Address]core.Handler
}

func (r *registry) get(address core.Address) core.Handler {
	return r.templates[address]
}

func (r *registry) register(address core.Address, handler core.Handler) {
	if _, exist := r.templates[address]; exist {
		panic(fmt.Sprintf("%x already register", address))
	}
	r.templates[address] = handler
}
