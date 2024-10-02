package activation

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	awire "github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func createIdentity(tb testing.TB, db sql.Executor, sig *signing.EdSigner) {
	tb.Helper()
	atx := &types.ActivationTx{
		PublishEpoch: types.EpochID(1),
		NumUnits:     1,
		SmesherID:    sig.NodeID(),
	}
	atx.SetReceived(time.Now())
	atx.SetID(types.RandomATXID())
	atx.TickCount = 1
	require.NoError(tb, atxs.Add(db, atx, types.AtxBlob{}))
}

type testMalfeasanceHandler struct {
	*MalfeasanceHandler

	db sql.StateDatabase
}

func newTestMalfeasanceHandler(tb testing.TB) *testMalfeasanceHandler {
	db := statesql.InMemory()
	observer, _ := observer.New(zapcore.WarnLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	h := NewMalfeasanceHandler(db, logger, signing.NewEdVerifier())
	return &testMalfeasanceHandler{
		MalfeasanceHandler: h,

		db: db,
	}
}

func TestMalfeasanceHandler_Validate(t *testing.T) {
	t.Run("unknown identity", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		// identity is unknown to handler

		ap := wire.AtxProof{
			Messages: [2]wire.AtxProofMsg{
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      types.RandomHash(),
					},
				},
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      types.RandomHash(),
					},
				},
			},
		}
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[0].SmesherID = sig.NodeID()
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		ap.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &ap)
		require.ErrorContains(t, err, "identity does not exist")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("same msg hash", func(t *testing.T) {
		msgHash := types.RandomHash()
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		ap := wire.AtxProof{
			Messages: [2]wire.AtxProofMsg{
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      msgHash,
					},
					SmesherID: sig.NodeID(),
				},
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      msgHash,
					},
					SmesherID: sig.NodeID(),
				},
			},
		}
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())

		nodeID, err := h.Validate(context.Background(), &ap)
		require.ErrorContains(t, err, "invalid atx malfeasance proof")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("different epoch", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		ap := wire.AtxProof{
			Messages: [2]wire.AtxProofMsg{
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      types.RandomHash(),
					},
					SmesherID: sig.NodeID(),
				},
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(4),
						MsgHash:      types.RandomHash(),
					},
					SmesherID: sig.NodeID(),
				},
			},
		}
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())

		nodeID, err := h.Validate(context.Background(), &ap)
		require.ErrorContains(t, err, "invalid atx malfeasance proof")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("different signer", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig2)

		ap := wire.AtxProof{
			Messages: [2]wire.AtxProofMsg{
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      types.RandomHash(),
					},
					SmesherID: sig.NodeID(),
				},
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      types.RandomHash(),
					},
					SmesherID: sig2.NodeID(),
				},
			},
		}
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig2.Sign(signing.ATX, ap.Messages[1].SignedBytes())

		nodeID, err := h.Validate(context.Background(), &ap)
		require.ErrorContains(t, err, "invalid atx malfeasance proof")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("valid", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		ap := wire.AtxProof{
			Messages: [2]wire.AtxProofMsg{
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      types.RandomHash(),
					},
					SmesherID: sig.NodeID(),
				},
				{
					InnerMsg: types.ATXMetadata{
						PublishEpoch: types.EpochID(3),
						MsgHash:      types.RandomHash(),
					},
					SmesherID: sig.NodeID(),
				},
			},
		}
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())

		nodeID, err := h.Validate(context.Background(), &ap)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), nodeID)
	})
}

type testInvalidPostIndexHandler struct {
	*InvalidPostIndexHandler

	mockPostVerifier *MockPostVerifier
}

func newTestInvalidPostIndexHandler(tb testing.TB) *testInvalidPostIndexHandler {
	db := statesql.InMemory()

	ctrl := gomock.NewController(tb)
	postVerifier := NewMockPostVerifier(ctrl)

	h := NewInvalidPostIndexHandler(db, signing.NewEdVerifier(), postVerifier)
	return &testInvalidPostIndexHandler{
		InvalidPostIndexHandler: h,

		mockPostVerifier: postVerifier,
	}
}

func TestInvalidPostIndexHandler_Validate(t *testing.T) {
	t.Run("valid malfeasance proof", func(t *testing.T) {
		h := newTestInvalidPostIndexHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atx := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PositioningATXID: types.RandomATXID(),
					PrevATXID:        types.EmptyATXID,
					PublishEpoch:     rand.N[types.EpochID](types.EpochID(100)),
					Sequence:         0,
					CommitmentATXID:  &types.ATXID{1, 2, 3},
				},
				NIPost: &awire.NIPostV1{
					Post: &awire.PostV1{
						Nonce:   1,
						Indices: types.RandomBytes(8),
						Pow:     1,
					},
					PostMetadata: &awire.PostMetadataV1{
						Challenge:     types.RandomBytes(32),
						LabelsPerUnit: 123,
					},
				},
				NumUnits: rand.Uint32N(100),
			},
			SmesherID: sig.NodeID(),
		}
		atx.Signature = sig.Sign(signing.ATX, atx.SignedBytes())
		proof := &wire.InvalidPostIndexProof{
			Atx:        atx,
			InvalidIdx: 7,
		}

		postProof := &shared.Proof{
			Nonce:   atx.NIPost.Post.Nonce,
			Indices: atx.NIPost.Post.Indices,
			Pow:     atx.NIPost.Post.Pow,
		}
		meta := &shared.ProofMetadata{
			NodeId:          atx.SmesherID.Bytes(),
			CommitmentAtxId: atx.NIPostChallengeV1.CommitmentATXID.Bytes(),

			Challenge:     atx.NIPost.PostMetadata.Challenge,
			NumUnits:      atx.NumUnits,
			LabelsPerUnit: atx.NIPost.PostMetadata.LabelsPerUnit,
		}
		h.mockPostVerifier.EXPECT().Verify(gomock.Any(),
			postProof,
			meta,
			gomock.Any(),
		).Return(errors.New("invalid post"))
		nodeID, err := h.Validate(context.Background(), proof)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), nodeID)
	})

	t.Run("invalid malfeasance proof (POST valid)", func(t *testing.T) {
		h := newTestInvalidPostIndexHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atx := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PositioningATXID: types.RandomATXID(),
					PrevATXID:        types.EmptyATXID,
					PublishEpoch:     rand.N[types.EpochID](types.EpochID(100)),
					Sequence:         0,
					CommitmentATXID:  &types.ATXID{1, 2, 3},
				},
				NIPost: &awire.NIPostV1{
					Post: &awire.PostV1{
						Nonce:   1,
						Indices: types.RandomBytes(8),
						Pow:     1,
					},
					PostMetadata: &awire.PostMetadataV1{
						Challenge:     types.RandomBytes(32),
						LabelsPerUnit: 123,
					},
				},
				NumUnits: rand.Uint32N(100),
			},
			SmesherID: sig.NodeID(),
		}
		atx.Signature = sig.Sign(signing.ATX, atx.SignedBytes())
		proof := &wire.InvalidPostIndexProof{
			Atx:        atx,
			InvalidIdx: 7,
		}

		postProof := &shared.Proof{
			Nonce:   atx.NIPost.Post.Nonce,
			Indices: atx.NIPost.Post.Indices,
			Pow:     atx.NIPost.Post.Pow,
		}
		meta := &shared.ProofMetadata{
			NodeId:          atx.SmesherID.Bytes(),
			CommitmentAtxId: atx.NIPostChallengeV1.CommitmentATXID.Bytes(),

			Challenge:     atx.NIPost.PostMetadata.Challenge,
			NumUnits:      atx.NumUnits,
			LabelsPerUnit: atx.NIPost.PostMetadata.LabelsPerUnit,
		}
		h.mockPostVerifier.EXPECT().Verify(gomock.Any(),
			postProof,
			meta,
			gomock.Any(),
		).Return(nil)
		nodeID, err := h.Validate(context.Background(), proof)
		require.ErrorContains(t, err, "POST is valid")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("invalid malfeasance proof (ATX signature invalid)", func(t *testing.T) {
		h := newTestInvalidPostIndexHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atx := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PositioningATXID: types.RandomATXID(),
					PrevATXID:        types.EmptyATXID,
					PublishEpoch:     rand.N[types.EpochID](types.EpochID(100)),
					Sequence:         0,
					CommitmentATXID:  &types.ATXID{1, 2, 3},
				},
				NIPost: &awire.NIPostV1{
					Post: &awire.PostV1{
						Nonce:   1,
						Indices: types.RandomBytes(8),
						Pow:     1,
					},
					PostMetadata: &awire.PostMetadataV1{
						Challenge:     types.RandomBytes(32),
						LabelsPerUnit: 123,
					},
				},
				NumUnits: rand.Uint32N(100),
			},
			SmesherID: sig.NodeID(),
		}
		atx.Signature = types.RandomEdSignature()
		proof := &wire.InvalidPostIndexProof{
			Atx:        atx,
			InvalidIdx: 7,
		}

		nodeID, err := h.Validate(context.Background(), proof)
		require.ErrorContains(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})
}

type testInvalidPrevATXHandler struct {
	*InvalidPrevATXHandler

	db sql.StateDatabase
}

func newTestInvalidPrevATXHandler(tb testing.TB) *testInvalidPrevATXHandler {
	db := statesql.InMemory()

	h := NewInvalidPrevATXHandler(db, signing.NewEdVerifier())
	return &testInvalidPrevATXHandler{
		InvalidPrevATXHandler: h,

		db: db,
	}
}

func TestInvalidPrevATX_Validate(t *testing.T) {
	t.Run("valid malfeasance proof", func(t *testing.T) {
		h := newTestInvalidPrevATXHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		prevATXID := types.RandomATXID()

		atx1 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(2),
				},
			},
		}
		atx1.Sign(sig)

		atx2 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(3),
				},
			},
		}
		atx2.Sign(sig)

		proof := &wire.InvalidPrevATXProof{
			Atx1: atx1,
			Atx2: atx2,
		}

		nodeID, err := h.Validate(context.Background(), proof)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), nodeID)
	})

	t.Run("unknown identity", func(t *testing.T) {
		h := newTestInvalidPrevATXHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		prevATXID := types.RandomATXID()

		atx1 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(2),
				},
			},
		}
		atx1.Sign(sig)

		atx2 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(3),
				},
			},
		}
		atx2.Sign(sig)

		proof := &wire.InvalidPrevATXProof{
			Atx1: atx1,
			Atx2: atx2,
		}

		nodeID, err := h.Validate(context.Background(), proof)
		require.ErrorContains(t, err, "identity does not exist")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("invalid malfeasance proof (invalid signature for first)", func(t *testing.T) {
		h := newTestInvalidPrevATXHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		prevATXID := types.RandomATXID()

		atx1 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(2),
				},
			},
		}
		atx1.Signature = types.EdSignature(types.RandomBytes(64))
		atx1.SmesherID = sig.NodeID()

		atx2 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(3),
				},
			},
		}
		atx2.Sign(sig)

		proof := &wire.InvalidPrevATXProof{
			Atx1: atx1,
			Atx2: atx2,
		}

		nodeID, err := h.Validate(context.Background(), proof)
		require.ErrorContains(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("invalid malfeasance proof (invalid signature for first)", func(t *testing.T) {
		h := newTestInvalidPrevATXHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		prevATXID := types.RandomATXID()

		atx1 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(2),
				},
			},
		}
		atx1.Sign(sig)

		atx2 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(3),
				},
			},
		}
		atx2.Signature = types.EdSignature(types.RandomBytes(64))
		atx2.SmesherID = sig.NodeID()

		proof := &wire.InvalidPrevATXProof{
			Atx1: atx1,
			Atx2: atx2,
		}

		nodeID, err := h.Validate(context.Background(), proof)
		require.ErrorContains(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("invalid malfeasance proof (same ATX)", func(t *testing.T) {
		h := newTestInvalidPrevATXHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		prevATXID := types.RandomATXID()

		atx1 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(2),
				},
			},
		}
		atx1.Sign(sig)

		atx2 := atx1

		proof := &wire.InvalidPrevATXProof{
			Atx1: atx1,
			Atx2: atx2,
		}

		nodeID, err := h.Validate(context.Background(), proof)
		require.ErrorContains(t, err, "ATX IDs are the same")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("invalid malfeasance proof (prev ATXs differ)", func(t *testing.T) {
		h := newTestInvalidPrevATXHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		atx1 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    types.RandomATXID(),
					PublishEpoch: types.EpochID(2),
				},
			},
		}
		atx1.Sign(sig)

		atx2 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    atx1.ID(),
					PublishEpoch: types.EpochID(3),
				},
			},
		}
		atx2.Sign(sig)

		proof := &wire.InvalidPrevATXProof{
			Atx1: atx1,
			Atx2: atx2,
		}

		nodeID, err := h.Validate(context.Background(), proof)
		require.ErrorContains(t, err, "prev ATX IDs are different")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("invalid malfeasance proof (ATXs by different identities)", func(t *testing.T) {
		h := newTestInvalidPrevATXHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig2)

		prevATXID := types.RandomATXID()

		atx1 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(2),
				},
			},
		}
		atx1.Sign(sig)

		atx2 := awire.ActivationTxV1{
			InnerActivationTxV1: awire.InnerActivationTxV1{
				NIPostChallengeV1: awire.NIPostChallengeV1{
					PrevATXID:    prevATXID,
					PublishEpoch: types.EpochID(3),
				},
			},
		}
		atx2.Sign(sig2)

		proof := &wire.InvalidPrevATXProof{
			Atx1: atx1,
			Atx2: atx2,
		}

		nodeID, err := h.Validate(context.Background(), proof)
		require.ErrorContains(t, err, "smesher IDs are different")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})
}
