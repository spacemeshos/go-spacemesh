package tokenwallet

import (
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates"
)

type SpawnArguments struct {
	Owner          core.PublicKey
	MintTemplate   types.Address
	WalletTemplate types.Address
}

type SpendArguments struct {
	TokenId types.Address
	To      types.Address
	Amount  uint64
}

type ReceiveArguments struct {
	TokenId types.Address
	Amount  uint64
}

func ParseArgs(payload athcon.Payload) (any, error) {
	if payload.Selector == nil {
		return nil, errors.New("nil method selector")
	}
	var txArgs any

	switch *payload.Selector {
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
