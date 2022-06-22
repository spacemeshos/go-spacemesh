package wallet

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

const (
	// TotalGasSpawn is consumed from principal in case of successful spawn.
	TotalGasSpawn = 200
	// TotalGasSpend is consumed from principal in case of successful spend.
	TotalGasSpend = 100
)

const (
	methodSpawn = 0
	methodSpend = 1
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
func (*handler) Parse(ctx *core.Context, method uint8, decoder *scale.Decoder) (header core.Header, args scale.Encodable, err error) {
	switch method {
	case methodSpawn:
		var p SpawnPayload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		args = &p.Arguments
		header.GasPrice = p.GasPrice
		header.MaxGas = TotalGasSpawn
	case methodSpend:
		var p SpendPayload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		args = &p.Arguments
		header.GasPrice = p.GasPrice
		header.Nonce.Counter = p.Nonce.Counter
		header.Nonce.Bitfield = p.Nonce.Bitfield
		header.MaxGas = TotalGasSpend
	default:
		return header, args, fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	return header, args, nil
}

// Init wallet.
func (*handler) Init(method uint8, args any, state []byte) (core.Template, error) {
	if method == 0 {
		return New(args.(*SpawnArguments)), nil
	}
	decoder := scale.NewDecoder(bytes.NewReader(state))
	var wallet Wallet
	if _, err := wallet.DecodeScale(decoder); err != nil {
		return nil, fmt.Errorf("%w: malformed state %s", core.ErrInternal, err.Error())
	}
	return &wallet, nil
}

// Exec spawn or spend based on the method selector.
func (*handler) Exec(ctx *core.Context, method uint8, args scale.Encodable) error {
	switch method {
	case methodSpawn:
		if err := ctx.Consume(TotalGasSpawn); err != nil {
			return err
		}
		if err := ctx.Spawn(TemplateAddress, args); err != nil {
			return err
		}
	case methodSpend:
		if err := ctx.Consume(TotalGasSpend); err != nil {
			return err
		}
		if err := ctx.Template.(*Wallet).Spend(ctx, args.(*SpendArguments)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	return nil
}
