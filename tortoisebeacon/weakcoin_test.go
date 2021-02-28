package tortoisebeacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestWeakCoinGenerator_GenerateProposal(t *testing.T) {
	r := require.New(t)

	wcmp := mockWeakCoinPublisher{}
	wcg := NewWeakCoinGenerator(defaultPrefix, defaultThreshold, wcmp)
	epoch := types.EpochID(3)
	round := 1
	expected := 0xb9

	p, err := wcg.GenerateProposal(epoch, round)
	r.NoError(err)

	r.EqualValues(expected, p)
}
