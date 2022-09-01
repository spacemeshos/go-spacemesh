package multisig

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

const (
	// TotalGasSpawn1 is consumed from principal in case of successful spawn.
	TotalGasSpawn1 = 100
	// TotalGasSpend1 is consumed from principal in case of successful spend.
	TotalGasSpend1 = 100

	// TotalGasSpawn2 is consumed from principal in case of successful spawn.
	TotalGasSpawn2 = 200
	// TotalGasSpend2 is consumed from principal in case of successful spend.
	TotalGasSpend2 = 200

	// TotalGasSpawn3 is consumed from principal in case of successful spawn.
	TotalGasSpawn3 = 300
	// TotalGasSpend3 is consumed from principal in case of successful spend.
	TotalGasSpend3 = 300

	// StorageLimit is a limit of keys that can be used when multisig is spawned.
	StorageLimit = 10
)

const (
	methodSpawn = 0
	methodSpend = 1
)

func init() {
	TemplateAddress1[len(TemplateAddress1)-1] = 2
	TemplateAddress2[len(TemplateAddress2)-1] = 3
	TemplateAddress3[len(TemplateAddress3)-1] = 4
}

// Register template.
func Register(registry *registry.Registry) {
	registry.Register(TemplateAddress1, &handler{
		k: 1, address: TemplateAddress1,
		totalGasSpawn: TotalGasSpawn1,
		totalGasSpend: TotalGasSpend1,
	})
	registry.Register(TemplateAddress2, &handler{
		k: 2, address: TemplateAddress2,
		totalGasSpawn: TotalGasSpawn2,
		totalGasSpend: TotalGasSpend2,
	})
	registry.Register(TemplateAddress3, &handler{
		k: 3, address: TemplateAddress3,
		totalGasSpawn: TotalGasSpawn3,
		totalGasSpend: TotalGasSpend3,
	})
}

var (
	_ (core.Handler) = (*handler)(nil)
	// TemplateAddress1 is an address of the 1/N multisig template.
	TemplateAddress1 core.Address
	// TemplateAddress2 is an address of the 2/N multisig template.
	TemplateAddress2 core.Address
	// TemplateAddress3 is an address of the 3/N multisig template.
	TemplateAddress3 core.Address
)

type handler struct {
	k, n                         uint8
	address                      core.Address
	totalGasSpawn, totalGasSpend uint64
}

// Parse header and arguments.
func (h *handler) Parse(ctx *core.Context, method uint8, decoder *scale.Decoder) (out core.ParseOutput, args scale.Encodable, err error) {
	switch method {
	case methodSpawn:
		var p SpawnPayload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		args = &p.Arguments
		out.GasPrice = p.GasPrice
		out.Nonce = p.Nonce
		out.FixedGas = h.totalGasSpawn
	case methodSpend:
		var p SpendPayload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		args = &p.Arguments
		out.GasPrice = p.GasPrice
		out.Nonce.Counter = p.Nonce.Counter
		out.Nonce.Bitfield = p.Nonce.Bitfield
		out.FixedGas = h.totalGasSpend
	default:
		return out, args, fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	return out, args, nil
}

// Init wallet.
func (h *handler) Init(method uint8, args any, state []byte) (core.Template, error) {
	if method == methodSpawn {
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
		n := len(args.(*SpawnArguments).PublicKeys)
		if n < int(h.k) {
			return fmt.Errorf("multisig requires atleast %d keys", h.k)
		}
		if n > StorageLimit {
			return fmt.Errorf("multisig supports atmost %d keys", StorageLimit)
		}
		if err := ctx.Spawn(h.address, args); err != nil {
			return err
		}
	case methodSpend:
		if err := ctx.Template.(*MultiSig).Spend(ctx, args.(*SpendArguments)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	return nil
}
