package dbsync

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

func verifyP2P(t *testing.T, itemsA, itemsB, combinedItems []KeyBytes) {
	nr := hashsync.RmmeNumRead()
	nw := hashsync.RmmeNumWritten()
	const maxDepth = 24
	log := zaptest.NewLogger(t)
	t.Logf("QQQQQ: 0")
	dbA := populateDB(t, 32, itemsA)
	t.Logf("QQQQQ: 1")
	dbB := populateDB(t, 32, itemsB)
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)
	proto := "itest"
	t.Logf("QQQQQ: 2")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	st := &SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	storeA := NewItemStoreAdapter(NewDBItemStore(dbA, st, 32, maxDepth))
	t.Logf("QQQQQ: 2.1")
	require.NoError(t, dbA.WithTx(ctx, func(tx sql.Transaction) error {
		return storeA.s.EnsureLoaded(WithSQLExec(ctx, tx))
	}))
	t.Logf("QQQQQ: 3")
	storeB := NewItemStoreAdapter(NewDBItemStore(dbB, st, 32, maxDepth))
	t.Logf("QQQQQ: 3.1")
	var x *types.Hash32
	require.NoError(t, dbB.WithTx(ctx, func(tx sql.Transaction) error {
		ctx := WithSQLExec(ctx, tx)
		if err := storeB.s.EnsureLoaded(ctx); err != nil {
			return err
		}
		it, err := storeB.Min(ctx)
		if err != nil {
			return err
		}
		if it != nil {
			x = &types.Hash32{}
			k, err := it.Key()
			if err != nil {
				return err
			}
			h := k.(types.Hash32)
			v := load64(h[:]) & ^uint64(1<<(64-maxDepth)-1)
			binary.BigEndian.PutUint64(x[:], v)
			for i := 8; i < len(x); i++ {
				x[i] = 0
			}
			t.Logf("x: %s", x.String())
		}
		return nil
	}))
	t.Logf("QQQQQ: 4")

	// QQQQQ: rmme
	// storeB.s.ft.traceEnabled = true
	// storeB.qqqqq = true
	// require.NoError(t, storeB.s.EnsureLoaded())
	// var sb strings.Builder
	// storeA.s.ft.dump(&sb)
	// t.Logf("storeA:\n%s", sb.String())
	// sb = strings.Builder{}
	// storeB.s.ft.dump(&sb)
	// t.Logf("storeB:\n%s", sb.String())

	srvPeerID := mesh.Hosts()[0].ID()
	srv := server.New(mesh.Hosts()[0], proto,
		func(ctx context.Context, req []byte, stream io.ReadWriter) error {
			pss := hashsync.NewPairwiseStoreSyncer(nil, []hashsync.RangeSetReconcilerOption{
				hashsync.WithMaxSendRange(1),
				// uncomment to enable verbose logging which may slow down tests
				// hashsync.WithRangeSyncLogger(log.Named("sideA")),
			})
			return dbA.WithTx(ctx, func(tx sql.Transaction) error {
				return pss.Serve(WithSQLExec(ctx, tx), req, stream, storeA)
			})
		},
		server.WithTimeout(10*time.Second),
		server.WithLog(log))

	var eg errgroup.Group

	client := server.New(mesh.Hosts()[1], proto,
		func(ctx context.Context, req []byte, stream io.ReadWriter) error {
			return errors.New("client should not receive requests")
		},
		server.WithTimeout(10*time.Second),
		server.WithLog(log))

	defer func() {
		cancel()
		eg.Wait()
	}()
	eg.Go(func() error {
		return srv.Run(ctx)
	})
	eg.Go(func() error {
		// TBD: this probably isn't needed
		return client.Run(ctx)
	})

	require.Eventually(t, func() bool {
		for _, h := range mesh.Hosts() {
			if len(h.Mux().Protocols()) == 0 {
				return false
			}
		}
		return true
	}, time.Second, 10*time.Millisecond)

	pss := hashsync.NewPairwiseStoreSyncer(client, []hashsync.RangeSetReconcilerOption{
		hashsync.WithMaxSendRange(1),
		// uncomment to enable verbose logging which may slow down tests
		// hashsync.WithRangeSyncLogger(log.Named("sideB")),
	})

	tStart := time.Now()
	require.NoError(t, dbB.WithTx(ctx, func(tx sql.Transaction) error {
		return pss.SyncStore(WithSQLExec(ctx, tx), srvPeerID, storeB, x, x)
	}))
	t.Logf("synced in %v", time.Since(tStart))
	t.Logf("bytes read: %d, bytes written: %d", hashsync.RmmeNumRead()-nr, hashsync.RmmeNumWritten()-nw)

	// // QQQQQ: rmme
	// sb = strings.Builder{}
	// storeA.s.ft.dump(&sb)
	// t.Logf("storeA post-sync:\n%s", sb.String())
	// sb = strings.Builder{}
	// storeB.s.ft.dump(&sb)
	// t.Logf("storeB post-sync:\n%s", sb.String())

	if len(combinedItems) == 0 {
		return
	}
	var actItemsA []KeyBytes
	require.NoError(t, dbA.WithTx(ctx, func(tx sql.Transaction) error {
		it, err := storeA.Min(WithSQLExec(ctx, tx))
		require.NoError(t, err)
		if len(combinedItems) == 0 {
			assert.Nil(t, it)
		} else {
			for range combinedItems {
				k, err := it.Key()
				require.NoError(t, err)
				h := k.(types.Hash32)
				// t.Logf("synced itemA: %s", h.String())
				actItemsA = append(actItemsA, h[:])
				require.NoError(t, it.Next())
			}
			k, err := it.Key()
			require.NoError(t, err)
			h := k.(types.Hash32)
			assert.Equal(t, actItemsA[0], KeyBytes(h[:]))
		}
		return nil
	}))

	var actItemsB []KeyBytes
	require.NoError(t, dbB.WithTx(ctx, func(tx sql.Transaction) error {
		it, err := storeB.Min(WithSQLExec(ctx, tx))
		require.NoError(t, err)
		if len(combinedItems) == 0 {
			assert.Nil(t, it)
		} else {
			for range combinedItems {
				k, err := it.Key()
				require.NoError(t, err)
				h := k.(types.Hash32)
				// t.Logf("synced itemB: %s", h.String())
				actItemsB = append(actItemsB, h[:])
				require.NoError(t, it.Next())
			}
			k, err := it.Key()
			require.NoError(t, err)
			h := k.(types.Hash32)
			assert.Equal(t, actItemsB[0], KeyBytes(h[:]))
		}
		return nil
	}))
	assert.Equal(t, combinedItems, actItemsA)
	assert.Equal(t, actItemsA, actItemsB)
}

func TestP2P(t *testing.T) {
	t.Run("predefined items", func(t *testing.T) {
		verifyP2P(
			t, []KeyBytes{
				util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
				util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
				util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
				util.FromHex("abcdef1234567890000000000000000000000000000000000000000000000000"),
			},
			[]KeyBytes{
				util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
				util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
				util.FromHex("5555555555555555555555555555555555555555555555555555555555555555"),
				util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
			},
			[]KeyBytes{
				util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
				util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
				util.FromHex("5555555555555555555555555555555555555555555555555555555555555555"),
				util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
				util.FromHex("abcdef1234567890000000000000000000000000000000000000000000000000"),
			})
	})
	t.Run("predefined items 2", func(t *testing.T) {
		verifyP2P(
			t, []KeyBytes{
				util.FromHex("80e95b39faa731eb50eae7585a8b1cae98f503481f950fdb690e60ff86c21236"),
				util.FromHex("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7"),
				util.FromHex("d862b2413af5c252028e8f9871be8297e807661d64decd8249ac2682db168b90"),
				util.FromHex("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567"),
			},
			[]KeyBytes{
				util.FromHex("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7"),
				util.FromHex("bc6218a88d1648b8145fbf93ae74af8975f193af88788e7add3608e0bc50f701"),
				util.FromHex("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567"),
				util.FromHex("fbf03324234f79a3fe0587cf5505d7e4c826cb2be38d72eafa60296ed77b3f8f"),
			},
			[]KeyBytes{
				util.FromHex("80e95b39faa731eb50eae7585a8b1cae98f503481f950fdb690e60ff86c21236"),
				util.FromHex("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7"),
				util.FromHex("bc6218a88d1648b8145fbf93ae74af8975f193af88788e7add3608e0bc50f701"),
				util.FromHex("d862b2413af5c252028e8f9871be8297e807661d64decd8249ac2682db168b90"),
				util.FromHex("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567"),
				util.FromHex("fbf03324234f79a3fe0587cf5505d7e4c826cb2be38d72eafa60296ed77b3f8f"),
			})
	})
	t.Run("empty to non-empty", func(t *testing.T) {
		verifyP2P(
			t, nil,
			[]KeyBytes{
				util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
				util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
				util.FromHex("5555555555555555555555555555555555555555555555555555555555555555"),
				util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
			},
			[]KeyBytes{
				util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
				util.FromHex("123456789abcdef0000000000000000000000000000000000000000000000000"),
				util.FromHex("5555555555555555555555555555555555555555555555555555555555555555"),
				util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
			})
	})
	t.Run("empty to empty", func(t *testing.T) {
		verifyP2P(t, nil, nil, nil)
	})
	t.Run("random test", func(t *testing.T) {
		// TODO: increase these values and profile
		// const nShared = 8000000
		// const nUniqueA = 100
		// const nUniqueB = 80000
		// const nShared = 8000000
		// const nUniqueA = 10
		// const nUniqueB = 8000
		const nShared = 80000
		const nUniqueA = 400
		const nUniqueB = 800
		// const nShared = 2
		// const nUniqueA = 2
		// const nUniqueB = 2
		combined := make([]KeyBytes, 0, nShared+nUniqueA+nUniqueB)
		itemsA := make([]KeyBytes, nShared+nUniqueA)
		for i := range itemsA {
			h := types.RandomHash()
			itemsA[i] = KeyBytes(h[:])
			combined = append(combined, itemsA[i])
			// t.Logf("itemsA[%d] = %s", i, itemsA[i])
		}
		itemsB := make([]KeyBytes, nShared+nUniqueB)
		for i := range itemsB {
			if i < nShared {
				itemsB[i] = slices.Clone(itemsA[i])
			} else {
				h := types.RandomHash()
				itemsB[i] = KeyBytes(h[:])
				combined = append(combined, itemsB[i])
			}
			// t.Logf("itemsB[%d] = %s", i, itemsB[i])
		}
		slices.SortFunc(combined, func(a, b KeyBytes) int {
			return a.Compare(b)
		})
		// for i, v := range combined {
		// 	t.Logf("combined[%d] = %s", i, v)
		// }
		verifyP2P(t, itemsA, itemsB, combined)
		// TODO: multiple iterations
	})
}
