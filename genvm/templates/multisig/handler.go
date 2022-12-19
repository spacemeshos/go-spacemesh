package multisig

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

const (
	// BaseGas is a cost of Parse and Verify methods.
	BaseGas1 = 100
	BaseGas2 = 200
	BaseGas3 = 300

	// FixedGasSpawn1 is consumed from principal in case of successful spawn.
	FixedGasSpawn1 = 100
	// FixedGasSpend1 is consumed from principal in case of successful spend.
	FixedGasSpend1 = 100

	// FixedGasSpawn2 is consumed from principal in case of successful spawn.
	FixedGasSpawn2 = 200
	// FixedGasSpend2 is consumed from principal in case of successful spend.
	FixedGasSpend2 = 200

	// FixedGasSpawn3 is consumed from principal in case of successful spawn.
	FixedGasSpawn3 = 300
	// FixedGasSpend3 is consumed from principal in case of successful spend.
	FixedGasSpend3 = 300

	// StorageLimit is a limit of keys that can be used when multisig is spawned.
	StorageLimit = 10
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
		baseGas:       BaseGas1,
		totalGasSpawn: FixedGasSpawn1,
		totalGasSpend: FixedGasSpend1,
	})
	registry.Register(TemplateAddress2, &handler{
		k: 2, address: TemplateAddress2,
		baseGas:       BaseGas2,
		totalGasSpawn: FixedGasSpawn2,
		totalGasSpend: FixedGasSpend2,
	})
	registry.Register(TemplateAddress3, &handler{
		k: 3, address: TemplateAddress3,
		baseGas:       BaseGas3,
		totalGasSpawn: FixedGasSpawn3,
		totalGasSpend: FixedGasSpend3,
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

// NewHandler instantiates multisig handler with a particular configuration.
func NewHandler(address core.Address, k uint8, baseGas, gasSpawn, gasSpend uint64) core.Handler {
	return &handler{k: k, address: address, baseGas: baseGas, totalGasSpawn: gasSpawn, totalGasSpend: gasSpend}
}

type handler struct {
	k                                     uint8
	address                               core.Address
	baseGas, totalGasSpawn, totalGasSpend uint64
}

// Parse header and arguments.
func (h *handler) Parse(host core.Host, method uint8, decoder *scale.Decoder) (output core.ParseOutput, err error) {
	switch method {
	case core.MethodSpawn:
		output.FixedGas = h.totalGasSpawn
	case core.MethodSpend:
		output.FixedGas = h.totalGasSpend
	default:
		return output, fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	var p core.Payload
	if _, err = p.DecodeScale(decoder); err != nil {
		err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
		return
	}
	output.BaseGas = h.baseGas
	output.GasPrice = p.GasPrice
	output.Nonce = p.Nonce
	return output, nil
}

// New instantiates k-multisig instance.
func (h *handler) New(args any) (core.Template, error) {
	n := len(args.(*SpawnArguments).PublicKeys)
	if n < int(h.k) {
		return nil, fmt.Errorf("multisig requires atleast %d keys", h.k)
	}
	if n > StorageLimit {
		return nil, fmt.Errorf("multisig supports atmost %d keys", StorageLimit)
	}
	return &MultiSig{
		PublicKeys: args.(*SpawnArguments).PublicKeys,
		k:          h.k,
	}, nil
}

// Load k-multisig instance from stored state.
func (h *handler) Load(state []byte) (core.Template, error) {
	decoder := scale.NewDecoder(bytes.NewReader(state))
	ms := MultiSig{k: h.k}
	if _, err := ms.DecodeScale(decoder); err != nil {
		return nil, fmt.Errorf("%w: malformed state %s", core.ErrInternal, err.Error())
	}
	return &ms, nil
}

// Exec spawn or spend based on the method selector.
func (h *handler) Exec(host core.Host, method uint8, args scale.Encodable) error {
	switch method {
	case core.MethodSpawn:
		if err := host.Spawn(args); err != nil {
			return err
		}
	case core.MethodSpend:
		if err := host.Template().(SpendTemplate).Spend(host, args.(*SpendArguments)); err != nil {
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

// SpendTemplate interface for the template that support Spend method.
type SpendTemplate interface {
	Spend(core.Host, *SpendArguments) error
}
