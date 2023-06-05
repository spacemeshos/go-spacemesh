package malfeasance_test

import (
	"context"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
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
	atx := types.NewActivationTx(challenge, types.Address{}, nil, 1, nil, nil)
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
	mcp := malfeasance.NewMockconsensusProtocol(gomock.NewController(t))
	sigVerifier, err := signing.NewEdVerifier()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, sigVerifier)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.LayerID(11)

	atxProof := types.AtxProof{
		Messages: [2]types.AtxProofMsg{
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
					Data: &ap,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Equal(t, nil, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		// FIXME(mafa): this test relies on the previous test to pass (database needs to contain malfeasance proof)
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
		ap.Messages[0].SmesherID = sig.NodeID()
		ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
		ap.Messages[1].SmesherID = sig.NodeID()
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: types.Proof{
					Type: types.MultipleATXs,
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

		require.Equal(t, nil, h.HandleMalfeasanceProof(context.Background(), "self", data))
		malProof, err = identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)
	})
}

func TestHandler_HandleMalfeasanceProof_multipleBallots(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
	mcp := malfeasance.NewMockconsensusProtocol(gomock.NewController(t))
	sigVerifier, err := signing.NewEdVerifier()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, sigVerifier)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.LayerID(11)

	ballotProof := types.BallotProof{
		Messages: [2]types.BallotProofMsg{
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
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
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
					Data: &bp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Equal(t, nil, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
		bp.Messages[0].SmesherID = sig.NodeID()
		bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
		bp.Messages[1].SmesherID = sig.NodeID()
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: types.Proof{
					Type: types.MultipleBallots,
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

		require.Equal(t, nil, h.HandleMalfeasanceProof(context.Background(), "self", data))
		malProof, err = identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)
	})
}

func TestHandler_HandleMalfeasanceProof_hareEquivocation(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
	mcp := malfeasance.NewMockconsensusProtocol(gomock.NewController(t))
	sigVerifier, err := signing.NewEdVerifier()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, sigVerifier)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.LayerID(11)

	hareProof := types.HareProof{
		Messages: [2]types.HareProofMsg{
			{
				InnerMsg: types.HareMetadata{
					Layer:   lid,
					Round:   3,
					MsgHash: types.RandomHash(),
				},
			},
			{
				InnerMsg: types.HareMetadata{
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
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hp,
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
		hp := hareProof
		hp.Messages[0].InnerMsg.MsgHash = msgHash
		hp.Messages[1].InnerMsg.MsgHash = msgHash
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hp,
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
		hp := hareProof
		hp.Messages[1].InnerMsg.Layer = lid.Sub(1)
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hp,
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

	t.Run("different round", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].InnerMsg.Round = hp.Messages[1].InnerMsg.Round + 1
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hp,
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
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		hp.Messages[1].Signature = sig2.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hp,
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
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hp,
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
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hp,
				},
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Equal(t, nil, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
		hp.Messages[0].SmesherID = sig.NodeID()
		hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
		hp.Messages[1].SmesherID = sig.NodeID()
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: types.Proof{
					Type: types.HareEquivocation,
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

		require.Equal(t, nil, h.HandleMalfeasanceProof(context.Background(), "self", data))
		malProof, err = identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)
	})
}

func TestHandler_HandleMalfeasanceProof_validateHare(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
	mcp := malfeasance.NewMockconsensusProtocol(gomock.NewController(t))
	sigVerifier, err := signing.NewEdVerifier()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, sigVerifier)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	createIdentity(t, db, sig)
	lid := types.LayerID(11)
	round := uint32(11)
	proofByte := types.RandomVrfSignature()
	eCount := uint16(3)

	bp := types.BallotProof{
		Messages: [2]types.BallotProofMsg{
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
	gossip := &types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: lid,
			Proof: types.Proof{
				Type: types.MultipleBallots,
				Data: &bp,
			},
		},
	}

	t.Run("different node id", func(t *testing.T) {
		gs := gossip
		gs.Eligibility = &types.HareEligibilityGossip{
			NodeID: types.RandomNodeID(),
		}
		data, err := codec.Encode(gs)
		require.NoError(t, err)
		require.Error(t, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})

	t.Run("relay eligibility", func(t *testing.T) {
		gs := gossip
		gs.Eligibility = &types.HareEligibilityGossip{
			Layer:  lid,
			Round:  round,
			NodeID: sig.NodeID(),
			Eligibility: types.HareEligibility{
				Proof: proofByte,
				Count: eCount,
			},
		}
		data, err := codec.Encode(gs)
		require.NoError(t, err)
		mcp.EXPECT().HandleEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got *types.HareEligibilityGossip) {
				require.NotNil(t, got)
				require.EqualValues(t, gs.Eligibility, got)
			})
		require.Equal(t, nil, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})
}

func TestHandler_CrossDomain(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
	mcp := malfeasance.NewMockconsensusProtocol(gomock.NewController(t))
	sigVerifier, err := signing.NewEdVerifier()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, sigVerifier)
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

	msg, err := codec.Encode(&types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Proof: types.Proof{
				Type: types.MultipleBallots,
				Data: &types.BallotProof{
					Messages: [2]types.BallotProofMsg{
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
