package transactions

import (
	"bytes"
	"context"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
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

	gen := fixture.NewTransactionResultGenerator()
	txs := make([]types.TransactionWithResult, 100)
	require.NoError(t, db.WithTx(context.TODO(), func(dtx *sql.Tx) error {
		for i := range txs {
			tx := gen.Next()

			require.NoError(t, Add(dtx, &tx.Transaction, time.Time{}))
			require.NoError(t, AddResult(dtx, tx.ID, &tx.TransactionResult))
			txs[i] = *tx
		}
		return nil
	}))

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
				Start: &gen.Layers[5],
			},
		},
		{
			desc: "End",
			filter: ResultsFilter{
				End: &gen.Layers[8],
			},
		},
		{
			desc: "StartEnd",
			filter: ResultsFilter{
				Start: &gen.Layers[4],
				End:   &gen.Layers[8],
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
				Address: &gen.Addrs[0],
			},
		},
		{
			desc: "StartAddress",
			filter: ResultsFilter{
				Address: &gen.Addrs[0],
				Start:   &gen.Layers[3],
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

func TestIterateSnapshot(t *testing.T) {
	db, err := sql.Open("file:" + filepath.Join(t.TempDir(), "test.sql"))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, err)
	gen := fixture.NewTransactionResultGenerator()
	expect := 10
	require.NoError(t, db.WithTx(context.TODO(), func(dtx *sql.Tx) error {
		for i := 0; i < expect; i++ {
			tx := gen.Next()

			require.NoError(t, Add(dtx, &tx.Transaction, time.Time{}))
			require.NoError(t, AddResult(dtx, tx.ID, &tx.TransactionResult))
		}
		return nil
	}))

	var (
		once        sync.Once
		initialized = make(chan struct{})
		wait        = make(chan struct{})
		rst         = make(chan int)
	)
	go func() {
		n := 0
		assert.NoError(t, IterateResults(db, ResultsFilter{}, func(*types.TransactionWithResult) bool {
			once.Do(func() { close(initialized) })
			<-wait
			n++
			return true
		}))
		rst <- n
	}()
	<-initialized

	require.NoError(t, db.WithTx(context.TODO(), func(dtx *sql.Tx) error {
		for i := 0; i < 10; i++ {
			tx := gen.Next()

			require.NoError(t, Add(dtx, &tx.Transaction, time.Time{}))
			require.NoError(t, AddResult(dtx, tx.ID, &tx.TransactionResult))
		}
		return nil
	}))
	close(wait)
	select {
	case <-time.After(time.Second):
		require.Fail(t, "failed on timeout")
	case n := <-rst:
		require.Equal(t, expect, n)
	}
}
