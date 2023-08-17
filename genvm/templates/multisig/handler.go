package multisig

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
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
		err = fmt.Errorf("%w: %w", core.ErrMalformed, err)
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
		return nil, fmt.Errorf("%w: malformed state %w", core.ErrInternal, err)
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
