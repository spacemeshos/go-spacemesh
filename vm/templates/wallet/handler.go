package wallet

import (
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/vm/core"
	vmhost "github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/registry"
)

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 1
}

// Register Wallet template.
func Register(registry *registry.Registry) {
	registry.Register(TemplateAddress, &handler{})
}

var (
	_ core.Handler = (*handler)(nil)
	// TemplateAddress is an address of the Wallet template.
	TemplateAddress core.Address
)

type handler struct{}

// Parse header.
func (*handler) Parse(decoder *scale.Decoder) (output core.ParseOutput, err error) {
	var p core.Payload
	if _, err = p.DecodeScale(decoder); err != nil {
		err = fmt.Errorf("%w: %w", core.ErrMalformed, err)
		return
	}
	output.GasPrice = p.GasPrice
	output.Nonce = p.Nonce
	return output, nil
}

// New instatiates single sig wallet with spawn arguments.
func (*handler) New(args any) (core.Template, error) {
	return New(args.(*SpawnArguments)), nil
}

// Load single sig wallet from stored state.
func (*handler) Load(state []byte) (core.Template, error) {
	// TODO(lane): pass blob into VM to instantiate the template instance (program)
	var wallet Wallet
	return &wallet, nil
}

// Pass the transaction into the VM for execution.
func (*handler) Exec(host core.Host, cache *core.StagedCache, payload []byte) error {
	// Load the template code
	templateAccount, err := cache.Get(host.TemplateAddress())
	if err != nil {
		return fmt.Errorf("failed to load template account: %w", err)
	} else if len(templateAccount.State) == 0 {
		return fmt.Errorf("template account state is empty")
	}

	// Construct the context
	staticContext := vmhost.StaticContext{
		// Athena does not currently allow proxied calls, so by definition the principal is the
		// same as the destination, for now. See https://github.com/athenavm/athena/issues/174.
		Principal:   host.Principal(),
		Destination: host.Principal(),
		Nonce:       host.Nonce(),
	}
	dynamicContext := vmhost.DynamicContext{
		Template: host.TemplateAddress(),
		Callee:   host.Principal(),
	}

	// Instantiate the VM
	vmhost, err := vmhost.NewHost(host, cache, cache, staticContext, dynamicContext)
	if err != nil {
		return err
	}

	// Execute the transaction in the VM
	maxgas := host.MaxGas()
	if int64(maxgas) < 0 {
		return fmt.Errorf("gas limit exceeds maximum int64 value")
	}
	vmhost.Execute(
		host.Layer(),
		int64(maxgas),
		host.Principal(),
		host.Principal(),
		payload,
		// note: value here is zero because this is unused at the top-level. any amount actually being
		// transferred is encoded in the args to a wallet.Spend() method inside the payload; in other
		// words, it's abstracted inside the VM as part of our account abstraction.
		// note that this field is used for lower-level calls triggered by Call.
		0,
		templateAccount.State,
	)
	return nil
}

func (h *handler) IsSpawn(payload []byte) bool {
	// TODO(lane): rewrite to use the VM
	// mock for now
	return true
}

// Args ...
func (h *handler) Args(payload []byte) scale.Type {
	// TODO(lane): rewrite to use the VM
	// mock for now
	return &SpawnArguments{}
	// switch method {
	// case core.MethodSpawn:
	// 	return &SpawnArguments{}
	// case core.MethodSpend:
	// 	return &SpendArguments{}
	// }
	// return nil
}
