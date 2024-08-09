package atxwriter_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation/atxwriter"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql/migrations"
)

var (
	postGenesisEpoch types.EpochID = 2
	goldenATXID                    = types.RandomATXID()
	wAtx                           = newInitialATXv1(goldenATXID)
	atx                            = toAtx(wAtx)
)

func TestWriteCoalesce_One(t *testing.T) {
	w, db := newTestAtxWriter(t)

	ch, errfn := w.Store(atx, wAtx)
	var err error
	select {
	case <-ch:
		err = errfn()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	require.NoError(t, err)
	has, err := atxs.Has(db, atx.ID())
	require.True(t, has)
	require.NoError(t, err)
}

func TestWriteCoalesce_Duplicates(t *testing.T) {
	w, db := newTestAtxWriter(t)

	ch, errfn := w.Store(atx, wAtx)
	_, _ = w.Store(atx, wAtx)
	var err error
	select {
	case <-ch:
		err = errfn()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	require.NoError(t, err)
	has, err := atxs.Has(db, atx.ID())
	require.True(t, has)
	require.NoError(t, err)
}

func TestWriteCoalesce_MultipleBatches(t *testing.T) {
	w, db := newTestAtxWriter(t)

	ch, errfn := w.Store(atx, wAtx)
	var err error
	select {
	case <-ch:
		err = errfn()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	require.NoError(t, err)
	has, err := atxs.Has(db, atx.ID())
	require.True(t, has)
	require.NoError(t, err)

	wAtx2 := newInitialATXv1(types.RandomATXID())
	atx2 := toAtx(wAtx)

	ch, errfn = w.Store(atx2, wAtx2)
	select {
	case <-ch:
		err = errfn()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	require.NoError(t, err)
	has, err = atxs.Has(db, atx2.ID())
	require.True(t, has)
	require.NoError(t, err)
}

func BenchmarkWriteCoalesing(b *testing.B) {
	a := make([]atxwriter.AtxBatchItem, 100000)
	for i := 0; i < len(a); i++ {
		goldenATXID := types.RandomATXID()
		wAtx := newInitialATXv1(goldenATXID)
		atx := toAtx(wAtx)
		a[i] = atxwriter.AtxBatchItem{Atx: atx, Watx: wAtx}
	}

	b.ResetTimer()
	b.Run("No Coalesing", func(b *testing.B) {
		db := newDiskSqlite(b)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := db.WithTx(context.Background(), func(tx *sql.Tx) error {
				var err error
				err = atxs.Add(tx, a[i].Atx, a[i].Watx.Blob())
				if err != nil {
					b.Fatal(err)
				}
				err = atxs.SetPost(tx, a[i].Atx.ID(), a[i].Watx.PrevATXID, 0,
					a[i].Atx.SmesherID, a[i].Watx.NumUnits)
				if err != nil {
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
			if err := db.WithTx(context.Background(), func(tx *sql.Tx) error {
				var err error
				for i := (j * 1000); i < (j*1000)+1000; i++ {
					err = atxs.Add(tx, a[i].Atx, a[i].Watx.Blob())
					if err != nil {
						b.Fatal(err)
					}
					err = atxs.SetPost(tx, a[i].Atx.ID(), a[i].Watx.PrevATXID, 0,
						a[i].Atx.SmesherID, a[i].Watx.NumUnits)
					if err != nil {
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
			if err := db.WithTx(context.Background(), func(tx *sql.Tx) error {
				var err error
				for i := (j * 5000); i < (j*5000)+5000; i++ {
					err = atxs.Add(tx, a[i].Atx, a[i].Watx.Blob())
					if err != nil {
						b.Fatal(err)
					}
					err = atxs.SetPost(tx, a[i].Atx.ID(), a[i].Watx.PrevATXID, 0,
						a[i].Atx.SmesherID, a[i].Watx.NumUnits)
					if err != nil {
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

func toAtx(watx *wire.ActivationTxV1) *types.ActivationTx {
	atx := wire.ActivationTxFromWireV1(watx)
	atx.SetReceived(time.Now())
	atx.BaseTickHeight = uint64(atx.PublishEpoch)
	atx.TickCount = 1
	return atx
}

func newTestAtxWriter(t testing.TB) (*atxwriter.AtxWriter, *sql.Database) {
	t.Helper()
	db := sql.InMemoryTest(t)
	log := zaptest.NewLogger(t)
	w := atxwriter.New(db, log)
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

func newDiskSqlite(tb testing.TB) *sql.Database {
	tb.Helper()
	m21 := migrations.New0021Migration(zap.NewNop(), 1_000_000)
	migrations, err := sql.StateMigrations()
	if err != nil {
		tb.Fatal(err)
	}
	dbopts := []sql.Opt{
		sql.WithMigrations(migrations),
		sql.WithMigration(m21),
	}
	dir := tb.TempDir()
	sqlDB, err := sql.Open("file:"+filepath.Join(dir, "sql.sql"), dbopts...)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

func newInitialATXv1(
	goldenATXID types.ATXID,
	opts ...func(*wire.ActivationTxV1),
) *wire.ActivationTxV1 {
	nonce := uint64(999)
	poetRef := types.RandomHash()
	atx := &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				PrevATXID:        types.EmptyATXID,
				PublishEpoch:     postGenesisEpoch,
				PositioningATXID: goldenATXID,
				CommitmentATXID:  &goldenATXID,
				InitialPost:      &wire.PostV1{},
			},
			NIPost:   newNIPostV1WithPoet(poetRef.Bytes()),
			VRFNonce: &nonce,
			Coinbase: types.GenerateAddress([]byte("aaaa")),
			NumUnits: 100,
		},
	}
	for _, opt := range opts {
		opt(atx)
	}
	return atx
}

func newMerkleProof(leafs []types.Hash32) (types.MerkleProof, types.Hash32) {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(shared.HashMembershipTreeNode).
		WithLeavesToProve(map[uint64]bool{0: true}).
		Build()
	if err != nil {
		panic(err)
	}
	for _, m := range leafs {
		if err := tree.AddLeaf(m[:]); err != nil {
			panic(err)
		}
	}
	root, nodes := tree.RootAndProof()
	nodesH32 := make([]types.Hash32, 0, len(nodes))
	for _, n := range nodes {
		nodesH32 = append(nodesH32, types.BytesToHash(n))
	}
	return types.MerkleProof{
		Nodes: nodesH32,
	}, types.BytesToHash(root)
}

func newNIPostV1WithPoet(poetRef []byte) *wire.NIPostV1 {
	proof, _ := newMerkleProof([]types.Hash32{
		types.BytesToHash([]byte("challenge")),
		types.BytesToHash([]byte("leaf2")),
		types.BytesToHash([]byte("leaf3")),
		types.BytesToHash([]byte("leaf4")),
	})

	return &wire.NIPostV1{
		Membership: wire.MerkleProofV1{
			Nodes:     proof.Nodes,
			LeafIndex: 0,
		},
		Post: &wire.PostV1{
			Nonce:   0,
			Indices: []byte{1, 2, 3},
			Pow:     0,
		},
		PostMetadata: &wire.PostMetadataV1{
			Challenge: poetRef,
		},
	}
}
