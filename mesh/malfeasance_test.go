package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

type testMalfeasanceHandler struct {
	*MalfeasanceHandler

	observedLogs *observer.ObservedLogs
	db           *sql.Database
}

func newTestMalfeasanceHandler(tb testing.TB) *testMalfeasanceHandler {
	db := sql.InMemory()
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	h := NewMalfeasanceHandler(db, signing.NewEdVerifier(), WithMalfeasanceLogger(logger))

	return &testMalfeasanceHandler{
		MalfeasanceHandler: h,

		observedLogs: observedLogs,
		db:           db,
	}
}

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

func TestHandler_Validate(t *testing.T) {
	t.Run("unknown identity", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		// identity is unknown to handler

		bp := wire.BallotProof{
			Messages: [2]wire.BallotProofMsg{
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &bp)
		require.ErrorContains(t, err, "identity does not exist")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("same msg hash", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		msgHash := types.RandomHash()
		bp := wire.BallotProof{
			Messages: [2]wire.BallotProofMsg{
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: msgHash,
					},
				},
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: msgHash,
					},
				},
			},
		}
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &bp)
		require.ErrorContains(t, err, "invalid ballot malfeasance proof")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("different layer", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		bp := wire.BallotProof{
			Messages: [2]wire.BallotProofMsg{
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(10),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &bp)
		require.ErrorContains(t, err, "invalid ballot malfeasance proof")
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

		bp := wire.BallotProof{
			Messages: [2]wire.BallotProofMsg{
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig2.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig2.NodeID()

		nodeID, err := h.Validate(context.Background(), &bp)
		require.ErrorContains(t, err, "invalid ballot malfeasance proof")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("valid", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		bp := wire.BallotProof{
			Messages: [2]wire.BallotProofMsg{
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: types.BallotMetadata{
						Layer:   types.LayerID(11),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &bp)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), nodeID)
	})
}
