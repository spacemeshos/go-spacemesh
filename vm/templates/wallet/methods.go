package wallet

import (
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/templates"
)

type DeployArgs struct {
	Code []byte
}

type ProxyArgs struct {
	Destination types.Address
	Method      *athcon.MethodSelector
	Args        *[]byte
	Amount      uint64
}

type SpawnArgs struct {
	Pubkey [32]byte
}

type SpendArgs struct {
	To     types.Address
	Amount uint64
}

func ParseArgs(payload athcon.Payload) (any, error) {
	if payload.Selector == nil {
		return nil, errors.New("nil method selector")
	}
	var txArgs any

	switch *payload.Selector {
	case templates.DeploySelector:
		txArgs = new(DeployArgs)
	case templates.ProxySelector:
		txArgs = new(ProxyArgs)
	case templates.SpawnSelector:
		txArgs = new(SpawnArgs)
	case templates.SpendSelector:
		txArgs = new(SpendArgs)
	default:
		return nil, fmt.Errorf("unknown method selector %q", payload.Selector.String())
	}
	err := gossamerScale.Unmarshal(payload.Input, txArgs)
	if err != nil {
		return nil, fmt.Errorf("malformed tx arguments payload: %w", err)
	}

	return txArgs, nil
}
