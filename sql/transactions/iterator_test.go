package transactions

import (
	"bytes"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func matchTx(tx types.TransactionWithResult, filter ResultsFilter) bool {
	if filter.Address != nil {
		addr := *filter.Address
		matched := false
		for _, txaddr := range tx.Addresses {
			if addr == txaddr {
				matched = true
			}
		}
		if !matched {
			return false
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

	rng := rand.New(rand.NewSource(101))

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
	txs := make([]types.TransactionWithResult, 100)
	for i := range txs {
		rng.Read(txs[i].ID[:])
		txs[i].Raw = make([]byte, 10)
		rng.Read(txs[i].Raw)
		txs[i].Block = blocks[rng.Intn(len(blocks))]
		txs[i].Layer = layers[rng.Intn(len(layers))]
		if lth := rng.Intn(len(addrs)); lth > 0 {
			txs[i].Addresses = make([]types.Address, lth)

			rng.Shuffle(len(addrs), func(i, j int) {
				addrs[i], addrs[j] = addrs[j], addrs[i]
			})
			copy(txs[i].Addresses, addrs)
		}

		require.NoError(t, Add(db, &txs[i].Transaction, time.Time{}))
		require.NoError(t, AddResult(db, txs[i].ID, &txs[i].TransactionResult))
	}
	sort.Slice(txs, func(i, j int) bool {
		if txs[i].Layer == txs[j].Layer {
			return bytes.Compare(txs[i].ID[:], txs[j].ID[:]) == -1
		}
		return txs[i].Layer.Before(txs[j].Layer)
	})
	for _, tc := range []struct {
		desc   string
		filter ResultsFilter
	}{
		{
			desc: "All",
		},
		{
			desc: "Start",
			filter: ResultsFilter{
				Start: &layers[5],
			},
		},
		{
			desc: "End",
			filter: ResultsFilter{
				End: &layers[8],
			},
		},
		{
			desc: "StartEnd",
			filter: ResultsFilter{
				Start: &layers[4],
				End:   &layers[8],
			},
		},
		{
			desc: "ID",
			filter: ResultsFilter{
				TID: &txs[50].ID,
			},
		},
		{
			desc: "Address",
			filter: ResultsFilter{
				Address: &addrs[0],
			},
		},
		{
			desc: "StartAddress",
			filter: ResultsFilter{
				Address: &addrs[0],
				Start:   &layers[3],
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			expected := filterTxs(txs, tc.filter)
			i := 0
			require.NoError(t, IterateResults(db, tc.filter, func(rst *types.TransactionWithResult) bool {
				if !assert.Equal(t, expected[i], *rst) {
					return false
				}
				i++
				return true
			}))
			assert.Equal(t, len(expected), i)
		})
	}
}
