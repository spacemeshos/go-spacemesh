package vesting

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
)

var TemplateAddress core.Address

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 3
}

// MethodDrainVault is used to relay a call to drain a vault.
const MethodDrainVault = 17

// Register vesting templates.
func Register(reg *registry.Registry) {
	reg.Register(TemplateAddress, &handler{
		multisig: multisig.NewHandler(TemplateAddress),
	})
}

type handler struct {
	multisig core.Handler
}

// Parse header and arguments.
func (h *handler) Parse(host core.Host, method uint8, decoder *scale.Decoder) (output core.ParseOutput, err error) {
	return h.multisig.Parse(host, method, decoder)
}

// New instatiates vesting state, note that the state is the same as multisig.
// The difference is that vesting supports one more transaction type.
func (h *handler) New(args any) (core.Template, error) {
	template, err := h.multisig.New(args)
	if err != nil {
		return nil, err
	}
	return &Vesting{MultiSig: template.(*multisig.MultiSig)}, nil
}

// Load instnatiates vesting state from stored state. See comment on New.
func (h *handler) Load(state []byte) (core.Template, error) {
	template, err := h.multisig.Load(state)
	if err != nil {
		return nil, err
	}
	return &Vesting{MultiSig: template.(*multisig.MultiSig)}, nil
}

// Exec spawn or spend based on the method selector.
func (h *handler) Exec(host core.Host, method uint8, args scale.Encodable) error {
	if method == MethodDrainVault {
		drain := args.(*DrainVaultArguments)
		return host.Relay(vault.TemplateAddress, drain.Vault, func(host core.Host) error {
			return host.Handler().Exec(host, core.MethodSpend, &drain.SpendArguments)
		})
	}
	return h.multisig.Exec(host, method, args)
}

// Args ...
func (h *handler) Args(method uint8) scale.Type {
	if method == MethodDrainVault {
		return &DrainVaultArguments{}
	}
	return h.multisig.Args(method)
}
