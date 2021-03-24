package tortoisebeacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestWeakCoinGenerator_GenerateProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	wcg := NewWeakCoinGenerator(defaultPrefix, defaultThreshold, nil)
	epoch := types.EpochID(3)
	round := uint64(1)
	expected := 0xb9

	p, err := wcg.(*weakCoinGenerator).generateProposal(epoch, round)
	r.NoError(err)

	r.EqualValues(expected, p)
}
