package core

import (
	"fmt"

	"github.com/spacemeshos/go-scale"
)

var (
	registry = Registry{templates: map[scale.Bytes20]TemplateAPI{}}
)

func Get(address Address) TemplateAPI {
	return registry.Get(address)
}

func Register(address Address, api TemplateAPI) {
	registry.Register(address, api)
}

type Registry struct {
	templates map[Address]TemplateAPI
}

func (r *Registry) Get(address Address) TemplateAPI {
	return r.templates[address]
}

func (r *Registry) Register(address Address, api TemplateAPI) {
	if _, exist := r.templates[address]; exist {
		panic(fmt.Sprintf("%x already register", address))
	}
	r.templates[address] = api
}
