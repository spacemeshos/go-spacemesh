package malfeasance_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)

	res := m.Run()
	os.Exit(res)
}

func createIdentity(tb testing.TB, db sql.Executor, sig *signing.EdSigner) {
	tb.Helper()
	atx := &types.ActivationTx{
		PublishEpoch: types.EpochID(1),
		Coinbase:     types.Address{},
		NumUnits:     1,
		SmesherID:    sig.NodeID(),
	}
	atx.SetReceived(time.Now())
	atx.SetID(types.RandomATXID())
	atx.TickCount = 1
	require.NoError(tb, atxs.Add(db, atx))
}

func TestHandler_HandleMalfeasanceProof_multipleATXs(t *testing.T) {
	t.Skip("TODO: update test")

	db := sql.InMemory()
	lg := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	trt := malfeasance.NewMocktortoise(ctrl)
	postVerifier := malfeasance.NewMockpostVerifier(ctrl)

	store := atxsdata.New()
	h := malfeasance.NewHandler(
		datastore.NewCachedDB(db, lg, datastore.WithConsensusCache(store)),
		lg,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		signing.NewEdVerifier(),
		trt,
		postVerifier,
	)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.LayerID(11)

	atxProof := wire.AtxProof{
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

	t.Run("proof equivalence", func(t *testing.T) {
		// FIXME(mafa): this test relies on the previous test to pass (database needs to contain malfeasance proof)
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[0].SmesherID = sig.NodeID()
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		ap.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: wire.Proof{
					Type: wire.MultipleATXs,
					Data: &ap,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Error(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))
		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)

		trt.EXPECT().OnMalfeasance(sig.NodeID())
		require.NoError(t, h.HandleMalfeasanceProof(context.Background(), "self", data))
		malProof, err = identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)
	})
}

func TestHandler_HandleSyncedMalfeasanceProof_multipleATXs(t *testing.T) {
	db := sql.InMemory()
	lg := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	trt := malfeasance.NewMocktortoise(ctrl)
	postVerifier := malfeasance.NewMockpostVerifier(ctrl)

	h := malfeasance.NewHandler(
		datastore.NewCachedDB(db, lg),
		lg,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		signing.NewEdVerifier(),
		trt,
		postVerifier,
	)
	var handlerCalled bool
	h.RegisterHandlerV1(wire.MultipleATXs, func(ctx context.Context, data scale.Type) (types.NodeID, error) {
		handlerCalled = true

		require.IsType(t, &wire.AtxProof{}, data)
		proof := data.(*wire.AtxProof)
		return proof.Messages[0].SmesherID, nil
	})

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	createIdentity(t, db, sig)

	malicious, err := identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.False(t, malicious)

	lid := types.LayerID(11)
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
	proof := wire.MalfeasanceProof{
		Layer: lid,
		Proof: wire.Proof{
			Type: wire.MultipleATXs,
			Data: &ap,
		},
	}
	data, err := codec.Encode(&proof)
	require.NoError(t, err)
	trt.EXPECT().OnMalfeasance(sig.NodeID())
	require.NoError(t, h.HandleSyncedMalfeasanceProof(context.Background(), types.Hash32(sig.NodeID()), "peer", data))
	require.True(t, handlerCalled)

	malicious, err = identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}

func TestHandler_HandleSyncedMalfeasanceProof_multipleBallots(t *testing.T) {
	db := sql.InMemory()
	lg := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	trt := malfeasance.NewMocktortoise(ctrl)
	postVerifier := malfeasance.NewMockpostVerifier(ctrl)

	h := malfeasance.NewHandler(
		datastore.NewCachedDB(db, lg),
		lg,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		signing.NewEdVerifier(),
		trt,
		postVerifier,
	)
	var handlerCalled bool
	h.RegisterHandlerV1(wire.MultipleBallots, func(ctx context.Context, data scale.Type) (types.NodeID, error) {
		handlerCalled = true

		require.IsType(t, &wire.BallotProof{}, data)
		proof := data.(*wire.BallotProof)
		return proof.Messages[0].SmesherID, nil
	})

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	createIdentity(t, db, sig)

	malicious, err := identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.False(t, malicious)

	lid := types.LayerID(11)
	bp := wire.BallotProof{
		Messages: [2]wire.BallotProofMsg{
			{
				InnerMsg: types.BallotMetadata{
					Layer:   lid,
					MsgHash: types.RandomHash(),
				},
			},
			{
				InnerMsg: types.BallotMetadata{
					Layer:   lid,
					MsgHash: types.RandomHash(),
				},
			},
		},
	}
	bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
	bp.Messages[0].SmesherID = sig.NodeID()
	bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
	bp.Messages[1].SmesherID = sig.NodeID()
	proof := wire.MalfeasanceProof{
		Layer: lid,
		Proof: wire.Proof{
			Type: wire.MultipleBallots,
			Data: &bp,
		},
	}
	data, err := codec.Encode(&proof)
	require.NoError(t, err)
	trt.EXPECT().OnMalfeasance(sig.NodeID())
	require.NoError(t, h.HandleSyncedMalfeasanceProof(context.Background(), types.Hash32(sig.NodeID()), "peer", data))
	require.True(t, handlerCalled)

	malicious, err = identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}

func TestHandler_HandleSyncedMalfeasanceProof_hareEquivocation(t *testing.T) {
	db := sql.InMemory()
	lg := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	trt := malfeasance.NewMocktortoise(ctrl)
	postVerifier := malfeasance.NewMockpostVerifier(ctrl)

	h := malfeasance.NewHandler(
		datastore.NewCachedDB(db, lg),
		lg,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		signing.NewEdVerifier(),
		trt,
		postVerifier,
	)
	var handlerCalled bool
	h.RegisterHandlerV1(wire.HareEquivocation, func(ctx context.Context, data scale.Type) (types.NodeID, error) {
		handlerCalled = true

		require.IsType(t, &wire.HareProof{}, data)
		proof := data.(*wire.HareProof)
		return proof.Messages[0].SmesherID, nil
	})

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	createIdentity(t, db, sig)

	malicious, err := identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.False(t, malicious)

	lid := types.LayerID(11)
	hp := wire.HareProof{
		Messages: [2]wire.HareProofMsg{
			{
				InnerMsg: wire.HareMetadata{
					Layer:   lid,
					Round:   3,
					MsgHash: types.RandomHash(),
				},
			},
			{
				InnerMsg: wire.HareMetadata{
					Layer:   lid,
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

	proof := wire.MalfeasanceProof{
		Layer: lid,
		Proof: wire.Proof{
			Type: wire.HareEquivocation,
			Data: &hp,
		},
	}
	data, err := codec.Encode(&proof)
	require.NoError(t, err)
	trt.EXPECT().OnMalfeasance(sig.NodeID())
	require.NoError(t, h.HandleSyncedMalfeasanceProof(context.Background(), types.Hash32(sig.NodeID()), "peer", data))
	require.True(t, handlerCalled)

	malicious, err = identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}

func TestHandler_HandleSyncedMalfeasanceProof_wrongHash(t *testing.T) {
	db := sql.InMemory()
	lg := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	trt := malfeasance.NewMocktortoise(ctrl)
	postVerifier := malfeasance.NewMockpostVerifier(ctrl)

	h := malfeasance.NewHandler(
		datastore.NewCachedDB(db, lg),
		lg,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		signing.NewEdVerifier(),
		trt,
		postVerifier,
	)
	var handlerCalled bool
	h.RegisterHandlerV1(wire.MultipleBallots, func(ctx context.Context, data scale.Type) (types.NodeID, error) {
		handlerCalled = true

		require.IsType(t, &wire.BallotProof{}, data)
		proof := data.(*wire.BallotProof)
		return proof.Messages[0].SmesherID, nil
	})
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	createIdentity(t, db, sig)

	malicious, err := identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.False(t, malicious)

	lid := types.LayerID(11)
	bp := wire.BallotProof{
		Messages: [2]wire.BallotProofMsg{
			{
				InnerMsg: types.BallotMetadata{
					Layer:   lid,
					MsgHash: types.RandomHash(),
				},
			},
			{
				InnerMsg: types.BallotMetadata{
					Layer:   lid,
					MsgHash: types.RandomHash(),
				},
			},
		},
	}
	bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
	bp.Messages[0].SmesherID = sig.NodeID()
	bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
	bp.Messages[1].SmesherID = sig.NodeID()
	proof := wire.MalfeasanceProof{
		Layer: lid,
		Proof: wire.Proof{
			Type: wire.MultipleBallots,
			Data: &bp,
		},
	}
	data, err := codec.Encode(&proof)
	require.NoError(t, err)
	trt.EXPECT().OnMalfeasance(sig.NodeID())
	err = h.HandleSyncedMalfeasanceProof(context.Background(), types.RandomHash(), "peer", data)
	require.ErrorIs(t, err, pubsub.ErrValidationReject)
	require.True(t, handlerCalled)

	malicious, err = identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}
