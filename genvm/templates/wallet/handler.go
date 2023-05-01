package wallet

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 1
}

// Register Wallet template.
func Register(registry *registry.Registry) {
	registry.Register(TemplateAddress, &handler{})
}

var (
	_ (core.Handler) = (*handler)(nil)
	// TemplateAddress is an address of the Wallet template.
	TemplateAddress core.Address
)

type handler struct{}

// Parse header and arguments.
func (*handler) Parse(host core.Host, method uint8, decoder *scale.Decoder) (output core.ParseOutput, err error) {
	var p core.Payload
	if _, err = p.DecodeScale(decoder); err != nil {
		err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
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
	decoder := scale.NewDecoder(bytes.NewReader(state))
	var wallet Wallet
	if _, err := wallet.DecodeScale(decoder); err != nil {
		return nil, fmt.Errorf("%w: malformed state %s", core.ErrInternal, err.Error())
	}
	return &wallet, nil
}

// Exec spawn or spend based on the method selector.
func (*handler) Exec(host core.Host, method uint8, args scale.Encodable) error {
	switch method {
	case core.MethodSpawn:
		if err := host.Spawn(args); err != nil {
			return err
		}
	case core.MethodSpend:
		if err := host.Template().(*Wallet).Spend(host, args.(*SpendArguments)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	return nil
}

// Args ...
func (h *handler) Args(method uint8) scale.Type {
	switch method {
	case core.MethodSpawn:
		return &SpawnArguments{}
	case core.MethodSpend:
		return &SpendArguments{}
	}
	return nil
}
