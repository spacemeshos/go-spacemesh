package wallet

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

const (
	gasParse = 10
	gasSpawn = 100
	gasSpend = 50
)

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 1
	registry.Register(TemplateAddress, &api{})
}

var (
	_               (core.Handler) = (*api)(nil)
	TemplateAddress core.Address
)

type api struct{}

func (a *api) Parse(ctx *core.Context, method uint8, decoder *scale.Decoder) (header core.Header, args scale.Encodable) {
	// TODO rethink cost approach
	header.MaxGas += gasParse
	switch method {
	case 0:
		var p SpawnPayload
		if _, err := p.DecodeScale(decoder); err != nil {
			ctx.Fail(errors.New("invalid tx"))
		}
		args = &p.Arguments
		header.GasPrice = uint64(p.GasPrice)
		header.MaxGas += gasSpawn
	case 1:
		var p SpendPayload
		if _, err := p.DecodeScale(decoder); err != nil {
			ctx.Fail(errors.New("invalid tx"))
		}
		args = &p.Arguments
		header.GasPrice = uint64(p.GasPrice)
		header.Nonce.Counter = p.Nonce.Counter
		header.Nonce.Bitfield = p.Nonce.Bitfield
		header.MaxGas += gasSpend
	}
	ctx.Header = header
	ctx.Args = args
	ctx.Consume(gasParse)
	return header, args
}

func (a *api) Init(method uint8, args any, imu []byte) (core.Template, error) {
	if method == 0 {
		return New(args.(*SpawnArguments)), nil
	}
	// TODO i dont like that it needs to initialize decoder here
	// what can be done about it?
	decoder := scale.NewDecoder(bytes.NewReader(imu))
	var wallet Wallet
	if _, err := wallet.DecodeScale(decoder); err != nil {
		return nil, fmt.Errorf("malformed state %w", err)
	}
	return &wallet, nil
}

func (a *api) Exec(ctx *core.Context, method uint8, args any) {
	switch method {
	case 0:
		ctx.Consume(gasSpawn)
		ctx.Spawn()
	case 1:
		ctx.Consume(gasSpend)
		ctx.Template.(*Wallet).Spend(ctx, args.(*Arguments))
	default:
		// TODO change it to propagate errors without throws
		ctx.Fail(errors.New("unkown selector"))
	}
}
