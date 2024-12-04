package wallet

import (
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var DeploySelector, spawnSelector, spendSelector athcon.MethodSelector

func init() {
	var err error
	DeploySelector, err = athcon.FromString("athexp_deploy")
	if err != nil {
		panic(err.Error())
	}
	spawnSelector, err = athcon.FromString("athexp_spawn")
	if err != nil {
		panic(err.Error())
	}
	spendSelector, err = athcon.FromString("athexp_spend")
	if err != nil {
		panic(err.Error())
	}
}

type DeployArgs struct{}

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
	var (
		txArgs any
		err    error
	)
	switch *payload.Selector {
	case DeploySelector:
		txArgs = new(DeployArgs)
		// decoding deploy isn't interesting
	case spawnSelector:
		txArgs = new(SpawnArgs)
		err = gossamerScale.Unmarshal(payload.Input, txArgs)
	case spendSelector:
		txArgs = new(SpendArgs)
		err = gossamerScale.Unmarshal(payload.Input, txArgs)
	default:
		return nil, fmt.Errorf("unknown method selector %q", payload.Selector.String())
	}
	if err != nil {
		return nil, fmt.Errorf("malformed tx arguments payload: %w", err)
	}

	return txArgs, nil
}
