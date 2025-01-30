package vm

import (
	"encoding/binary"
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	vmhost "github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/templates"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

// maxSpend checks the maximum amount that might be spent during the execution
// of the TX.
// NOTE: It doesn't take into the account possible spending in a PROXIED call!
func maxSpend(host core.Host, account *types.Account, payload []byte, logger *zap.Logger) (uint64, error) {
	maxgas := int64(host.MaxGas())
	if maxgas < 0 {
		return 0, errors.New("gas limit exceeds maximum int64 value")
	}

	var unmarshaled athcon.Payload
	err := gossamerScale.Unmarshal(payload, &unmarshaled)
	if err != nil {
		return 0, fmt.Errorf("%w: malformed spawn payload", core.ErrMalformed)
	}

	// Check the method selector
	// We define MaxSpend for any method other than spend to be zero for now.
	if unmarshaled.Selector == nil ||
		!(*unmarshaled.Selector == templates.SpendSelector || *unmarshaled.Selector == templates.ProxySelector) {
		return 0, nil
	}

	// FIXME: special-case proxying as wallet contract doesn't support calculating max spend for it...
	if *unmarshaled.Selector == templates.ProxySelector {
		args, err := wallet.ParseArgs(unmarshaled)
		if err != nil {
			return 0, fmt.Errorf("parsing wallet arguments for MaxSpend: %w", err)
		}
		maxSpend := args.(*wallet.ProxyArgs).Amount
		return maxSpend, nil
	}

	// construct the payload. this requires some surgery to replace the method maxSpendSelector.
	maxGasPayload := athcon.Payload{
		Selector: &templates.MaxSpendSelector,
		Input:    unmarshaled.Input,
	}
	maxGasPayloadEncoded, err := gossamerScale.Marshal(maxGasPayload)
	if err != nil {
		return 0, fmt.Errorf("marshaling maxSpend payload: %w", err)
	}
	executionPayload := athcon.EncodedExecutionPayload(account.State, maxGasPayloadEncoded)

	// Instantiate the VM
	// Use a mock host to ensure that no state changes occur.
	host = host.Clone()
	vmhost, err := vmhost.NewHost(host, logger)
	if err != nil {
		return 0, fmt.Errorf("loading Athena VM: %w", err)
	}
	defer vmhost.Destroy()

	logger.Debug(
		"executing maxspend",
		zap.Stringer("template", host.TemplateAddress()),
		zap.Uint32("layer", host.Layer().Uint32()),
		zap.Int64("maxgas", maxgas),
	)
	output, _, err := vmhost.Execute(
		host.Layer(),
		maxgas,
		host.Principal(),
		host.Principal(),
		executionPayload,
		host.Template(),
	)
	if err != nil {
		return 0, fmt.Errorf("executing max_spend: %w", err)
	}
	if len(output) != 8 {
		return 0, errors.New("max spend output is not 8 bytes")
	}
	return binary.LittleEndian.Uint64(output), nil
}
