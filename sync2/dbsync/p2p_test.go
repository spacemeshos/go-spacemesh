package dbsync

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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

	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

var startDate = time.Date(2024, 8, 29, 18, 0, 0, 0, time.UTC)

type fooRow struct {
	id types.KeyBytes
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

var _ rangesync.Tracer = &syncTracer{}

func (tr *syncTracer) OnDumbSync() {
	// QQQQQQ: use mutex and also update handler_test.go in rangesync!!!!
	tr.dumb = true
}

func (tr *syncTracer) OnRecent(receivedItems, sentItems int) {
	tr.receivedItems += receivedItems
	tr.sentItems += sentItems
}

func verifyP2P(
	t *testing.T,
	rowsA, rowsB []fooRow,
	combinedItems []types.KeyBytes,
	clockAt time.Time,
	receivedRecent, sentRecent bool,
	opts ...rangesync.RangeSetReconcilerOption,
) {
	// nr := rangesync.RmmeNumRead()
	// nw := rangesync.RmmeNumWritten()
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
	setA := NewDBSet(dbA, st, 32, maxDepth)
	t.Logf("QQQQQ: 2.1")
	require.NoError(t, dbA.WithTx(ctx, func(tx sql.Transaction) error {
		return setA.EnsureLoaded(WithSQLExec(ctx, tx))
	}))
	t.Logf("QQQQQ: 3")
	setB := NewDBSet(dbB, st, 32, maxDepth)
	t.Logf("QQQQQ: 3.1")
	var x types.KeyBytes
	require.NoError(t, dbB.WithTx(ctx, func(tx sql.Transaction) error {
		ctx := WithSQLExec(ctx, tx)
		empty, err := setB.Empty(ctx)
		if err != nil {
			return fmt.Errorf("check if the set is empty: %w", err)
		}
		if empty {
			return nil
		}
		seq, err := setB.Items(ctx)
		if err != nil {
			return fmt.Errorf("get items: %w", err)
		}
		x = make(types.KeyBytes, 32)
		k, err := seq.First()
		if err != nil {
			return fmt.Errorf("get first item: %w", err)
		}
		v := load64(k.(types.KeyBytes)) & ^uint64(1<<(64-maxDepth)-1)
		binary.BigEndian.PutUint64(x[:], v)
		for i := 8; i < len(x); i++ {
			x[i] = 0
		}
		t.Logf("x: %s", x.String())
		return nil
	}))
	t.Logf("QQQQQ: 4")

	// QQQQQ: rmme
	// setB.s.ft.traceEnabled = true
	// setB.qqqqq = true
	// require.NoError(t, setB.s.EnsureLoaded())
	// var sb strings.Builder
	// setA.s.ft.dump(&sb)
	// t.Logf("setA:\n%s", sb.String())
	// sb = strings.Builder{}
	// setB.s.ft.dump(&sb)
	// t.Logf("setB:\n%s", sb.String())

	var tr syncTracer
	opts = append(opts,
		rangesync.WithClock(clockwork.NewFakeClockAt(clockAt)),
		rangesync.WithTracer(&tr),
	)
	opts = opts[:len(opts):len(opts)]

	srvPeerID := mesh.Hosts()[0].ID()
	srv := server.New(mesh.Hosts()[0], proto,
		func(ctx context.Context, req []byte, stream io.ReadWriter) error {
			pss := rangesync.NewPairwiseSetSyncer(nil, append(
				opts,
				rangesync.WithMaxSendRange(1),
				// uncomment to enable verbose logging which may slow down tests
				// rangesync.WithLogger(log.Named("sideA")),
			))
			return dbA.WithTx(ctx, func(tx sql.Transaction) error {
				return pss.Serve(WithSQLExec(ctx, tx), req, stream, setA)
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

	pss := rangesync.NewPairwiseSetSyncer(client, append(
		opts,
		rangesync.WithMaxSendRange(1),
		// uncomment to enable verbose logging which may slow down tests
		// rangesync.WithLogger(log.Named("sideB")),
	))

	tStart := time.Now()
	require.NoError(t, dbB.WithTx(ctx, func(tx sql.Transaction) error {
		return pss.Sync(WithSQLExec(ctx, tx), srvPeerID, setB, x, x)
	}))
	t.Logf("synced in %v", time.Since(tStart))
	// t.Logf("bytes read: %d, bytes written: %d", rangesync.RmmeNumRead()-nr, rangesync.RmmeNumWritten()-nw)

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
	var actItemsA []types.KeyBytes
	require.NoError(t, dbA.WithTx(ctx, func(tx sql.Transaction) error {
		seq, err := setA.Items(WithSQLExec(ctx, tx))
		require.NoError(t, err)
		if l := len(combinedItems); l == 0 {
			requireEmpty(t, seq)
		} else {
			collected, err := types.GetN[types.KeyBytes](seq, l+1)
			require.NoError(t, err)
			actItemsA = collected[:l]
			assert.Equal(t, actItemsA[0], collected[l]) // verify wraparound
		}
		return nil
	}))

	var actItemsB []types.KeyBytes
	require.NoError(t, dbB.WithTx(ctx, func(tx sql.Transaction) error {
		seq, err := setB.Items(WithSQLExec(ctx, tx))
		require.NoError(t, err)
		if l := len(combinedItems); l == 0 {
			requireEmpty(t, seq)
		} else {
			collected, err := types.GetN[types.KeyBytes](seq, l+1)
			require.NoError(t, err)
			actItemsB = collected[:l]
			assert.Equal(t, actItemsB[0], collected[l]) // verify wraparound
		}
		return nil
	}))
	assert.Equal(t, combinedItems, actItemsA)
	assert.Equal(t, actItemsA, actItemsB)
}

func fooR(id string, seconds int) fooRow {
	return fooRow{
		types.HexToKeyBytes(id),
		startDate.Add(time.Duration(seconds) * time.Second).UnixNano(),
	}
}

func TestP2P(t *testing.T) {
	hexID := types.HexToKeyBytes
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
			[]types.KeyBytes{
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
				fooR("0e69888877324da35693decc7ded1b2bac16d394ced869af494568d66473a6f0", 10),
				fooR("3a78db9e386493402561d9c6f69a6b434a62388f61d06d960598ebf29a3a2187", 20),
				fooR("66c9aa8f3be7da713db66e56cc165a46764f88d3113244dd5964bb0a10ccacc3", 30),
				fooR("72e1adaaf140d809a5da325a197341a453b00807ef8d8995fd3c8079b917c9d7", 40),
				fooR("782c24553b0a8cf1d95f632054b7215be192facfb177cfd1312901dd4c9e0bfd", 50),
				fooR("9e11fdb099f1118144738f9b68ca601e74b97280fd7bbc97cfc377f432e9b7b5", 60),
			},
			[]fooRow{
				fooR("0e69888877324da35693decc7ded1b2bac16d394ced869af494568d66473a6f0", 11),
				fooR("3a78db9e386493402561d9c6f69a6b434a62388f61d06d960598ebf29a3a2187", 12),
				fooR("66c9aa8f3be7da713db66e56cc165a46764f88d3113244dd5964bb0a10ccacc3", 13),
				fooR("90b25f2d1ee9c9e2d20df5f2226d14ee4223ea27ba565a49aa66a9c44a51c241", 14),
				fooR("9e11fdb099f1118144738f9b68ca601e74b97280fd7bbc97cfc377f432e9b7b5", 15),
				fooR("c1690e47798295cca02392cbfc0a86cb5204878c04a29b3ae7701b6b51681128", 16),
			},
			[]types.KeyBytes{
				hexID("0e69888877324da35693decc7ded1b2bac16d394ced869af494568d66473a6f0"),
				hexID("3a78db9e386493402561d9c6f69a6b434a62388f61d06d960598ebf29a3a2187"),
				hexID("66c9aa8f3be7da713db66e56cc165a46764f88d3113244dd5964bb0a10ccacc3"),
				hexID("72e1adaaf140d809a5da325a197341a453b00807ef8d8995fd3c8079b917c9d7"),
				hexID("782c24553b0a8cf1d95f632054b7215be192facfb177cfd1312901dd4c9e0bfd"),
				hexID("90b25f2d1ee9c9e2d20df5f2226d14ee4223ea27ba565a49aa66a9c44a51c241"),
				hexID("9e11fdb099f1118144738f9b68ca601e74b97280fd7bbc97cfc377f432e9b7b5"),
				hexID("c1690e47798295cca02392cbfc0a86cb5204878c04a29b3ae7701b6b51681128"),
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
			[]types.KeyBytes{
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
			rangesync.WithRecentTimeSpan(48*time.Second),
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
			[]types.KeyBytes{
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
			[]types.KeyBytes{
				hexID("1111111111111111111111111111111111111111111111111111111111111111"),
				hexID("123456789abcdef0000000000000000000000000000000000000000000000000"),
				hexID("5555555555555555555555555555555555555555555555555555555555555555"),
				hexID("8888888888888888888888888888888888888888888888888888888888888888"),
			},
			startDate.Add(time.Minute),
			true,
			true,
			rangesync.WithRecentTimeSpan(48*time.Second),
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
			[]types.KeyBytes{
				hexID("1111111111111111111111111111111111111111111111111111111111111111"),
				hexID("123456789abcdef0000000000000000000000000000000000000000000000000"),
				hexID("5555555555555555555555555555555555555555555555555555555555555555"),
				hexID("8888888888888888888888888888888888888888888888888888888888888888"),
			},
			startDate.Add(time.Minute),
			// no actual recent exchange happens due to the initial EmptySet message
			false,
			false,
			rangesync.WithRecentTimeSpan(48*time.Second),
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
		// const nShared = 2
		// const nUniqueA = 2
		// const nUniqueB = 2
		const nShared = 80000
		const nUniqueA = 400
		const nUniqueB = 800

		combined := make([]types.KeyBytes, 0, nShared+nUniqueA+nUniqueB)
		rowsA := make([]fooRow, nShared+nUniqueA)
		for i := range rowsA {
			k := types.RandomKeyBytes(32)
			rowsA[i] = fooRow{
				id: k,
				ts: startDate.Add(time.Duration(i) * time.Second).UnixNano(),
			}
			combined = append(combined, k)
			// t.Logf("itemsA[%d] = %s", i, rowsA[i].id)
		}
		rowsB := make([]fooRow, nShared+nUniqueB)
		for i := range rowsB {
			if i < nShared {
				rowsB[i] = fooRow{
					id: slices.Clone(rowsA[i].id),
					ts: rowsA[i].ts,
				}
			} else {
				k := types.RandomKeyBytes(32)
				rowsB[i] = fooRow{
					id: k,
					ts: startDate.Add(time.Duration(i) * time.Second).UnixNano(),
				}
				combined = append(combined, k)
			}
			// t.Logf("itemsB[%d] = %s", i, rowsB[i].id)
		}
		slices.SortFunc(combined, func(a, b types.KeyBytes) int {
			return a.Compare(b)
		})
		// for i, v := range combined {
		// 	t.Logf("combined[%d] = %s", i, v)
		// }
		verifyP2P(t, rowsA, rowsB, combined, startDate, false, false)
		// TODO: multiple iterations
	})
}
