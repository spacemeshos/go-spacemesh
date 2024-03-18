package fixture

import (
	"encoding/binary"
	"math/rand"
	"slices"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// NewRewardsGenerator with some random parameters.
func NewRewardsGenerator() *RewardsGenerator {
	return new(RewardsGenerator).
		WithSeed(time.Now().UnixNano()).
		WithAddresses(10).
		WithLayers(0, 10)
}

// RewardsGenerator generates random rewards.
// Rewards are not syntactically or contextually valid. This is for testing databases and APIs.
type RewardsGenerator struct {
	rng *rand.Rand

	UniqueCoinbase bool
	Addrs          []types.Address
	Layers         []types.LayerID
}

// WithSeed update randomness source.
func (g *RewardsGenerator) WithSeed(seed int64) *RewardsGenerator {
	g.rng = rand.New(rand.NewSource(seed))
	return g
}

// WithAddresses update addresses.
func (g *RewardsGenerator) WithAddresses(n int) *RewardsGenerator {
	g.Addrs = nil
	for i := 1; i <= n; i++ {
		addr := types.Address{}
		binary.BigEndian.PutUint64(addr[:], uint64(i))
		g.Addrs = append(g.Addrs, addr)
	}
	return g
}

// WithLayers updates layers.
func (g *RewardsGenerator) WithLayers(start, n int) *RewardsGenerator {
	g.Layers = nil
	for i := 1; i <= n; i++ {
		g.Layers = append(g.Layers, types.LayerID(uint32(start+i)))
	}
	return g
}

// WithUniqueCoinbase will generate every reward with different address.
func (g *RewardsGenerator) WithUniqueCoinbase() *RewardsGenerator {
	g.UniqueCoinbase = true
	return g
}

// Next generates Reward.
func (g *RewardsGenerator) Next() *types.Reward {
	var reward types.Reward
	g.rng.Read(reward.SmesherID[:])

	index := g.rng.Intn(len(g.Addrs))
	coinbase := g.Addrs[index]

	if g.UniqueCoinbase {
		g.Addrs = slices.Delete(g.Addrs, index, index+1)
	}

	reward.Coinbase = coinbase
	reward.LayerReward = uint64(g.rng.Int())
	reward.TotalReward = uint64(g.rng.Int())
	reward.Layer = g.Layers[g.rng.Intn(len(g.Layers))]
	return &reward
}
