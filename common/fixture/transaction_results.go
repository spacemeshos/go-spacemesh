package fixture

import (
	"encoding/binary"
	"math/rand"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// NewTransactionResultGenerator with some random parameters.
func NewTransactionResultGenerator() *TransactionResultGenerator {
	return new(TransactionResultGenerator).
		WithSeed(time.Now().UnixNano()).
		WithBlocks(10).
		WithAddresses(10).
		WithLayers(0, 10)
}

// TransactionResultGenerator generates random transaction with results.
// Transactions are not syntactically or contextually valid. This is for testing databases and APIs.
type TransactionResultGenerator struct {
	rng *rand.Rand

	Blocks []types.BlockID
	Addrs  []types.Address
	Layers []types.LayerID
}

// WithSeed update randomness source.
func (g *TransactionResultGenerator) WithSeed(seed int64) *TransactionResultGenerator {
	g.rng = rand.New(rand.NewSource(seed))
	return g
}

// WithBlocks update blocks ids.
func (g *TransactionResultGenerator) WithBlocks(n int) *TransactionResultGenerator {
	g.Blocks = nil
	for i := 1; i <= n; i++ {
		g.Blocks = append(g.Blocks, types.BlockID{byte(i)})
	}
	return g
}

// WithAddresses update addresses.
func (g *TransactionResultGenerator) WithAddresses(n int) *TransactionResultGenerator {
	g.Addrs = nil
	for i := 1; i <= n; i++ {
		addr := types.Address{}
		binary.BigEndian.PutUint64(addr[:], uint64(i))
		g.Addrs = append(g.Addrs, addr)
	}
	return g
}

// WithLayers updates layers.
func (g *TransactionResultGenerator) WithLayers(start int, n int) *TransactionResultGenerator {
	g.Layers = nil
	for i := 1; i <= n; i++ {
		g.Layers = append(g.Layers, types.LayerID(uint32(start+i)))
	}
	return g
}

// Next generates TransactionWithResult.
func (g *TransactionResultGenerator) Next() *types.TransactionWithResult {
	var tx types.TransactionWithResult
	g.rng.Read(tx.ID[:])
	tx.Raw = make([]byte, 10)
	g.rng.Read(tx.Raw)
	tx.Block = g.Blocks[g.rng.Intn(len(g.Blocks))]
	tx.Layer = g.Layers[g.rng.Intn(len(g.Layers))]
	if lth := g.rng.Intn(len(g.Addrs)); lth > 0 {
		tx.Addresses = make([]types.Address, lth%10+1)

		g.rng.Shuffle(len(g.Addrs), func(i, j int) {
			g.Addrs[i], g.Addrs[j] = g.Addrs[j], g.Addrs[i]
		})
		copy(tx.Addresses, g.Addrs)
	}
	return &tx
}
