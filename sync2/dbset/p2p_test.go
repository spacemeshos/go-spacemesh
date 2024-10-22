package dbset_test

import (
	"context"
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

	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/dbset"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

var startDate = time.Date(2024, 8, 29, 18, 0, 0, 0, time.UTC)

type fooRow struct {
	id rangesync.KeyBytes
	ts int64
}

func insertRow(t *testing.T, db sql.Executor, row fooRow) {
	_, err := db.Exec(
		"insert into foo (id, received) values (?, ?)",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, row.id)
			stmt.BindInt64(2, row.ts)
		}, nil)
	require.NoError(t, err)
}

func populateFoo(t *testing.T, rows []fooRow) sql.Database {
	db := sql.InMemoryTest(t)
	require.NoError(t, db.WithTx(context.Background(), func(tx sql.Transaction) error {
		_, err := tx.Exec(
			"create table foo(id char(32) not null primary key, received int)",
			nil, nil)
		require.NoError(t, err)
		for _, row := range rows {
			insertRow(t, tx, row)
		}
		return nil
	}))
	return db
}

type syncTracer struct {
	receivedItems int
	sentItems     int
}

var _ rangesync.Tracer = &syncTracer{}

func (tr *syncTracer) OnDumbSync() {}

func (tr *syncTracer) OnRecent(receivedItems, sentItems int) {
	tr.receivedItems += receivedItems
	tr.sentItems += sentItems
}

func addReceived(t *testing.T, db sql.Executor, to, from *dbset.DBSet) {
	sr := from.Received()
	for k := range sr.Seq {
		has, err := to.Has(k)
		require.NoError(t, err)
		if !has {
			insertRow(t, db, fooRow{id: k, ts: time.Now().UnixNano()})
		}
	}
	require.NoError(t, sr.Error())
	require.NoError(t, to.Advance())
}

func verifyP2P(
	t *testing.T,
	rowsA, rowsB []fooRow,
	combinedItems []rangesync.KeyBytes,
	clockAt time.Time,
	receivedRecent, sentRecent bool,
	opts ...rangesync.RangeSetReconcilerOption,
) {
	const maxDepth = 24
	log := zaptest.NewLogger(t)
	dbAx := populateFoo(t, rowsA)
	dbA, err := dbAx.Tx(context.Background())
	require.NoError(t, err)
	defer dbA.Release()
	dbBx := populateFoo(t, rowsB)
	dbB, err := dbBx.Tx(context.Background())
	require.NoError(t, err)
	defer dbB.Release()
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)
	proto := "itest"
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	st := &sqlstore.SyncedTable{
		TableName:       "foo",
		IDColumn:        "id",
		TimestampColumn: "received",
	}

	setA := dbset.NewDBSet(dbA, st, testKeyLen, maxDepth)
	loadStart := time.Now()
	require.NoError(t, setA.EnsureLoaded())
	t.Logf("loaded setA in %v", time.Since(loadStart))

	setB := dbset.NewDBSet(dbB, st, testKeyLen, maxDepth)
	loadStart = time.Now()
	require.NoError(t, setB.EnsureLoaded())
	t.Logf("loaded setB in %v", time.Since(loadStart))

	empty, err := setB.Empty()
	require.NoError(t, err)
	var x rangesync.KeyBytes
	if !empty {
		k, err := setB.Items().First()
		require.NoError(t, err)
		x := k.Clone()
		x.Trim(maxDepth)
	}

	var tr syncTracer
	opts = append(opts,
		rangesync.WithClock(clockwork.NewFakeClockAt(clockAt)),
		rangesync.WithTracer(&tr),
	)
	opts = opts[:len(opts):len(opts)]

	srvPeerID := mesh.Hosts()[0].ID()
	pssA := rangesync.NewPairwiseSetSyncer(nil, "test", append(
		opts,
		rangesync.WithMaxSendRange(1),
		// uncomment to enable verbose logging which may slow down tests
		// rangesync.WithLogger(log.Named("sideA")),
	), nil)
	d := rangesync.NewDispatcher(log)
	syncSetA := setA.Copy(false).(*dbset.DBSet)
	pssA.Register(d, syncSetA)
	srv := server.New(mesh.Hosts()[0], proto,
		func(ctx context.Context, req []byte, stream io.ReadWriter) error {
			return d.Dispatch(ctx, req, stream)
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

	require.Eventually(t, func() bool {
		for _, h := range mesh.Hosts() {
			if len(h.Mux().Protocols()) == 0 {
				return false
			}
		}
		return true
	}, time.Second, 10*time.Millisecond)

	pssB := rangesync.NewPairwiseSetSyncer(client, "test", append(
		opts,
		rangesync.WithMaxSendRange(1),
		// uncomment to enable verbose logging which may slow down tests
		// rangesync.WithLogger(log.Named("sideB")),
	), nil)

	tStart := time.Now()
	syncSetB := setB.Copy(false).(*dbset.DBSet)
	require.NoError(t, pssB.Sync(ctx, srvPeerID, syncSetB, x, x))
	t.Logf("synced in %v, sent %d, recv %d", time.Since(tStart), pssB.Sent(), pssB.Received())
	addReceived(t, dbA, setA, syncSetA)
	addReceived(t, dbB, setB, syncSetB)

	require.Equal(t, receivedRecent, tr.receivedItems > 0)
	require.Equal(t, sentRecent, tr.sentItems > 0)

	if len(combinedItems) == 0 {
		return
	}

	actItemsA, err := setA.Items().Collect()
	require.NoError(t, err)

	actItemsB, err := setB.Items().Collect()
	require.NoError(t, err)

	assert.Equal(t, combinedItems, actItemsA)
	assert.Equal(t, actItemsA, actItemsB)
}

func fooR(id string, seconds int) fooRow {
	return fooRow{
		rangesync.MustParseHexKeyBytes(id),
		startDate.Add(time.Duration(seconds) * time.Second).UnixNano(),
	}
}

func TestP2P(t *testing.T) {
	hexID := rangesync.MustParseHexKeyBytes
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
			[]rangesync.KeyBytes{
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
			[]rangesync.KeyBytes{
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
	t.Run("predefined items 3", func(t *testing.T) {
		verifyP2P(
			t, []fooRow{
				fooR("08addda193ce5c8dfa56d58efaaaa51ccb534738027c4c73631f76811702e54f", 5),
				fooR("112d34ac1724faa17502e9f1654808daa43d8e99c384c42faeccc6c713993079", 3),
				fooR("8599b0264623ede5d198fd2caa537720e011ce17bd9f34c140de269f472a1126", 4),
				fooR("9e8dc977998b3cbc30071202cb8ebb0c8bfa2c400fd28067f6d43c7e92acd077", 2),
				fooR("a67249d334bd0c68e92b4c6d8716cdc218130c0e765838e52890133d07d35d48", 0),
				fooR("e7f3c0ecf1410711cf16d8188dc0075f10d17c95208bdbf7a5c910a0ecb68085", 1),
			},
			[]fooRow{
				fooR("112d34ac1724faa17502e9f1654808daa43d8e99c384c42faeccc6c713993079", 3),
				fooR("8599b0264623ede5d198fd2caa537720e011ce17bd9f34c140de269f472a1126", 4),
				fooR("9e8dc977998b3cbc30071202cb8ebb0c8bfa2c400fd28067f6d43c7e92acd077", 2),
				fooR("a67249d334bd0c68e92b4c6d8716cdc218130c0e765838e52890133d07d35d48", 0),
				fooR("dc5938b62a49a31e947d48d85cf358a77dbbed0f3ad5d06e2df63da3cbe7c80a", 5),
				fooR("e7f3c0ecf1410711cf16d8188dc0075f10d17c95208bdbf7a5c910a0ecb68085", 1),
			},
			[]rangesync.KeyBytes{
				hexID("08addda193ce5c8dfa56d58efaaaa51ccb534738027c4c73631f76811702e54f"),
				hexID("112d34ac1724faa17502e9f1654808daa43d8e99c384c42faeccc6c713993079"),
				hexID("8599b0264623ede5d198fd2caa537720e011ce17bd9f34c140de269f472a1126"),
				hexID("9e8dc977998b3cbc30071202cb8ebb0c8bfa2c400fd28067f6d43c7e92acd077"),
				hexID("a67249d334bd0c68e92b4c6d8716cdc218130c0e765838e52890133d07d35d48"),
				hexID("dc5938b62a49a31e947d48d85cf358a77dbbed0f3ad5d06e2df63da3cbe7c80a"),
				hexID("e7f3c0ecf1410711cf16d8188dc0075f10d17c95208bdbf7a5c910a0ecb68085"),
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
			[]rangesync.KeyBytes{
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
			[]rangesync.KeyBytes{
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
			[]rangesync.KeyBytes{
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
			[]rangesync.KeyBytes{
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
		// higher values for "stress testing":
		// const nShared = 8000000
		// const nUniqueA = 100
		// const nUniqueB = 80000
		const nShared = 80000
		const nUniqueA = 400
		const nUniqueB = 800

		combined := make([]rangesync.KeyBytes, 0, nShared+nUniqueA+nUniqueB)
		rowsA := make([]fooRow, nShared+nUniqueA)
		for i := range rowsA {
			k := rangesync.RandomKeyBytes(testKeyLen)
			rowsA[i] = fooRow{
				id: k,
				ts: startDate.Add(time.Duration(i) * time.Second).UnixNano(),
			}
			combined = append(combined, k)
		}
		rowsB := make([]fooRow, nShared+nUniqueB)
		for i := range rowsB {
			if i < nShared {
				rowsB[i] = fooRow{
					id: slices.Clone(rowsA[i].id),
					ts: rowsA[i].ts,
				}
			} else {
				k := rangesync.RandomKeyBytes(testKeyLen)
				rowsB[i] = fooRow{
					id: k,
					ts: startDate.Add(time.Duration(i) * time.Second).UnixNano(),
				}
				combined = append(combined, k)
			}
		}
		slices.SortFunc(combined, func(a, b rangesync.KeyBytes) int {
			return a.Compare(b)
		})
		verifyP2P(t, rowsA, rowsB, combined, startDate, false, false)
	})
}
