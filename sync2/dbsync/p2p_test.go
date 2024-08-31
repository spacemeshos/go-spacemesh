package dbsync

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

var startDate = time.Date(2024, 8, 29, 18, 0, 0, 0, time.UTC)

type fooRow struct {
	id KeyBytes
	ts int64
}

func populateFoo(t *testing.T, rows []fooRow) sql.Database {
	db := sql.InMemoryTest(t)
	_, err := db.Exec(
		"create table foo(id char(32) not null primary key, received int)",
		nil, nil)
	require.NoError(t, err)
	for _, row := range rows {
		_, err := db.Exec(
			"insert into foo (id, received) values (?, ?)",
			func(stmt *sql.Statement) {
				stmt.BindBytes(1, row.id)
				stmt.BindInt64(2, row.ts)
			}, nil)
		require.NoError(t, err)
	}
	return db
}

type syncTracer struct {
	dumb          bool
	receivedItems int
	sentItems     int
}

var _ hashsync.Tracer = &syncTracer{}

func (tr *syncTracer) OnDumbSync() {
	// QQQQQQ: use mutex and also update handler_test.go in hashsync!!!!
	tr.dumb = true
}

func (tr *syncTracer) OnRecent(receivedItems, sentItems int) {
	tr.receivedItems += receivedItems
	tr.sentItems += sentItems
}

func verifyP2P(
	t *testing.T,
	rowsA, rowsB []fooRow,
	combinedItems []KeyBytes,
	clockAt time.Time,
	receivedRecent, sentRecent bool,
	opts ...hashsync.RangeSetReconcilerOption,
) {
	nr := hashsync.RmmeNumRead()
	nw := hashsync.RmmeNumWritten()
	const maxDepth = 24
	log := zaptest.NewLogger(t)
	t.Logf("QQQQQ: 0")
	dbA := populateFoo(t, rowsA)
	t.Logf("QQQQQ: 1")
	dbB := populateFoo(t, rowsB)
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)
	proto := "itest"
	t.Logf("QQQQQ: 2")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	st := &SyncedTable{
		TableName:       "foo",
		IDColumn:        "id",
		TimestampColumn: "received",
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

	var tr syncTracer
	opts = append(opts,
		hashsync.WithRangeReconcilerClock(clockwork.NewFakeClockAt(clockAt)),
		hashsync.WithTracer(&tr),
	)
	opts = opts[:len(opts):len(opts)]

	srvPeerID := mesh.Hosts()[0].ID()
	srv := server.New(mesh.Hosts()[0], proto,
		func(ctx context.Context, req []byte, stream io.ReadWriter) error {
			pss := hashsync.NewPairwiseStoreSyncer(nil, append(
				opts,
				hashsync.WithMaxSendRange(1),
				// uncomment to enable verbose logging which may slow down tests
				// hashsync.WithRangeReconcilerLogger(log.Named("sideA")),
			))
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

	pss := hashsync.NewPairwiseStoreSyncer(client, append(
		opts,
		hashsync.WithMaxSendRange(1),
		// uncomment to enable verbose logging which may slow down tests
		// hashsync.WithRangeReconcilerLogger(log.Named("sideB")),
	))

	tStart := time.Now()
	require.NoError(t, dbB.WithTx(ctx, func(tx sql.Transaction) error {
		return pss.SyncStore(WithSQLExec(ctx, tx), srvPeerID, storeB, x, x)
	}))
	t.Logf("synced in %v", time.Since(tStart))
	t.Logf("bytes read: %d, bytes written: %d", hashsync.RmmeNumRead()-nr, hashsync.RmmeNumWritten()-nw)

	require.Equal(t, receivedRecent, tr.receivedItems > 0)
	require.Equal(t, sentRecent, tr.sentItems > 0)

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

func fooR(id string, seconds int) fooRow {
	return fooRow{
		hexID(id),
		startDate.Add(time.Duration(seconds) * time.Second).UnixNano(),
	}
}

func hexID(s string) KeyBytes {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestP2P(t *testing.T) {
	t.Run("predefined items", func(t *testing.T) {
		verifyP2P(
			t, []fooRow{
				fooR("1111111111111111111111111111111111111111111111111111111111111111", 10),
				fooR("123456789abcdef0000000000000000000000000000000000000000000000000", 20),
				fooR("8888888888888888888888888888888888888888888888888888888888888888", 30),
				fooR("abcdef1234567890000000000000000000000000000000000000000000000000", 40),
			},
			[]fooRow{
				fooR("1111111111111111111111111111111111111111111111111111111111111111", 11),
				fooR("123456789abcdef0000000000000000000000000000000000000000000000000", 12),
				fooR("5555555555555555555555555555555555555555555555555555555555555555", 13),
				fooR("8888888888888888888888888888888888888888888888888888888888888888", 14),
			},
			[]KeyBytes{
				hexID("1111111111111111111111111111111111111111111111111111111111111111"),
				hexID("123456789abcdef0000000000000000000000000000000000000000000000000"),
				hexID("5555555555555555555555555555555555555555555555555555555555555555"),
				hexID("8888888888888888888888888888888888888888888888888888888888888888"),
				hexID("abcdef1234567890000000000000000000000000000000000000000000000000"),
			},
			startDate,
			false,
			false,
		)
	})
	t.Run("predefined items 2", func(t *testing.T) {
		verifyP2P(
			t, []fooRow{
				fooR("80e95b39faa731eb50eae7585a8b1cae98f503481f950fdb690e60ff86c21236", 10),
				fooR("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7", 20),
				fooR("d862b2413af5c252028e8f9871be8297e807661d64decd8249ac2682db168b90", 30),
				fooR("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567", 40),
			},
			[]fooRow{
				fooR("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7", 11),
				fooR("bc6218a88d1648b8145fbf93ae74af8975f193af88788e7add3608e0bc50f701", 12),
				fooR("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567", 13),
				fooR("fbf03324234f79a3fe0587cf5505d7e4c826cb2be38d72eafa60296ed77b3f8f", 14),
			},
			[]KeyBytes{
				hexID("80e95b39faa731eb50eae7585a8b1cae98f503481f950fdb690e60ff86c21236"),
				hexID("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7"),
				hexID("bc6218a88d1648b8145fbf93ae74af8975f193af88788e7add3608e0bc50f701"),
				hexID("d862b2413af5c252028e8f9871be8297e807661d64decd8249ac2682db168b90"),
				hexID("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567"),
				hexID("fbf03324234f79a3fe0587cf5505d7e4c826cb2be38d72eafa60296ed77b3f8f"),
			},
			startDate,
			false,
			false,
		)
	})
	t.Run("predefined items with recent", func(t *testing.T) {
		verifyP2P(
			t, []fooRow{
				fooR("80e95b39faa731eb50eae7585a8b1cae98f503481f950fdb690e60ff86c21236", 10),
				fooR("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7", 20),
				fooR("d862b2413af5c252028e8f9871be8297e807661d64decd8249ac2682db168b90", 30),
				fooR("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567", 40),
			},
			[]fooRow{
				fooR("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7", 11),
				fooR("bc6218a88d1648b8145fbf93ae74af8975f193af88788e7add3608e0bc50f701", 12),
				fooR("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567", 13),
				fooR("fbf03324234f79a3fe0587cf5505d7e4c826cb2be38d72eafa60296ed77b3f8f", 14),
			},
			[]KeyBytes{
				hexID("80e95b39faa731eb50eae7585a8b1cae98f503481f950fdb690e60ff86c21236"),
				hexID("b46eb2c08f01a87aa0fd76f70dc6b1048b04a1125a44cca79c1a61932d3773d7"),
				hexID("bc6218a88d1648b8145fbf93ae74af8975f193af88788e7add3608e0bc50f701"),
				hexID("d862b2413af5c252028e8f9871be8297e807661d64decd8249ac2682db168b90"),
				hexID("db1903851d4eba1e973fef5326cb997ea191c62a4b30d7830cc76931d28fd567"),
				hexID("fbf03324234f79a3fe0587cf5505d7e4c826cb2be38d72eafa60296ed77b3f8f"),
			},
			startDate.Add(time.Minute),
			true,
			true,
			hashsync.WithRecentTimeSpan(48*time.Second),
		)
	})
	t.Run("empty to non-empty", func(t *testing.T) {
		verifyP2P(
			t, nil,
			[]fooRow{
				fooR("1111111111111111111111111111111111111111111111111111111111111111", 11),
				fooR("123456789abcdef0000000000000000000000000000000000000000000000000", 12),
				fooR("5555555555555555555555555555555555555555555555555555555555555555", 13),
				fooR("8888888888888888888888888888888888888888888888888888888888888888", 14),
			},
			[]KeyBytes{
				hexID("1111111111111111111111111111111111111111111111111111111111111111"),
				hexID("123456789abcdef0000000000000000000000000000000000000000000000000"),
				hexID("5555555555555555555555555555555555555555555555555555555555555555"),
				hexID("8888888888888888888888888888888888888888888888888888888888888888"),
			},
			startDate,
			false,
			false,
		)
	})
	t.Run("empty to non-empty with recent", func(t *testing.T) {
		verifyP2P(
			t, nil,
			[]fooRow{
				fooR("1111111111111111111111111111111111111111111111111111111111111111", 11),
				fooR("123456789abcdef0000000000000000000000000000000000000000000000000", 12),
				fooR("5555555555555555555555555555555555555555555555555555555555555555", 13),
				fooR("8888888888888888888888888888888888888888888888888888888888888888", 14),
			},
			[]KeyBytes{
				hexID("1111111111111111111111111111111111111111111111111111111111111111"),
				hexID("123456789abcdef0000000000000000000000000000000000000000000000000"),
				hexID("5555555555555555555555555555555555555555555555555555555555555555"),
				hexID("8888888888888888888888888888888888888888888888888888888888888888"),
			},
			startDate.Add(time.Minute),
			true,
			true,
			hashsync.WithRecentTimeSpan(48*time.Second),
		)
	})
	t.Run("non-empty to empty with recent", func(t *testing.T) {
		verifyP2P(
			t,
			[]fooRow{
				fooR("1111111111111111111111111111111111111111111111111111111111111111", 11),
				fooR("123456789abcdef0000000000000000000000000000000000000000000000000", 12),
				fooR("5555555555555555555555555555555555555555555555555555555555555555", 13),
				fooR("8888888888888888888888888888888888888888888888888888888888888888", 14),
			},
			nil,
			[]KeyBytes{
				hexID("1111111111111111111111111111111111111111111111111111111111111111"),
				hexID("123456789abcdef0000000000000000000000000000000000000000000000000"),
				hexID("5555555555555555555555555555555555555555555555555555555555555555"),
				hexID("8888888888888888888888888888888888888888888888888888888888888888"),
			},
			startDate.Add(time.Minute),
			// no actual recent exchange happens due to the initial EmptySet message
			false,
			false,
			hashsync.WithRecentTimeSpan(48*time.Second),
		)
	})
	t.Run("empty to empty", func(t *testing.T) {
		verifyP2P(t, nil, nil, nil, startDate, false, false)
	})
	t.Run("random test", func(t *testing.T) {
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
		rowsA := make([]fooRow, nShared+nUniqueA)
		for i := range rowsA {
			h := types.RandomHash()
			k := KeyBytes(h[:])
			rowsA[i] = fooRow{
				id: k,
				ts: startDate.Add(time.Duration(i) * time.Second).UnixNano(),
			}
			combined = append(combined, k)
			// t.Logf("itemsA[%d] = %s", i, itemsA[i])
		}
		rowsB := make([]fooRow, nShared+nUniqueB)
		for i := range rowsB {
			if i < nShared {
				rowsB[i] = fooRow{
					id: slices.Clone(rowsA[i].id),
					ts: rowsA[i].ts,
				}
			} else {
				h := types.RandomHash()
				k := KeyBytes(h[:])
				rowsB[i] = fooRow{
					id: k,
					ts: startDate.Add(time.Duration(i) * time.Second).UnixNano(),
				}
				combined = append(combined, k)
			}
			// t.Logf("itemsB[%d] = %s", i, itemsB[i])
		}
		slices.SortFunc(combined, func(a, b KeyBytes) int {
			return a.Compare(b)
		})
		// for i, v := range combined {
		// 	t.Logf("combined[%d] = %s", i, v)
		// }
		verifyP2P(t, rowsA, rowsB, combined, startDate, false, false)
		// TODO: multiple iterations
	})
}

// QQQQQ: TBD empty sets with recent
