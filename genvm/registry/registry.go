package registry

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

var (
	registry = Registry{templates: map[core.Address]core.Handler{}}
)

func Get(address core.Address) core.Handler {
	return registry.Get(address)
}

func Register(address core.Address, api core.Handler) {
	registry.Register(address, api)
}

type Registry struct {
	templates map[core.Address]core.Handler
}

func (r *Registry) Get(address core.Address) core.Handler {
	return r.templates[address]
}

func (r *Registry) Register(address core.Address, api core.Handler) {
	if _, exist := r.templates[address]; exist {
		panic(fmt.Sprintf("%x already register", address))
	}
	r.templates[address] = api
}
