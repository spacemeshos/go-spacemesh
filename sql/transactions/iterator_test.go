package transactions

import (
	"bytes"
	"sort"
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/stretchr/testify/require"
)

func matchTx(tx types.TransactionWithResult, filter ResultsFilter) bool {
	if len(filter.Addresses) > 0 {
		for _, addr := range filter.Addresses {
			found := false
			for _, txaddr := range tx.Addresses {
				if addr == txaddr {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	if filter.Start != nil {
		if tx.Layer.Before(*filter.Start) {
			return false
		}
	}
	if filter.End != nil {
		if tx.Layer.After(*filter.End) {
			return false
		}
	}
	if filter.TID != nil {
		if tx.ID != *filter.TID {
			return false
		}
	}
	return true
}

func filterTxs(txs []types.TransactionWithResult, filter ResultsFilter) []types.TransactionWithResult {
	var rst []types.TransactionWithResult
	for _, tx := range txs {
		if matchTx(tx, filter) {
			rst = append(rst, tx)
		}
	}
	return rst
}

func TestIterateResults(t *testing.T) {
	db := sql.InMemory()

	fuzzer := fuzz.NewWithSeed(time.Now().Unix())
	addrs := make([]types.Address, 10)
	for i := range addrs {
		addrs[i] = types.Address{byte(i)}
	}
	blocks := make([]types.BlockID, 10)
	for i := range blocks {
		blocks[i] = types.BlockID{byte(i)}
	}
	layers := make([]types.LayerID, 10)
	for i := range layers {
		layers[i] = types.NewLayerID(uint32(1 + i))
	}
	fuzzer = fuzzer.Funcs(
		func(e *[]types.Address, c fuzz.Continue) {
			n := c.Intn(len(addrs))
			*e = addrs[:n]
		},
		func(e *types.BlockID, c fuzz.Continue) {
			*e = blocks[c.Intn(len(blocks))]
		},
		func(e *types.LayerID, c fuzz.Continue) {
			*e = layers[c.Intn(len(layers))]
		},
	)
	txs := make([]types.TransactionWithResult, 100)
	for i := range txs {
		fuzzer.Fuzz(&txs[i])
		txs[i].ID = types.TransactionID{byte(i)}
		require.NoError(t, Add(db, &txs[i].Transaction, time.Time{}))
		require.NoError(t, AddResult(db, txs[i].ID, &txs[i].TransactionResult))
		_, err := Apply(db, txs[i].ID, txs[i].Layer, txs[i].Block)
		require.NoError(t, err)
	}
	sort.Slice(txs, func(i, j int) bool {
		if txs[i].Layer.Before(txs[j].Layer) {
			return true
		}
		return bytes.Compare(txs[i].ID[:], txs[j].ID[:]) == -1
	})
	for _, tc := range []struct {
		desc   string
		filter ResultsFilter
	}{
		{
			desc: "All",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			expected := filterTxs(txs, tc.filter)
			i := 0
			require.NoError(t, IterateResults(db, tc.filter, func(rst *types.TransactionWithResult) bool {
				require.Equal(t, expected[i], *rst)
				i++
				return true
			}))
		})
	}
}
