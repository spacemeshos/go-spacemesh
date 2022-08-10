package multisig

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

const (
	// TotalGasSpawn23 is consumed from principal in case of successful spawn.
	TotalGasSpawn23 = 200
	// TotalGasSpend23 is consumed from principal in case of successful spend.
	TotalGasSpend23 = 100

	// TotalGasSpawn35 is consumed from principal in case of successful spawn.
	TotalGasSpawn35 = 300
	// TotalGasSpend35 is consumed from principal in case of successful spend.
	TotalGasSpend35 = 150

	keysLimit = 10
)

const (
	methodSpawn = 0
	methodSpend = 1
)

func init() {
	TemplateAddress23[len(TemplateAddress23)-1] = 2
	TemplateAddress35[len(TemplateAddress35)-1] = 3
}

// Register template.
func Register(registry *registry.Registry) {
	registry.Register(TemplateAddress23, &handler{
		k: 2, address: TemplateAddress23,
		totalGasSpawn: TotalGasSpawn23,
		totalGasSpend: TotalGasSpend23,
	})
	registry.Register(TemplateAddress35, &handler{
		k: 3, address: TemplateAddress35,
		totalGasSpawn: TotalGasSpawn35,
		totalGasSpend: TotalGasSpend35,
	})
}

var (
	_ (core.Handler) = (*handler)(nil)
	// TemplateAddress23 is an address of the 2/3 multisig template.
	TemplateAddress23 core.Address
	// TemplateAddress35 is an address of the 3/5 multisig template.
	TemplateAddress35 core.Address
)

type handler struct {
	k, n                         uint8
	address                      core.Address
	totalGasSpawn, totalGasSpend uint64
}

// Parse header and arguments.
func (h *handler) Parse(ctx *core.Context, method uint8, decoder *scale.Decoder) (header core.Header, args scale.Encodable, err error) {
	switch method {
	case methodSpawn:
		var p SpawnPayload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		args = &p.Arguments
		header.GasPrice = p.GasPrice
		header.MaxGas = h.totalGasSpawn
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
		header.MaxGas = h.totalGasSpend
	default:
		return header, args, fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	return header, args, nil
}

// Init wallet.
func (h *handler) Init(method uint8, args any, state []byte) (core.Template, error) {
	if method == 0 {
		return &MultiSig{
			PublicKeys: args.(*SpawnArguments).PublicKeys,
			k:          h.k,
		}, nil
	}
	decoder := scale.NewDecoder(bytes.NewReader(state))
	ms := MultiSig{k: h.k}
	if _, err := ms.DecodeScale(decoder); err != nil {
		return nil, fmt.Errorf("%w: malformed state %s", core.ErrInternal, err.Error())
	}
	return &ms, nil
}

// Exec spawn or spend based on the method selector.
func (h *handler) Exec(ctx *core.Context, method uint8, args scale.Encodable) error {
	switch method {
	case methodSpawn:
		if err := ctx.Consume(h.totalGasSpawn); err != nil {
			return err
		}
		if len(args.(*SpawnArguments).PublicKeys) > keysLimit {
			return fmt.Errorf("multisig supports atmost %d key", keysLimit)
		}
		if err := ctx.Spawn(h.address, args); err != nil {
			return err
		}
	case methodSpend:
		if err := ctx.Consume(h.totalGasSpend); err != nil {
			return err
		}
		if err := ctx.Template.(*MultiSig).Spend(ctx, args.(*SpendArguments)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	return nil
}
