package wallet

import (
	"fmt"

	"github.com/spacemeshos/go-scale"

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

// Parse header and arguments.
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

// Exec spawn or spend based on the method selector.
func (*handler) Exec(host core.Host, args []byte) error {
	// TODO(lane): rewrite to use the VM
	// switch method {
	// case core.MethodSpawn:
	// 	if err := host.Spawn(args); err != nil {
	// 		return err
	// 	}
	// case core.MethodSpend:
	// 	if err := host.Template().(*Wallet).Spend(host, args.(*SpendArguments)); err != nil {
	// 		return err
	// 	}
	// default:
	return fmt.Errorf("%w: unknown method", core.ErrMalformed)
	// }
	// return nil
}

// Args ...
func (h *handler) Args() scale.Type {
	// switch method {
	// case core.MethodSpawn:
	// 	return &SpawnArguments{}
	// case core.MethodSpend:
	// 	return &SpendArguments{}
	// }
	return nil
}
