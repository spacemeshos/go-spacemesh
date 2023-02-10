package wallet

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

const (
	// BaseGas is a cost of Parse and Verify methods.
	BaseGas = 100
	// FixedGasSpawn is consumed from principal in case of successful spawn.
	FixedGasSpawn = 100
	// FixedGasSpend is consumed from principal in case of successful spend.
	FixedGasSpend = 100
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
func (*handler) Parse(txtype core.TxType, method core.Method, decoder *scale.Decoder) (output core.ParseOutput, err error) {
	output.BaseGas = BaseGas
	switch txtype {
	case core.SelfSpawn, core.Spawn:
		output.FixedGas = FixedGasSpawn
	case core.LocalMethodCall:
		if method == core.MethodSpend {
			output.FixedGas = FixedGasSpend
		} else {
			return output, fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
		}
	default:
		return output, fmt.Errorf("%w: unknown txtype %d", core.ErrMalformed, txtype)
	}
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
func (*handler) Exec(host core.Host, txtype core.TxType, method core.Method, args scale.Encodable) error {
	switch txtype {
	case core.SelfSpawn, core.Spawn:
		return host.Spawn(args)
	case core.LocalMethodCall:
		if method == core.MethodSpend {
			return host.Template().(*Wallet).Spend(host, args.(*SpendArguments))
		}
	}
	return fmt.Errorf("%w: unknown txtype/method %d/%d", core.ErrMalformed, txtype, method)
}

// Args ...
func (h *handler) Args(txtype core.TxType, method core.Method) scale.Type {
	switch txtype {
	case core.SelfSpawn, core.Spawn:
		return &SpawnArguments{}
	case core.LocalMethodCall:
		if method == core.MethodSpend {
			return &SpendArguments{}
		}
	}
	return nil
}
