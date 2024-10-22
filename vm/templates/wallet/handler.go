package wallet

import (
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/vm"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/registry"
)

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 1
}

// Register Wallet template.
func Register(registry *registry.Registry) {
	registry.Register(TemplateAddress, &handler{})
}

var (
	_ core.Handler = (*handler)(nil)
	// TemplateAddress is an address of the Wallet template.
	TemplateAddress core.Address
)

type handler struct{}

// Parse header.
func (*handler) Parse(decoder *scale.Decoder) (output core.ParseOutput, err error) {
	var p core.Payload
	if _, err = p.DecodeScale(decoder); err != nil {
		err = fmt.Errorf("%w: %w", core.ErrMalformed, err)
		return
	}
	output.GasPrice = p.GasPrice
	output.Nonce = p.Nonce
	return output, nil
}

// New instatiates single sig wallet with spawn arguments.
func (*handler) New(args any) (core.Template, error) {
	return New(args.(*SpawnArguments)), nil
}

// Load single sig wallet from stored state.
func (*handler) Load(state []byte) (core.Template, error) {
	// TODO(lane): pass blob into VM to instantiate the template instance (program)
	var wallet Wallet
	return &wallet, nil
}

// Pass the transaction into the VM for execution.
func (*handler) Exec(host core.Host, cache *core.StagedCache, payload []byte) error {
	// Construct the context
	staticContext := vm.StaticContext{}
	dynamicContext := vm.DynamicContext{}

	// Instantiate the VM
	vmhost, err := vm.NewHost(host, cache, cache, staticContext, dynamicContext)

	// TODO(lane): rewrite to use the VM
	return fmt.Errorf("%w: unknown method", core.ErrMalformed)
}

func (h *handler) IsSpawn(payload []byte) bool {
	// TODO(lane): rewrite to use the VM
	// mock for now
	return true
}

// Args ...
func (h *handler) Args(payload []byte) scale.Type {
	// TODO(lane): rewrite to use the VM
	// mock for now
	return &SpawnArguments{}
	// switch method {
	// case core.MethodSpawn:
	// 	return &SpawnArguments{}
	// case core.MethodSpend:
	// 	return &SpendArguments{}
	// }
	// return nil
}
