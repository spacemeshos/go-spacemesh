package wallet

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

const (
	gasParse = 10
	gasSpawn = 100
	gasSpend = 50

	// TotalGasSpawn is consumed from principal in case of succesful spawn.
	TotalGasSpawn = gasParse + gasSpawn
	// TotalGasSpend is consumed from principal in case of succesful spend.
	TotalGasSpend = gasParse + gasSpend
)

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 1
	registry.Register(TemplateAddress, &handler{})
}

var (
	_               (core.Handler) = (*handler)(nil)
	TemplateAddress core.Address
)

type handler struct{}

// Parse header and arguments.
func (*handler) Parse(ctx *core.Context, method uint8, decoder *scale.Decoder) (header core.Header, args scale.Encodable, err error) {
	header.MaxGas += gasParse
	switch method {
	case 0:
		var p SpawnPayload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		args = &p.Arguments
		header.GasPrice = uint64(p.GasPrice)
		header.MaxGas += gasSpawn
	case 1:
		var p SpendPayload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		args = &p.Arguments
		header.GasPrice = uint64(p.GasPrice)
		header.Nonce.Counter = p.Nonce.Counter
		header.Nonce.Bitfield = p.Nonce.Bitfield
		header.MaxGas += gasSpend
	}
	// TODO(dshulyak) need to implement it in a way that allows
	// to validate using gas from conservative cache only
	ctx.Header = header
	if err = ctx.Consume(gasParse); err != nil {
		return
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
	case 0:
		if err := ctx.Consume(gasSpawn); err != nil {
			return err
		}
		if err := ctx.Spawn(TemplateAddress, args); err != nil {
			return err
		}
	case 1:
		if err := ctx.Consume(gasSpend); err != nil {
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
