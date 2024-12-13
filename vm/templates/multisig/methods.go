package multisig

import (
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates"
)

// SpawnArguments contains a collection with PublicKeys.
type SpawnArguments struct {
	Required   uint8
	PublicKeys []core.PublicKey
}

type SpendArguments struct {
	To     types.Address
	Amount uint64
}

type DeployArguments struct {
	Code []byte
}

func ParseArgs(payload athcon.Payload) (any, error) {
	if payload.Selector == nil {
		return nil, errors.New("nil method selector")
	}
	var txArgs any

	switch *payload.Selector {
	case templates.DeploySelector:
		txArgs = new(DeployArguments)
	case templates.SpawnSelector:
		txArgs = new(SpawnArguments)
	case templates.SpendSelector:
		txArgs = new(SpendArguments)
	default:
		return nil, fmt.Errorf("unknown method selector %q", payload.Selector.String())
	}
	err := gossamerScale.Unmarshal(payload.Input, txArgs)
	if err != nil {
		return nil, fmt.Errorf("malformed tx arguments payload: %w", err)
	}

	return txArgs, nil
}
