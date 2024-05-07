package malfeasance_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/activation"
	awire "github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
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

func createIdentity(t *testing.T, db *sql.Database, sig *signing.EdSigner) {
	challenge := types.NIPostChallenge{
		PublishEpoch: types.EpochID(1),
	}
	atx := types.NewActivationTx(challenge, types.Address{}, nil, 1, nil)
	require.NoError(t, activation.SignAndFinalizeAtx(sig, atx))
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now())
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, vAtx))
}

func TestHandler_HandleMalfeasanceProof_multipleATXs(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)

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

	t.Run("unknown identity", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[0].SmesherID = sig.NodeID()
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		ap.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
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
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	createIdentity(t, db, sig)

	t.Run("same msg hash", func(t *testing.T) {
		msgHash := types.RandomHash()
		ap := atxProof
		ap.Messages[0].InnerMsg.MsgHash = msgHash
		ap.Messages[1].InnerMsg.MsgHash = msgHash
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
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
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different epoch", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].InnerMsg.PublishEpoch = ap.Messages[1].InnerMsg.PublishEpoch + 1
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
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
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		ap.Messages[1].Signature = sig2.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
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
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("invalid hare eligibility", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.MultipleATXs,
					Data: &ap,
				},
			},
			Eligibility: &types.HareEligibilityGossip{
				NodeID: types.RandomNodeID(),
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Error(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})

	t.Run("valid", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[0].SmesherID = sig.NodeID()
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		ap.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.MultipleATXs,
					Data: &ap,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		trt.EXPECT().OnMalfeasance(sig.NodeID())
		require.NoError(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotNil(t, malProof.Received())
		malProof.SetReceived(time.Time{})
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

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

func TestHandler_HandleMalfeasanceProof_multipleBallots(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
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
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.LayerID(11)

	ballotProof := wire.BallotProof{
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

	t.Run("unknown identity", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.MultipleBallots,
					Data: &bp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Error(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	createIdentity(t, db, sig)

	t.Run("same msg hash", func(t *testing.T) {
		msgHash := types.RandomHash()
		bp := ballotProof
		bp.Messages[0].InnerMsg.MsgHash = msgHash
		bp.Messages[1].InnerMsg.MsgHash = msgHash
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.MultipleBallots,
					Data: &bp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Error(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different layer", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[1].InnerMsg.Layer = lid.Sub(1)
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.MultipleBallots,
					Data: &bp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Error(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		bp.Messages[1].Signature = sig2.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig2.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.MultipleBallots,
					Data: &bp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Error(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("invalid hare eligibility", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.MultipleBallots,
					Data: &bp,
				},
			},
			Eligibility: &types.HareEligibilityGossip{
				NodeID: types.RandomNodeID(),
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Error(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})

	t.Run("valid", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.MultipleBallots,
					Data: &bp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		trt.EXPECT().OnMalfeasance(sig.NodeID())
		require.NoError(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotNil(t, malProof.Received())
		malProof.SetReceived(time.Time{})
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: wire.Proof{
					Type: wire.MultipleBallots,
					Data: &bp,
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

func TestHandler_HandleMalfeasanceProof_hareEquivocation(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
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
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.LayerID(11)

	hareProof := wire.HareProof{
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

	t.Run("unknown identity", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.ErrorIs(t, h.HandleMalfeasanceProof(context.Background(), "peer", data), pubsub.ErrValidationReject)

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	createIdentity(t, db, sig)

	t.Run("invalid signature", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = types.RandomEdSignature()
		hp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.ErrorIs(t, h.HandleMalfeasanceProof(context.Background(), "peer", data), pubsub.ErrValidationReject)

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("same msg hash", func(t *testing.T) {
		msgHash := types.RandomHash()
		hp := hareProof
		hp.Messages[0].InnerMsg.MsgHash = msgHash
		hp.Messages[1].InnerMsg.MsgHash = msgHash
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()

		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.ErrorIs(t, h.HandleMalfeasanceProof(context.Background(), "peer", data), pubsub.ErrValidationReject)

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different layer", func(t *testing.T) {
		hp := hareProof
		hp.Messages[1].InnerMsg.Layer = lid.Sub(1)
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.ErrorIs(t, h.HandleMalfeasanceProof(context.Background(), "peer", data), pubsub.ErrValidationReject)

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different round", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].InnerMsg.Round = hp.Messages[1].InnerMsg.Round + 1
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.ErrorIs(t, h.HandleMalfeasanceProof(context.Background(), "peer", data), pubsub.ErrValidationReject)

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		hp.Messages[1].Signature = sig2.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.ErrorIs(t, h.HandleMalfeasanceProof(context.Background(), "peer", data), pubsub.ErrValidationReject)

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("hare eligibility is deprecated", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
				},
			},
			Eligibility: &types.HareEligibilityGossip{
				NodeID: types.RandomNodeID(),
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.ErrorIs(t, h.HandleMalfeasanceProof(context.Background(), "peer", data), pubsub.ErrValidationReject)
	})

	t.Run("valid", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid,
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		trt.EXPECT().OnMalfeasance(sig.NodeID())
		require.NoError(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotNil(t, malProof.Received())
		malProof.SetReceived(time.Time{})
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()
		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: wire.Proof{
					Type: wire.HareEquivocation,
					Data: &hp,
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

func TestHandler_CrossDomain(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
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
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	createIdentity(t, db, sig)

	target := 10
	m1 := types.BallotMetadata{
		Layer:   types.LayerID(uint32(target)),
		MsgHash: types.Hash32{1, 1, 1},
	}
	m2 := types.ATXMetadata{
		PublishEpoch: types.EpochID(target),
		MsgHash:      types.Hash32{2, 2, 2},
	}
	m1buf, err := codec.Encode(&m1)
	require.NoError(t, err)
	m2buf, err := codec.Encode(&m2)
	require.NoError(t, err)

	msg, err := codec.Encode(&wire.MalfeasanceGossip{
		MalfeasanceProof: wire.MalfeasanceProof{
			Layer: types.LayerID(11),
			Proof: wire.Proof{
				Type: wire.MultipleBallots,
				Data: &wire.BallotProof{
					Messages: [2]wire.BallotProofMsg{
						{
							InnerMsg:  m1,
							Signature: sig.Sign(signing.BALLOT, m1buf),
							SmesherID: sig.NodeID(),
						},
						{
							InnerMsg:  *(*types.BallotMetadata)(unsafe.Pointer(&m2)),
							Signature: sig.Sign(signing.ATX, m2buf),
							SmesherID: sig.NodeID(),
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Error(t, h.HandleMalfeasanceProof(context.Background(), "", msg))

	malicious, err := identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.False(t, malicious)
}

func TestHandler_HandleSyncedMalfeasanceProof_multipleATXs(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
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

	malicious, err = identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}

func TestHandler_HandleSyncedMalfeasanceProof_multipleBallots(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
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

	malicious, err = identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}

func TestHandler_HandleSyncedMalfeasanceProof_hareEquivocation(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
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

	malicious, err = identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}

func TestHandler_HandleSyncedMalfeasanceProof_wrongHash(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
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

	malicious, err = identities.IsMalicious(db, sig.NodeID())
	require.NoError(t, err)
	require.True(t, malicious)
}

func TestHandler_HandleSyncedMalfeasanceProof_InvalidPostIndex(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeIdH32 := types.Hash32(sig.NodeID())
	id := sig.NodeID()
	atx := awire.ActivationTxV1{
		InnerActivationTxV1: awire.InnerActivationTxV1{
			NIPostChallengeV1: awire.NIPostChallengeV1{
				CommitmentATX: &types.ATXID{1, 2, 3},
			},
			NIPost: &awire.NIPostV1{
				Post:         &awire.PostV1{},
				PostMetadata: &awire.PostMetadataV1{},
			},
		},
		SmesherID: id,
	}
	atx.Signature = sig.Sign(signing.ATX, atx.SignedBytes())

	t.Run("valid malfeasance proof", func(t *testing.T) {
		db := sql.InMemory()
		lg := logtest.New(t)
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

		proof := wire.MalfeasanceProof{
			Layer: types.LayerID(11),
			Proof: wire.Proof{
				Type: wire.InvalidPostIndex,
				Data: &wire.InvalidPostIndexProof{
					Atx:        atx,
					InvalidIdx: 7,
				},
			},
		}

		postVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("invalid"))
		trt.EXPECT().OnMalfeasance(sig.NodeID())
		err := h.HandleSyncedMalfeasanceProof(context.Background(), nodeIdH32, "peer", codec.MustEncode(&proof))
		require.NoError(t, err)

		malicious, err := identities.IsMalicious(db, sig.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("invalid malfeasance proof (POST valid)", func(t *testing.T) {
		db := sql.InMemory()
		lg := logtest.New(t)
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

		proof := wire.MalfeasanceProof{
			Layer: types.LayerID(11),
			Proof: wire.Proof{
				Type: wire.InvalidPostIndex,
				Data: &wire.InvalidPostIndexProof{
					Atx:        atx,
					InvalidIdx: 7,
				},
			},
		}

		postVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		err := h.HandleSyncedMalfeasanceProof(context.Background(), nodeIdH32, "peer", codec.MustEncode(&proof))
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		malicious, err := identities.IsMalicious(db, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)
	})

	t.Run("invalid malfeasance proof (ATX signature invalid)", func(t *testing.T) {
		db := sql.InMemory()
		lg := logtest.New(t)
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

		atx := atx
		atx.NIPost.Post.Pow += 1 // invalidate signature by changing content

		proof := wire.MalfeasanceProof{
			Layer: types.LayerID(11),
			Proof: wire.Proof{
				Type: wire.InvalidPostIndex,
				Data: &wire.InvalidPostIndexProof{
					Atx:        atx,
					InvalidIdx: 7,
				},
			},
		}

		err := h.HandleSyncedMalfeasanceProof(context.Background(), nodeIdH32, "peer", codec.MustEncode(&proof))
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
		require.ErrorContains(t, err, "invalid signature")

		malicious, err := identities.IsMalicious(db, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)
	})
}

func TestHandler_HandleSyncedMalfeasanceProof_InvalidPrevATX(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeIdH32 := types.Hash32(sig.NodeID())

	prevATX := *types.NewActivationTx(
		types.NIPostChallenge{
			PublishEpoch:  types.EpochID(1),
			CommitmentATX: &types.ATXID{1, 2, 3},
		},
		types.Address{},
		nil,
		1,
		nil,
	)
	require.NoError(t, activation.SignAndFinalizeAtx(sig, &prevATX))

	atx1 := *types.NewActivationTx(
		types.NIPostChallenge{
			PublishEpoch: types.EpochID(2),
			PrevATXID:    prevATX.ID(),
		},
		types.Address{},
		nil,
		1,
		nil,
	)
	require.NoError(t, activation.SignAndFinalizeAtx(sig, &atx1))

	atx2 := *types.NewActivationTx(
		types.NIPostChallenge{
			PublishEpoch: types.EpochID(3),
			PrevATXID:    prevATX.ID(),
		},
		types.Address{},
		nil,
		1,
		nil,
	)
	require.NoError(t, activation.SignAndFinalizeAtx(sig, &atx2))

	t.Run("valid malfeasance proof", func(t *testing.T) {
		db := sql.InMemory()
		lg := logtest.New(t)
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

		proof := wire.MalfeasanceProof{
			Layer: types.LayerID(11),
			Proof: wire.Proof{
				Type: wire.InvalidPrevATX,
				Data: &wire.InvalidPrevATXProof{
					Atx1: *awire.ActivationTxToWireV1(&atx1),
					Atx2: *awire.ActivationTxToWireV1(&atx2),
				},
			},
		}

		trt.EXPECT().OnMalfeasance(sig.NodeID())
		err := h.HandleSyncedMalfeasanceProof(context.Background(), nodeIdH32, "peer", codec.MustEncode(&proof))
		require.NoError(t, err)

		malicious, err := identities.IsMalicious(db, sig.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("invalid malfeasance proof (same ATX)", func(t *testing.T) {
		db := sql.InMemory()
		lg := logtest.New(t)
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

		proof := wire.MalfeasanceProof{
			Layer: types.LayerID(11),
			Proof: wire.Proof{
				Type: wire.InvalidPrevATX,
				Data: &wire.InvalidPrevATXProof{
					Atx1: *awire.ActivationTxToWireV1(&atx1),
					Atx2: *awire.ActivationTxToWireV1(&atx1),
				},
			},
		}

		err := h.HandleSyncedMalfeasanceProof(context.Background(), nodeIdH32, "peer", codec.MustEncode(&proof))
		require.ErrorContains(t, err, "ATX IDs are the same")

		malicious, err := identities.IsMalicious(db, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)
	})

	t.Run("invalid malfeasance proof (prev ATXs differ)", func(t *testing.T) {
		db := sql.InMemory()
		lg := logtest.New(t)
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

		atx3 := *types.NewActivationTx(
			types.NIPostChallenge{
				PublishEpoch: types.EpochID(3),
				PrevATXID:    atx1.ID(),
			},
			types.Address{},
			nil,
			1,
			nil,
		)
		require.NoError(t, activation.SignAndFinalizeAtx(sig, &atx3))

		proof := wire.MalfeasanceProof{
			Layer: types.LayerID(11),
			Proof: wire.Proof{
				Type: wire.InvalidPrevATX,
				Data: &wire.InvalidPrevATXProof{
					Atx1: *awire.ActivationTxToWireV1(&atx1),
					Atx2: *awire.ActivationTxToWireV1(&atx3),
				},
			},
		}

		err := h.HandleSyncedMalfeasanceProof(context.Background(), nodeIdH32, "peer", codec.MustEncode(&proof))
		require.ErrorContains(t, err, "prev ATX IDs are different")

		malicious, err := identities.IsMalicious(db, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)
	})

	t.Run("invalid malfeasance proof (ATX signature invalid)", func(t *testing.T) {
		db := sql.InMemory()
		lg := logtest.New(t)
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

		atx3 := *types.NewActivationTx(
			types.NIPostChallenge{
				PublishEpoch: types.EpochID(3),
				PrevATXID:    atx1.ID(),
			},
			types.Address{},
			nil,
			1,
			nil,
		)
		require.NoError(t, activation.SignAndFinalizeAtx(sig, &atx3))
		atx3.PrevATXID = prevATX.ID() // invalidate signature by changing content

		proof := wire.MalfeasanceProof{
			Layer: types.LayerID(11),
			Proof: wire.Proof{
				Type: wire.InvalidPrevATX,
				Data: &wire.InvalidPrevATXProof{
					Atx1: *awire.ActivationTxToWireV1(&atx1),
					Atx2: *awire.ActivationTxToWireV1(&atx3),
				},
			},
		}

		err := h.HandleSyncedMalfeasanceProof(context.Background(), nodeIdH32, "peer", codec.MustEncode(&proof))
		require.ErrorContains(t, err, "invalid signature")

		malicious, err := identities.IsMalicious(db, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)
	})
}
