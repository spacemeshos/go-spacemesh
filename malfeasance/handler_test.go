package malfeasance_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
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
		PubLayerID: types.NewLayerID(1),
	}
	nodeID := sig.NodeID()
	atx := types.NewActivationTx(challenge, &nodeID, types.Address{}, nil, 1, nil, nil)
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
	pke, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, pke)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

	atxProof := types.AtxProof{
		Messages: [2]types.AtxProofMsg{
			{
				InnerMsg: types.ATXMetadata{
					Target:  types.EpochID(3),
					MsgHash: types.RandomHash(),
				},
			},
			{
				InnerMsg: types.ATXMetadata{
					Target:  types.EpochID(3),
					MsgHash: types.RandomHash(),
				},
			},
		},
	}

	t.Run("unknown identity", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(ap.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

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
		ap.Messages[0].Signature = sig.Sign(ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(ap.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different epoch", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].InnerMsg.Target = ap.Messages[1].InnerMsg.Target + 1
		ap.Messages[0].Signature = sig.Sign(ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(ap.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(ap.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		ap.Messages[1].Signature = sig2.Sign(ap.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("invalid hare eligibility", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(ap.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
					Data: &ap,
				},
			},
			Eligibility: &types.HareEligibilityGossip{
				PubKey: types.RandomBytes(64),
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})

	t.Run("valid", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(ap.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		ap := atxProof
		ap.Messages[0].Signature = sig.Sign(ap.Messages[0].SignedBytes())
		ap.Messages[1].Signature = sig.Sign(ap.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))
		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)

		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "self", data))
		malProof, err = identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)
	})
}

func TestHandler_HandleMalfeasanceProof_multipleBallots(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
	mcp := malfeasance.NewMockconsensusProtocol(gomock.NewController(t))
	pke, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, pke)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

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
		bp.Messages[0].Signature = sig.Sign(bp.Messages[0].SignedBytes())
		bp.Messages[1].Signature = sig.Sign(bp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

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
		bp.Messages[0].Signature = sig.Sign(bp.Messages[0].SignedBytes())
		bp.Messages[1].Signature = sig.Sign(bp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different layer", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[1].InnerMsg.Layer = lid.Sub(1)
		bp.Messages[0].Signature = sig.Sign(bp.Messages[0].SignedBytes())
		bp.Messages[1].Signature = sig.Sign(bp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(bp.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		bp.Messages[1].Signature = sig2.Sign(bp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("invalid hare eligibility", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(bp.Messages[0].SignedBytes())
		bp.Messages[1].Signature = sig.Sign(bp.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
					Data: &bp,
				},
			},
			Eligibility: &types.HareEligibilityGossip{
				PubKey: types.RandomBytes(64),
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})

	t.Run("valid", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(bp.Messages[0].SignedBytes())
		bp.Messages[1].Signature = sig.Sign(bp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		bp := ballotProof
		bp.Messages[0].Signature = sig.Sign(bp.Messages[0].SignedBytes())
		bp.Messages[1].Signature = sig.Sign(bp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))
		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)

		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "self", data))
		malProof, err = identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)
	})
}

func TestHandler_HandleMalfeasanceProof_hareEquivocation(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
	mcp := malfeasance.NewMockconsensusProtocol(gomock.NewController(t))
	pke, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, pke)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

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
		hp.Messages[0].Signature = sig.Sign(hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(hp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

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
		hp.Messages[0].Signature = sig.Sign(hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(hp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different layer", func(t *testing.T) {
		hp := hareProof
		hp.Messages[1].InnerMsg.Layer = lid.Sub(1)
		hp.Messages[0].Signature = sig.Sign(hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(hp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different round", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].InnerMsg.Round = hp.Messages[1].InnerMsg.Round + 1
		hp.Messages[0].Signature = sig.Sign(hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(hp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(hp.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		hp.Messages[1].Signature = sig2.Sign(hp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("invalid hare eligibility", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(hp.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hp,
				},
			},
			Eligibility: &types.HareEligibilityGossip{
				PubKey: types.RandomBytes(64),
			},
		}
		data, err := codec.Encode(gossip)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})

	t.Run("valid", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(hp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		hp := hareProof
		hp.Messages[0].Signature = sig.Sign(hp.Messages[0].SignedBytes())
		hp.Messages[1].Signature = sig.Sign(hp.Messages[1].SignedBytes())
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
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))
		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)

		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "self", data))
		malProof, err = identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)
	})
}

func TestHandler_HandleMalfeasanceProof_validateHare(t *testing.T) {
	db := sql.InMemory()
	lg := logtest.New(t)
	mcp := malfeasance.NewMockconsensusProtocol(gomock.NewController(t))
	pke, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)

	h := malfeasance.NewHandler(datastore.NewCachedDB(db, lg), lg, "self", mcp, pke)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	createIdentity(t, db, sig)
	lid := types.NewLayerID(11)
	round := uint32(11)
	proofByte := types.RandomBytes(64)
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
	bp.Messages[0].Signature = sig.Sign(bp.Messages[0].SignedBytes())
	bp.Messages[1].Signature = sig.Sign(bp.Messages[1].SignedBytes())
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
			PubKey: types.RandomBytes(64),
		}
		data, err := codec.Encode(gs)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})

	t.Run("relay eligibility", func(t *testing.T) {
		gs := gossip
		gs.Eligibility = &types.HareEligibilityGossip{
			Layer:  lid,
			Round:  round,
			PubKey: sig.PublicKey().Bytes(),
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
		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "peer", data))
	})
}
