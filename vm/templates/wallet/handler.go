package wallet

import (
	"errors"
	"fmt"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"

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
	var m core.Metadata
	var p core.Payload

	if _, err = m.DecodeScale(decoder); err != nil {
		err = fmt.Errorf("%w: %w", core.ErrMalformed, err)
		return
	}
	if _, err = p.DecodeScale(decoder); err != nil {
		err = fmt.Errorf("%w: %w", core.ErrMalformed, err)
		return
	}
	output.GasPrice = m.GasPrice
	output.Nonce = m.Nonce
	output.Payload = p
	return output, nil
}

// New instatiates single sig wallet with spawn arguments.
func (*handler) New(host core.Host, logger *zap.Logger) (core.Template, error) {
	return New(host, logger)
}

// Pass the transaction into the VM for execution.
func (*handler) Exec(host core.Host, payload core.Payload, logger *zap.Logger) ([]byte, int64, error) {
	// Load the template code
	templateAccount, err := host.Get(host.TemplateAddress())
	if err != nil {
		return []byte{}, 0, fmt.Errorf("failed to load template account: %w", err)
	} else if len(templateAccount.State) == 0 {
		return []byte{}, 0, errors.New("template account state is empty")
	}

	// Instantiate the VM
	vmhost, err := vmhost.NewHost(host, logger)
	if err != nil {
		return []byte{}, 0, fmt.Errorf("failed to instantiate VM: %w", err)
	}
	defer vmhost.Destroy()

	// Augment the payload with the account state snapshot
	// Note: for a spawn, this will be empty, which is fine.
	principalAccount, err := host.Get(host.Principal())
	if err != nil {
		return []byte{}, 0, fmt.Errorf("failed to load principal account: %w", err)
	}

	// sanity check - verify should have failed for this tx
	if host.IsSpawn() && len(principalAccount.State) > 0 {
		return []byte{}, 0, errors.New("wallet account state is not empty for spawn")
	}
	executionPayload := athcon.EncodedExecutionPayload(principalAccount.State, payload)

	// Execute the transaction in the VM
	// Note: at this point, maxgas was already consumed from the principal account, so we don't
	// need to check the account balance, but we still need to communicate the amount to the VM
	// so it can short-circuit execution if the amount is exceeded.
	maxgas := int64(host.MaxGas() - host.GasSpent())
	if maxgas < 0 {
		return []byte{}, 0, errors.New("gas limit exceeds maximum int64 value")
	}
	output, gasLeft, err := vmhost.Execute(
		host.Layer(),
		maxgas,
		host.Principal(),
		host.Principal(),
		executionPayload,
		// note: value here is zero because this is unused at the top-level. any amount actually being
		// transferred is encoded in the args to a wallet.Spend() method inside the payload; in other
		// words, it's abstracted inside the VM as part of our account abstraction.
		// note that this field is used for lower-level calls triggered by Call.
		0,
		templateAccount.State,
	)
	host.SpendGas(uint64(maxgas) - uint64(gasLeft))
	return output, gasLeft, err
}
