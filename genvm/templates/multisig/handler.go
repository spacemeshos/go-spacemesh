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
	BaseGas = 100
	// FixedGas is a cost to execute transaction.
	FixedGas = 100

	// StorageLimit is a limit of keys that can be used when multisig is spawned.
	StorageLimit = 10
)

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 2
}

// Register template.
func Register(registry *registry.Registry) {
	registry.Register(TemplateAddress, &handler{
		address: TemplateAddress,
	})
}

var (
	_               (core.Handler) = (*handler)(nil)
	TemplateAddress core.Address
)

// NewHandler instantiates multisig handler with a particular configuration.
func NewHandler(address core.Address) core.Handler {
	return &handler{address: address}
}

type handler struct {
	address core.Address
}

// Parse header and arguments.
func (h *handler) Parse(host core.Host, method uint8, decoder *scale.Decoder) (output core.ParseOutput, err error) {
	var p core.Payload
	if _, err = p.DecodeScale(decoder); err != nil {
		err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
		return
	}
	output.GasPrice = p.GasPrice
	output.Nonce = p.Nonce
	return output, nil
}

// New instantiates k-multisig instance.
func (h *handler) New(args any) (core.Template, error) {
	spawn := args.(*SpawnArguments)
	if spawn.Required == 0 {
		return nil, fmt.Errorf("number of required signatures must be larger than zero")
	}
	if len(spawn.PublicKeys) < int(spawn.Required) {
		return nil, fmt.Errorf("multisig requires atleast %d keys", spawn.Required)
	}
	if len(spawn.PublicKeys) > StorageLimit {
		return nil, fmt.Errorf("multisig supports atmost %d keys", StorageLimit)
	}
	return &MultiSig{
		PublicKeys: spawn.PublicKeys,
		Required:   spawn.Required,
	}, nil
}

// Load k-multisig instance from stored state.
func (h *handler) Load(state []byte) (core.Template, error) {
	decoder := scale.NewDecoder(bytes.NewReader(state))
	var ms MultiSig
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
