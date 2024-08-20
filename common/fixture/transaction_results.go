package fixture

import (
	"encoding/binary"
	"math/rand"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	wallet2 "github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
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
func (g *TransactionResultGenerator) WithLayers(start, n int) *TransactionResultGenerator {
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

	rnd := g.rng.Perm(len(g.Addrs))
	principal := g.Addrs[rnd[0]]
	tx.Addresses = []types.Address{
		principal,
	}

	_, priv, _ := ed25519.GenerateKey(g.rng)
	var rawTx []byte
	method := core.MethodSpawn

	if g.rng.Intn(2) == 1 {
		dest := g.Addrs[rnd[1]]
		tx.Addresses = append(tx.Addresses, dest)
		rawTx = wallet2.Spend(priv, dest, 100, types.Nonce(1))
		method = core.MethodSpend
	} else {
		rawTx = wallet2.SelfSpawn(priv, types.Nonce(1))
	}
	tx.RawTx = types.NewRawTx(rawTx)
	tx.Block = g.Blocks[g.rng.Intn(len(g.Blocks))]
	tx.Layer = g.Layers[g.rng.Intn(len(g.Layers))]
	tx.TxHeader = &types.TxHeader{
		TemplateAddress: wallet.TemplateAddress,
		Method:          uint8(method),
		Principal:       principal,
		Nonce:           types.Nonce(1),
	}
	return &tx
}
