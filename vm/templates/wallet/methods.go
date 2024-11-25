package wallet

import (
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var spawnSelector, spendSelector athcon.MethodSelector

func init() {
	var err error
	spawnSelector, err = athcon.FromString("athexp_spawn")
	if err != nil {
		panic(err.Error())
	}
	spendSelector, err = athcon.FromString("athexp_spend")
	if err != nil {
		panic(err.Error())
	}
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
	case spawnSelector:
		txArgs = new(SpawnArgs)
	case spendSelector:
		txArgs = new(SpendArgs)
	default:
		return nil, fmt.Errorf("unknown method selector %s", payload.Selector.String())
	}
	err := gossamerScale.Unmarshal(payload.Input, txArgs)
	if err != nil {
		return nil, fmt.Errorf("malformed tx arguments payload: %w", err)
	}

	return txArgs, nil
}
