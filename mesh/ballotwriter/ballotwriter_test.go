package ballotwriter_test

import (
	"context"
	"errors"
	"fmt"
	"os"
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
)

var testLayer = types.LayerID(5)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(10)
	res := m.Run()
	os.Exit(res)
}

func TestWriteCoalesce_One(t *testing.T) {
	w, db := newTestBallotWriter(t)
	ballot := genBallot(t)
	err := w.Store(ballot)
	require.NoError(t, err)
	has, err := ballots.Has(db, ballot.ID())
	require.True(t, has)
	require.NoError(t, err)
}

// TestWriteCoalesce_TwoPerSmesher tests that we accept two ballots
// for a smesher (and makes sure that the malfeasance circuit works).
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
	err = w.Store(b)
	require.NoError(t, err)
	err = w.Store(b1)
	require.NoError(t, err)
	has, err := ballots.Has(db, b.ID())
	require.True(t, has)
	require.NoError(t, err)
	has, err = ballots.Has(db, b1.ID())
	require.True(t, has)
	require.NoError(t, err)
	err = w.Store(b1)
	require.NoError(t, err)
	// check that a corresponding malfeasance proof has been stored
	var blob sql.Blob
	err = identities.LoadMalfeasanceBlob(context.Background(), db, b.SmesherID.Bytes(), &blob)
	require.NoError(t, err)
	require.NotNil(t, blob.Bytes)
}

func BenchmarkWriteCoalescing(b *testing.B) {
	a := make([]*types.Ballot, 1000000)
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

	b.Run("No Coalescing", func(b *testing.B) {
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

	b.Run("Coalescing 1000 entries", func(b *testing.B) {
		db := newDiskSqlite(b)
		b.ResetTimer()
		for j := 0; j < b.N/1000; j++ {
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

	b.Run("Coalescing 5000 entries", func(b *testing.B) {
		db := newDiskSqlite(b)
		b.ResetTimer()
		for j := 0; j < b.N/5000; j++ {
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

	dir := tb.TempDir()
	sqlDB, err := statesql.Open("file:" + filepath.Join(dir, "state.sql"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}
