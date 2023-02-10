package vault

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

// TemplateAddress is an address of the vault template.
var TemplateAddress core.Address

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 9
}

// Register vault template.
func Register(reg *registry.Registry) {
	reg.Register(TemplateAddress, &handler{})
}

type handler struct{}

// Parse is noop on vault template.
func (h *handler) Parse(core.TxType, core.Method, *scale.Decoder) (core.ParseOutput, error) {
	return core.ParseOutput{}, nil
}

// New instantiates vault state.
func (h *handler) New(args any) (core.Template, error) {
	spawn := args.(*SpawnArguments)
	if spawn.InitialUnlockAmount > spawn.TotalAmount {
		return nil, fmt.Errorf("initial %d should be less or equal to total %d", spawn.InitialUnlockAmount, spawn.TotalAmount)
	}
	if spawn.VestingEnd.Before(spawn.VestingStart) {
		return nil, fmt.Errorf("vesting end %s should be atleast equal to start %s",
			spawn.VestingEnd, spawn.VestingStart)
	}
	return &Vault{
		Owner:               spawn.Owner,
		TotalAmount:         spawn.TotalAmount,
		InitialUnlockAmount: spawn.InitialUnlockAmount,
		VestingStart:        spawn.VestingStart,
		VestingEnd:          spawn.VestingEnd,
	}, nil
}

// Load vault from state.
func (h *handler) Load(state []byte) (core.Template, error) {
	dec := scale.NewDecoder(bytes.NewBuffer(state))
	vault := &Vault{}
	if _, err := vault.DecodeScale(dec); err != nil {
		return nil, fmt.Errorf("%w: %s", core.ErrInternal, err)
	}
	return vault, nil
}

// Exec supports only MethodSpend.
func (h *handler) Exec(host core.Host, txtype core.TxType, method core.Method, args scale.Encodable) error {
	if txtype == core.LocalMethodCall && method != core.MethodSpend {
		return fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
	spend := args.(*SpendArguments)
	return host.Template().(*Vault).Spend(host, spend.Destination, spend.Amount)
}

// Args ...
func (h *handler) Args(txtype core.TxType, method core.Method) scale.Type {
	switch txtype {
	case core.SelfSpawn, core.Spawn:
		return &SpawnArguments{}
	case core.LocalMethodCall:
		if method == core.MethodSpend {
			return &SpendArguments{}
		}
	}
	return nil
}
