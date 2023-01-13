package malfeasance_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func TestHandler_handleProof_multipleATXs(t *testing.T) {
	db := sql.InMemory()
	h := malfeasance.NewHandler(db, logtest.New(t), "self")
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

	t.Run("same msg hash", func(t *testing.T) {
		msgHash := types.RandomHash()
		atxProof := types.AtxProof{
			Messages: [2]types.AtxProofMsg{
				{
					InnerMsg: types.ATXMetadata{
						Target:  types.EpochID(3),
						MsgHash: msgHash,
					},
				},
				{
					InnerMsg: types.ATXMetadata{
						Target:  types.EpochID(3),
						MsgHash: msgHash,
					},
				},
			},
		}
		atxProof.Messages[0].Signature = sig.Sign(atxProof.Messages[0].SignedBytes())
		atxProof.Messages[1].Signature = sig.Sign(atxProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
					Data: &atxProof,
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
						Target:  types.EpochID(4),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		for _, msg := range atxProof.Messages {
			msg.Signature = sig.Sign(msg.SignedBytes())
		}
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
					Data: &atxProof,
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
						Target:  types.EpochID(4),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		atxProof.Messages[0].Signature = sig.Sign(atxProof.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		atxProof.Messages[1].Signature = sig2.Sign(atxProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
					Data: &atxProof,
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

	t.Run("valid", func(t *testing.T) {
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
		atxProof.Messages[0].Signature = sig.Sign(atxProof.Messages[0].SignedBytes())
		atxProof.Messages[1].Signature = sig.Sign(atxProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleATXs,
					Data: &atxProof,
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
		atxProof := types.AtxProof{
			Messages: [2]types.AtxProofMsg{
				{
					InnerMsg: types.ATXMetadata{
						Target:  types.EpochID(4),
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: types.ATXMetadata{
						Target:  types.EpochID(4),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		atxProof.Messages[0].Signature = sig.Sign(atxProof.Messages[0].SignedBytes())
		atxProof.Messages[1].Signature = sig.Sign(atxProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: types.Proof{
					Type: types.MultipleATXs,
					Data: &atxProof,
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

func TestHandler_handleProof_multipleBallots(t *testing.T) {
	db := sql.InMemory()
	h := malfeasance.NewHandler(db, logtest.New(t), "self")
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

	t.Run("same msg hash", func(t *testing.T) {
		msgHash := types.RandomHash()
		ballotProof := types.BallotProof{
			Messages: [2]types.BallotProofMsg{
				{
					InnerMsg: types.BallotMetadata{
						Layer:   lid,
						MsgHash: msgHash,
					},
				},
				{
					InnerMsg: types.BallotMetadata{
						Layer:   lid,
						MsgHash: msgHash,
					},
				},
			},
		}
		ballotProof.Messages[0].Signature = sig.Sign(ballotProof.Messages[0].SignedBytes())
		ballotProof.Messages[1].Signature = sig.Sign(ballotProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
					Data: &ballotProof,
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
						Layer:   lid.Sub(1),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		ballotProof.Messages[0].Signature = sig.Sign(ballotProof.Messages[0].SignedBytes())
		ballotProof.Messages[1].Signature = sig.Sign(ballotProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
					Data: &ballotProof,
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
		ballotProof.Messages[0].Signature = sig.Sign(ballotProof.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		ballotProof.Messages[1].Signature = sig2.Sign(ballotProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
					Data: &ballotProof,
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

	t.Run("valid", func(t *testing.T) {
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
		ballotProof.Messages[0].Signature = sig.Sign(ballotProof.Messages[0].SignedBytes())
		ballotProof.Messages[1].Signature = sig.Sign(ballotProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.MultipleBallots,
					Data: &ballotProof,
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
		ballotProof := types.BallotProof{
			Messages: [2]types.BallotProofMsg{
				{
					InnerMsg: types.BallotMetadata{
						Layer:   lid.Add(9),
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: types.BallotMetadata{
						Layer:   lid.Add(9),
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		ballotProof.Messages[0].Signature = sig.Sign(ballotProof.Messages[0].SignedBytes())
		ballotProof.Messages[1].Signature = sig.Sign(ballotProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: types.Proof{
					Type: types.MultipleBallots,
					Data: &ballotProof,
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

func TestHandler_handleProof_hareEquivocation(t *testing.T) {
	db := sql.InMemory()
	h := malfeasance.NewHandler(db, logtest.New(t), "self")
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

	t.Run("same msg hash", func(t *testing.T) {
		msgHash := types.RandomHash()
		hareProof := types.HareProof{
			Messages: [2]types.HareProofMsg{
				{
					InnerMsg: types.HareMetadata{
						Layer:   lid,
						Round:   3,
						MsgHash: msgHash,
					},
				},
				{
					InnerMsg: types.HareMetadata{
						Layer:   lid,
						Round:   3,
						MsgHash: msgHash,
					},
				},
			},
		}
		hareProof.Messages[0].Signature = sig.Sign(hareProof.Messages[0].SignedBytes())
		hareProof.Messages[1].Signature = sig.Sign(hareProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hareProof,
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
						Layer:   lid.Sub(1),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hareProof.Messages[0].Signature = sig.Sign(hareProof.Messages[0].SignedBytes())
		hareProof.Messages[1].Signature = sig.Sign(hareProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hareProof,
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
		hareProof := types.HareProof{
			Messages: [2]types.HareProofMsg{
				{
					InnerMsg: types.HareMetadata{
						Layer:   lid,
						Round:   2,
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
		hareProof.Messages[0].Signature = sig.Sign(hareProof.Messages[0].SignedBytes())
		hareProof.Messages[1].Signature = sig.Sign(hareProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hareProof,
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
						Layer:   lid.Sub(1),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hareProof.Messages[0].Signature = sig.Sign(hareProof.Messages[0].SignedBytes())
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		hareProof.Messages[1].Signature = sig2.Sign(hareProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hareProof,
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

	t.Run("valid", func(t *testing.T) {
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
		hareProof.Messages[0].Signature = sig.Sign(hareProof.Messages[0].SignedBytes())
		hareProof.Messages[1].Signature = sig.Sign(hareProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid,
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hareProof,
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
		hareProof := types.HareProof{
			Messages: [2]types.HareProofMsg{
				{
					InnerMsg: types.HareMetadata{
						Layer:   lid.Add(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
				{
					InnerMsg: types.HareMetadata{
						Layer:   lid.Add(11),
						Round:   3,
						MsgHash: types.RandomHash(),
					},
				},
			},
		}
		hareProof.Messages[0].Signature = sig.Sign(hareProof.Messages[0].SignedBytes())
		hareProof.Messages[1].Signature = sig.Sign(hareProof.Messages[1].SignedBytes())
		gossip := &types.MalfeasanceGossip{
			MalfeasanceProof: types.MalfeasanceProof{
				Layer: lid.Add(11),
				Proof: types.Proof{
					Type: types.HareEquivocation,
					Data: &hareProof,
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
