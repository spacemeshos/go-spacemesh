package weakcoin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func TestWeakCoinGenerator_GenerateProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	logger := log.NewDefault("WeakCoin")
	wcg := NewWeakCoin(DefaultPrefix, DefaultThreshold, nil, logger)
	epoch := types.EpochID(3)
	round := types.RoundID(1)
	expected := 0xb9

	p, err := wcg.(*weakCoin).generateProposal(epoch, round)
	r.NoError(err)

	r.EqualValues(expected, p)
}
