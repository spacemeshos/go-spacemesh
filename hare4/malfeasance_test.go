package hare4

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
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testMalfeasanceHandler struct {
	*MalfeasanceHandler

	observedLogs *observer.ObservedLogs
	db           sql.StateDatabase
}

func newTestMalfeasanceHandler(tb testing.TB) *testMalfeasanceHandler {
	db := statesql.InMemory()
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

		hp := wire.HareProof{
			Messages: [2]wire.HareProofMsg{
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &hp)
		require.ErrorContains(t, err, "identity does not exist")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("invalid signature", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		hp := wire.HareProof{
			Messages: [2]wire.HareProofMsg{
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = types.RandomEdSignature()
		hp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &hp)
		require.ErrorContains(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("same msg hash", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		msgHash := types.RandomHash()
		hp := wire.HareProof{
			Messages: [2]wire.HareProofMsg{
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: msgHash,
					},
				},
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: msgHash,
					},
				},
			},
		}
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &hp)
		require.ErrorContains(t, err, "invalid hare malfeasance proof")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("different layer", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		hp := wire.HareProof{
			Messages: [2]wire.HareProofMsg{
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(10),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &hp)
		require.ErrorContains(t, err, "invalid hare malfeasance proof")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("different round", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		hp := wire.HareProof{
			Messages: [2]wire.HareProofMsg{
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(10),
						Round:   4,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &hp)
		require.ErrorContains(t, err, "invalid hare malfeasance proof")
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

		hp := wire.HareProof{
			Messages: [2]wire.HareProofMsg{
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(10),
						Round:   4,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig2.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig2.NodeID()

		nodeID, err := h.Validate(context.Background(), &hp)
		require.ErrorContains(t, err, "invalid hare malfeasance proof")
		require.Equal(t, types.EmptyNodeID, nodeID)
	})

	t.Run("valid", func(t *testing.T) {
		h := newTestMalfeasanceHandler(t)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		createIdentity(t, h.db, sig)

		hp := wire.HareProof{
			Messages: [2]wire.HareProofMsg{
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: wire.HareMetadata{
						Layer:   types.LayerID(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()

		nodeID, err := h.Validate(context.Background(), &hp)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), nodeID)
	})
}
