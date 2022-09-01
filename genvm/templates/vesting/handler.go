package vesting

import (
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
)

var (
	// TemplateAddress1 is an address for 1/n vesting wallet.
	TemplateAddress1 core.Address
	// TemplateAddress2 is an address for 2/n vesting wallet.
	TemplateAddress2 core.Address
	// TemplateAddress3 is an address for 3/n vesting wallet.
	TemplateAddress3 core.Address
)

const (
	TotalGasSpawn1 = multisig.TotalGasSpawn1
	TotalGasSpawn2 = multisig.TotalGasSpawn2
	TotalGasSpawn3 = multisig.TotalGasSpawn3

	TotalGasSpend1 = multisig.TotalGasSpend1
	TotalGasSpend2 = multisig.TotalGasSpend2
	TotalGasSpend3 = multisig.TotalGasSpend3
)

// MethodDrainVault is used to relay a call to drain a vault.
const MethodDrainVault = 17

// FixedGasDrainVault is a fixed gas for drain_vault method
const FixedGasDrainVault = 100

func init() {
	TemplateAddress1[len(TemplateAddress1)-1] = 5
	TemplateAddress2[len(TemplateAddress2)-1] = 6
	TemplateAddress3[len(TemplateAddress3)-1] = 7
}

// Register vesting templates.
func Register(reg *registry.Registry) {
	reg.Register(TemplateAddress1, &handler{
		multisig: multisig.NewHandler(TemplateAddress1, 1, multisig.TotalGasSpawn1, multisig.TotalGasSpend1),
	})
	reg.Register(TemplateAddress2, &handler{
		multisig: multisig.NewHandler(TemplateAddress2, 2, multisig.TotalGasSpawn2, multisig.TotalGasSpend2),
	})
	reg.Register(TemplateAddress3, &handler{
		multisig: multisig.NewHandler(TemplateAddress3, 3, multisig.TotalGasSpawn3, multisig.TotalGasSpend3),
	})
}

type handler struct {
	multisig core.Handler
}

// Parse header and arguments.
func (h *handler) Parse(host core.Host, method uint8, decoder *scale.Decoder) (output core.ParseOutput, err error) {
	if method == MethodDrainVault {
		var p DrainVaultPayload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		output.GasPrice = p.GasPrice
		output.Nonce = p.Nonce
		output.FixedGas = FixedGasDrainVault
		return output, nil
	}
	return h.multisig.Parse(host, method, decoder)
}

// New instatiates vesting state, note that the state is the same as multisig.
// The difference is that vesting supports one more transaction type.
func (h *handler) New(args any) (core.Template, error) {
	instance, err := h.multisig.New(args)
	if err != nil {
		return nil, err
	}
	return &Vesting{Template: instance}, nil
}

// Load instnatiates vesting state from stored state. See comment on New.
func (h *handler) Load(state []byte) (core.Template, error) {
	instance, err := h.multisig.Load(state)
	if err != nil {
		return nil, err
	}
	return &Vesting{Template: instance}, nil
}

// Exec spawn or spend based on the method selector.
func (h *handler) Exec(host core.Host, method uint8, args scale.Encodable) error {
	if method == MethodDrainVault {
		drain := args.(*DrainVaultArguments)
		return host.Relay(vault.TemplateAddress, drain.Vault, func(host core.Host) error {
			return host.Handler().Exec(host, vault.MethodSpend, &vault.SpendArguments{
				Destination: drain.Receiver,
				Amount:      drain.Amount,
			})
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
