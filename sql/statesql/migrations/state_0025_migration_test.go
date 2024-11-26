package migrations

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test0025Migration(t *testing.T) {
	setup := func(tb testing.TB) sql.StateDatabase {
		schema, err := statesql.Schema()
		require.NoError(tb, err)
		schema.Migrations = slices.DeleteFunc(schema.Migrations, func(m sql.Migration) bool {
			return m.Order() >= 25
		})
		db := statesql.InMemoryTest(
			tb,
			sql.WithDatabaseSchema(schema),
			sql.WithNoCheckSchemaDrift(),
			sql.WithForceMigrations(true),
		)
		return db
	}

	t.Run("non InvalidPrevATX proofs are ignored", func(t *testing.T) {
		db := setup(t)

		// store proofs of different types
		proofs := make(map[types.NodeID]*mwire.MalfeasanceProof)
		nodeID := types.RandomNodeID()
		proof := &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.MultipleATXs,
				Data: &mwire.AtxProof{},
			},
		}
		require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))
		proofs[nodeID] = proof

		nodeID = types.RandomNodeID()
		proof = &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.MultipleBallots,
				Data: &mwire.BallotProof{},
			},
		}
		require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))
		proofs[nodeID] = proof

		nodeID = types.RandomNodeID()
		proof = &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.HareEquivocation,
				Data: &mwire.HareProof{},
			},
		}
		require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))
		proofs[nodeID] = proof

		nodeID = types.RandomNodeID()
		proof = &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.InvalidPostIndex,
				Data: &mwire.InvalidPostIndexProof{},
			},
		}
		require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))
		proofs[nodeID] = proof

		m := New0025Migration(config.MainnetConfig())
		require.Equal(t, 25, m.Order())
		require.NoError(t, m.Apply(db, zaptest.NewLogger(t)))

		for nodeID, proof := range proofs {
			blob := &sql.Blob{}
			err := identities.LoadMalfeasanceBlob(context.Background(), db, nodeID.Bytes(), blob)
			require.NoError(t, err)
			got := &mwire.MalfeasanceProof{}
			codec.MustDecode(blob.Bytes, got)
			require.Equal(t, proof, got)
		}
	})

	t.Run("valid proof is ignored", func(t *testing.T) {
		db := setup(t)

		// store valid proof
		cfg := config.MainnetConfig()
		sig, err := signing.NewEdSigner(
			signing.WithPrefix(cfg.Genesis.GenesisID().Bytes()),
		)
		require.NoError(t, err)

		goldenATX := types.RandomATXID()
		atx1 := newInitialATXv1(t, goldenATX, &goldenATX)
		atx1.Sign(sig)
		atx2 := newInitialATXv1(t, goldenATX, &goldenATX)
		atx2.Sign(sig)

		proof := &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.InvalidPrevATX,
				Data: &mwire.InvalidPrevATXProof{
					Atx1: *atx1,
					Atx2: *atx2,
				},
			},
		}
		require.NoError(t, identities.SetMalicious(db, sig.NodeID(), codec.MustEncode(proof), time.Now()))

		observer, observedLogs := observer.New(zapcore.DebugLevel)
		logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
			func(core zapcore.Core) zapcore.Core {
				return zapcore.NewTee(core, observer)
			},
		)))

		m := New0025Migration(cfg)
		require.Equal(t, 25, m.Order())
		require.NoError(t, m.Apply(db, logger))

		require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
		require.Equal(t, zapcore.DebugLevel, observedLogs.All()[0].Level)
		require.Equal(t, "Proof is valid", observedLogs.All()[0].Message)
		require.Equal(t, sig.NodeID().ShortString(), observedLogs.All()[0].ContextMap()["smesherID"])

		// check proof was not changed
		blob := &sql.Blob{}
		require.NoError(t, identities.LoadMalfeasanceBlob(context.Background(), db, sig.NodeID().Bytes(), blob))
		got := &mwire.MalfeasanceProof{}
		codec.MustDecode(blob.Bytes, got)

		require.Equal(t, proof, got)
	})

	t.Run("invalid proof is fixed", func(t *testing.T) {
		db := setup(t)

		// store invalid proof
		cfg := config.MainnetConfig()
		sig, err := signing.NewEdSigner(
			signing.WithPrefix(cfg.Genesis.GenesisID().Bytes()),
		)
		require.NoError(t, err)
		verifier := signing.NewEdVerifier(
			signing.WithVerifierPrefix(cfg.Genesis.GenesisID().Bytes()),
		)

		goldenATX := types.RandomATXID()
		atx1 := newInitialATXv1(t, goldenATX, &goldenATX)
		atx1.Sign(sig)
		atx1.VRFNonce = new(uint64)
		*atx1.VRFNonce = 1337 // broken proof because we set VRFNonce before storing it
		require.False(t, verifier.Verify(signing.ATX, atx1.SmesherID, atx1.SignedBytes(), atx1.Signature))

		atx2 := newInitialATXv1(t, goldenATX, &goldenATX)
		atx2.Sign(sig)
		require.True(t, verifier.Verify(signing.ATX, atx2.SmesherID, atx2.SignedBytes(), atx2.Signature))

		proof := &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.InvalidPrevATX,
				Data: &mwire.InvalidPrevATXProof{
					Atx1: *atx1,
					Atx2: *atx2,
				},
			},
		}
		require.NoError(t, identities.SetMalicious(db, sig.NodeID(), codec.MustEncode(proof), time.Now()))

		observer, observedLogs := observer.New(zapcore.InfoLevel)
		logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
			func(core zapcore.Core) zapcore.Core {
				return zapcore.NewTee(core, observer)
			},
		)))

		m := New0025Migration(cfg)
		require.Equal(t, 25, m.Order())
		require.NoError(t, m.Apply(db, logger))

		require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
		require.Equal(t, zapcore.InfoLevel, observedLogs.All()[0].Level)
		require.Equal(t, "Fixed invalid proof during migration", observedLogs.All()[0].Message)
		require.Equal(t, sig.NodeID().ShortString(), observedLogs.All()[0].ContextMap()["smesherID"])

		// check proof was updated
		blob := &sql.Blob{}
		require.NoError(t, identities.LoadMalfeasanceBlob(context.Background(), db, sig.NodeID().Bytes(), blob))
		got := &mwire.MalfeasanceProof{}
		codec.MustDecode(blob.Bytes, got)

		require.NotEqual(t, proof, got)
		require.Nil(t, got.Proof.Data.(*mwire.InvalidPrevATXProof).Atx1.VRFNonce)
	})

	t.Run("invalid proof cannot be fixed", func(t *testing.T) {
		db := setup(t)

		// store invalid proof
		cfg := config.MainnetConfig()
		sig, err := signing.NewEdSigner(
			signing.WithPrefix(cfg.Genesis.GenesisID().Bytes()),
		)
		require.NoError(t, err)
		verifier := signing.NewEdVerifier(
			signing.WithVerifierPrefix(cfg.Genesis.GenesisID().Bytes()),
		)

		goldenATX := types.RandomATXID()
		atx1 := newInitialATXv1(t, goldenATX, &goldenATX)
		atx1.Sign(sig)
		atx1.Signature = types.RandomEdSignature() // invalid ATX signature
		require.False(t, verifier.Verify(signing.ATX, atx1.SmesherID, atx1.SignedBytes(), atx1.Signature))

		atx2 := newInitialATXv1(t, goldenATX, &goldenATX)
		atx2.Sign(sig)
		require.True(t, verifier.Verify(signing.ATX, atx2.SmesherID, atx2.SignedBytes(), atx2.Signature))

		proof := &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.InvalidPrevATX,
				Data: &mwire.InvalidPrevATXProof{
					Atx1: *atx1,
					Atx2: *atx2,
				},
			},
		}
		require.NoError(t, identities.SetMalicious(db, sig.NodeID(), codec.MustEncode(proof), time.Now()))

		observer, observedLogs := observer.New(zapcore.ErrorLevel)
		logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
			func(core zapcore.Core) zapcore.Core {
				return zapcore.NewTee(core, observer)
			},
		)))

		m := New0025Migration(config.MainnetConfig())
		require.Equal(t, 25, m.Order())
		require.NoError(t, m.Apply(db, logger))

		require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
		require.Equal(t, zapcore.ErrorLevel, observedLogs.All()[0].Level)
		require.Equal(t, "Failed to fix invalid malfeasance proof during migration", observedLogs.All()[0].Message)
		require.Equal(t, sig.NodeID().ShortString(), observedLogs.All()[0].ContextMap()["smesherID"])
		require.Equal(t, "atx1: invalid signature", observedLogs.All()[0].ContextMap()["error"])

		// check proof not updated
		blob := &sql.Blob{}
		require.NoError(t, identities.LoadMalfeasanceBlob(context.Background(), db, sig.NodeID().Bytes(), blob))
		got := &mwire.MalfeasanceProof{}
		codec.MustDecode(blob.Bytes, got)

		require.Equal(t, proof, got)
	})
}

func newInitialATXv1(
	tb testing.TB,
	posATXID types.ATXID,
	comATXID *types.ATXID,
	opts ...func(*wire.ActivationTxV1),
) *wire.ActivationTxV1 {
	tb.Helper()
	poetRef := types.RandomHash()
	atx := &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				PrevATXID:        types.EmptyATXID,
				PublishEpoch:     types.EpochID(3),
				PositioningATXID: posATXID,
				CommitmentATXID:  comATXID,
				InitialPost:      &wire.PostV1{},
			},
			NIPost:   newNIPosV1tWithPoet(tb, poetRef.Bytes()),
			Coinbase: types.GenerateAddress([]byte("aaaa")),
			NumUnits: 100,
		},
	}
	for _, opt := range opts {
		opt(atx)
	}
	return atx
}

func newNIPosV1tWithPoet(tb testing.TB, poetRef []byte) *wire.NIPostV1 {
	tb.Helper()
	proof, _ := newMerkleProof(tb, []types.Hash32{
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

func newMerkleProof(tb testing.TB, leafs []types.Hash32) (types.MerkleProof, types.Hash32) {
	tb.Helper()
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(shared.HashMembershipTreeNode).
		WithLeavesToProve(map[uint64]bool{0: true}).
		Build()
	require.NoError(tb, err)
	for _, m := range leafs {
		require.NoError(tb, tree.AddLeaf(m[:]))
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
