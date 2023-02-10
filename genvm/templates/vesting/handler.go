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
	// BaseGas is a cost of Parse and Verify methods.
	BaseGas1 = multisig.BaseGas1
	BaseGas2 = multisig.BaseGas1
	BaseGas3 = multisig.BaseGas1

	FixedGasSpawn1 = multisig.FixedGasSpawn1
	FixedGasSpawn2 = multisig.FixedGasSpawn2
	FixedGasSpawn3 = multisig.FixedGasSpawn3

	FixedGasSpend1 = multisig.FixedGasSpend1
	FixedGasSpend2 = multisig.FixedGasSpend2
	FixedGasSpend3 = multisig.FixedGasSpend3

	FixedGasDrainVault1 = 100
	FixedGasDrainVault2 = 200
	FixedGasDrainVault3 = 300
)

// MethodDrainVault is used to relay a call to drain a vault.
const MethodDrainVault = 17

func init() {
	TemplateAddress1[len(TemplateAddress1)-1] = 5
	TemplateAddress2[len(TemplateAddress2)-1] = 6
	TemplateAddress3[len(TemplateAddress3)-1] = 7
}

// Register vesting templates.
func Register(reg *registry.Registry) {
	reg.Register(TemplateAddress1, &handler{
		multisig:      multisig.NewHandler(TemplateAddress1, 1, BaseGas1, FixedGasSpawn1, FixedGasSpend1),
		baseGas:       BaseGas1,
		drainVaultGas: FixedGasDrainVault1,
	})
	reg.Register(TemplateAddress2, &handler{
		multisig:      multisig.NewHandler(TemplateAddress2, 2, BaseGas2, FixedGasSpawn2, FixedGasSpend2),
		baseGas:       BaseGas2,
		drainVaultGas: FixedGasDrainVault2,
	})
	reg.Register(TemplateAddress3, &handler{
		multisig:      multisig.NewHandler(TemplateAddress3, 3, BaseGas3, FixedGasSpawn3, FixedGasSpend3),
		baseGas:       BaseGas3,
		drainVaultGas: FixedGasDrainVault3,
	})
}

type handler struct {
	multisig               core.Handler
	baseGas, drainVaultGas uint64
}

// Parse header and arguments.
func (h *handler) Parse(txtype core.TxType, method core.Method, decoder *scale.Decoder) (output core.ParseOutput, err error) {
	if txtype == core.LocalMethodCall && method == MethodDrainVault {
		var p core.Payload
		if _, err = p.DecodeScale(decoder); err != nil {
			err = fmt.Errorf("%w: %s", core.ErrMalformed, err.Error())
			return
		}
		output.GasPrice = p.GasPrice
		output.Nonce = p.Nonce
		output.FixedGas = h.drainVaultGas
		return output, nil
	}
	return h.multisig.Parse(txtype, method, decoder)
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
func (h *handler) Exec(host core.Host, txtype core.TxType, method core.Method, args scale.Encodable) error {
	if txtype == core.LocalMethodCall && method == MethodDrainVault {
		drain := args.(*DrainVaultArguments)
		return host.Relay(vault.TemplateAddress, drain.Vault, func(host core.Host) error {
			return host.Handler().Exec(host, txtype, core.MethodSpend, &drain.SpendArguments)
		})
	}
	return h.multisig.Exec(host, txtype, method, args)
}

// Args ...
func (h *handler) Args(txtype core.TxType, method core.Method) scale.Type {
	if txtype == core.LocalMethodCall && method == MethodDrainVault {
		return &DrainVaultArguments{}
	}
	return h.multisig.Args(txtype, method)
}
