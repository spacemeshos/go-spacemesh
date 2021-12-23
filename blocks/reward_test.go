package blocks

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/rand"
)

func Test_calculateLayerReward(t *testing.T) {
	base1 := rand.Uint64()
	assert.Equal(t, base1, calculateLayerReward(RewardConfig{BaseReward: base1}))
	base2 := base1 + rand.Uint64()
	assert.Equal(t, base2, calculateLayerReward(RewardConfig{BaseReward: base2}))
}

func Test_calculateRewardPerProposal(t *testing.T) {
	var (
		numTXs       = rand.Intn(1000)
		base         = uint64(50000)
		numProposals = uint64(13)
	)
	totalFee, _, txs := createTransactions(t, numTXs)
	rewardInfo := calculateRewardPerProposal(types.NewLayerID(210), RewardConfig{BaseReward: base}, txs, int(numProposals))
	expectedTotalRewardsPer := (totalFee + base) / numProposals
	expectedLayerRewardPer := base / numProposals
	assert.Equal(t, numProposals, rewardInfo.numProposals)
	assert.Equal(t, base, rewardInfo.layerReward)
	assert.Equal(t, totalFee, rewardInfo.feesReward)
	assert.Equal(t, expectedTotalRewardsPer, rewardInfo.totalRewardPer)
	assert.Equal(t, expectedLayerRewardPer, rewardInfo.layerRewardPer)
}
