package ballotwriter_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/mesh/ballotwriter"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql/migrations"
)

func init() {
	// otherwise we get a divide by zero error
	types.SetLayersPerEpoch(1)
}

var testLayer = types.LayerID(5)

func TestWriteCoalesce_One(t *testing.T) {
	w, db := newTestBallotWriter(t)
	ballot := genBallot(t)
	ch, errfn, retry := w.Store(ballot)
	require.Nil(t, retry)
	var err error
	select {
	case <-ch:
		err = errfn()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	require.NoError(t, err)
	has, err := ballots.Has(db, ballot.ID())
	require.True(t, has)
	require.NoError(t, err)
}

// TestWriteCoalesce_OnePerSmesher tests that we won't accept two ballots
// from the same smesher in the same batch. This also implicitly tests
// for writing two different batches.
func TestWriteCoalesce_OnePerSmesher(t *testing.T) {
	w, db := newTestBallotWriter(t)

	b := types.RandomBallot()
	b1 := types.RandomBallot()
	b.Layer = testLayer
	b1.Layer = testLayer
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	b.Signature = sig.Sign(signing.BALLOT, b.SignedBytes())
	b1.Signature = sig.Sign(signing.BALLOT, b.SignedBytes())
	b.SmesherID = sig.NodeID()
	b1.SmesherID = sig.NodeID()
	require.NoError(t, b.Initialize())
	require.NoError(t, b1.Initialize())
	ch, errfn, retry := w.Store(b)
	require.Nil(t, retry)
	ch1, errfn1, retry1 := w.Store(b1)
	require.Nil(t, ch1)
	require.Nil(t, errfn1)
	require.NotNil(t, retry1)
	select {
	case <-ch:
		err = errfn()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	require.NoError(t, err)
	has, err := ballots.Has(db, b.ID())
	require.True(t, has)
	require.NoError(t, err)
	has, err = ballots.Has(db, b1.ID())
	require.False(t, has)
	require.NoError(t, err)
	retry1()
	ch1, errfn1, retry1 = w.Store(b1)
	require.NotNil(t, ch1)
	require.NotNil(t, errfn1)
	require.Nil(t, retry1)
	select {
	case <-ch1:
		err = errfn1()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	require.NoError(t, err)
	// check that a corresponding malfeasance proof has been stored
	proof, err := identities.GetMalfeasanceProof(db, b.SmesherID)
	require.NoError(t, err)
	require.NotNil(t, proof)
}

func BenchmarkWriteCoalesing(b *testing.B) {
	a := make([]*types.Ballot, 100000)
	for i := 0; i < len(a); i++ {
		a[i] = genBallot(b)
	}
	writeFn := func(ballot *types.Ballot, tx sql.Transaction) error {
		if !ballot.IsMalicious() {
			prev, err := ballots.LayerBallotByNodeID(tx, ballot.Layer, ballot.SmesherID)
			if err != nil && !errors.Is(err, sql.ErrNotFound) {
				return err
			}
			if prev != nil && prev.ID() != ballot.ID() {
				var ballotProof wire.BallotProof
				for i, b := range []*types.Ballot{prev, ballot} {
					ballotProof.Messages[i] = wire.BallotProofMsg{
						InnerMsg: types.BallotMetadata{
							Layer:   b.Layer,
							MsgHash: types.BytesToHash(b.HashInnerBytes()),
						},
						Signature: b.Signature,
						SmesherID: b.SmesherID,
					}
				}
				proof := &wire.MalfeasanceProof{
					Layer: ballot.Layer,
					Proof: wire.Proof{
						Type: wire.MultipleBallots,
						Data: &ballotProof,
					},
				}
				encoded := codec.MustEncode(proof)
				if err := identities.SetMalicious(tx, ballot.SmesherID, encoded, time.Now()); err != nil {
					return fmt.Errorf("add malfeasance proof: %w", err)
				}
				ballot.SetMalicious()
			}
		}
		if err := ballots.Add(tx, ballot); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return err
		}

		return nil
	}
	b.ResetTimer()
	b.Run("No Coalesing", func(b *testing.B) {
		db := newDiskSqlite(b)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := db.WithTx(context.Background(), func(tx sql.Transaction) error {
				if err := writeFn(a[i], tx); err != nil {
					b.Fatal(err)
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}
		}
	})

	// with the coalesing tests, one must take the "ns/op" metrics and divide it
	// by the number of entries written together to see how many items we're doing
	// per time unit.
	b.Run("Coalesing 1000 entries", func(b *testing.B) {
		db := newDiskSqlite(b)
		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			if err := db.WithTx(context.Background(), func(tx sql.Transaction) error {
				var err error
				for i := (j * 1000); i < (j*1000)+1000; i++ {
					if err = writeFn(a[i], tx); err != nil {
						b.Fatal(err)
					}
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Coalesing 5000 entries", func(b *testing.B) {
		db := newDiskSqlite(b)
		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			if err := db.WithTx(context.Background(), func(tx sql.Transaction) error {
				var err error
				for i := (j * 5000); i < (j*5000)+5000; i++ {
					if err = writeFn(a[i], tx); err != nil {
						b.Fatal(err)
					}
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func genBallot(tb testing.TB) *types.Ballot {
	b := types.RandomBallot()

	b.Layer = testLayer
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	b.Signature = sig.Sign(signing.BALLOT, b.SignedBytes())
	b.SmesherID = sig.NodeID()
	require.NoError(tb, b.Initialize())
	return b
}

func newTestBallotWriter(t testing.TB) (*ballotwriter.BallotWriter, sql.Database) {
	t.Helper()
	db := statesql.InMemoryTest(t)
	log := zaptest.NewLogger(t)
	w := ballotwriter.New(db, log)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		<-done
	})
	go func() {
		defer close(done)
		w.Start(ctx)
	}()
	return w, db
}

func newDiskSqlite(tb testing.TB) sql.Database {
	tb.Helper()
	schema, err := migrations.SchemaWithInCodeMigrations()
	require.NoError(tb, err)

	dbopts := []sql.Opt{
		sql.WithDatabaseSchema(schema),
		sql.WithForceMigrations(true),
	}
	dir := tb.TempDir()
	sqlDB, err := sql.Open("file:"+filepath.Join(dir, "sql.sql"), dbopts...)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}
