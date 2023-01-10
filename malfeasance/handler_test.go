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
	h := malfeasance.NewHandler(db, logtest.New(t))
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

	t.Run("same msg hash", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.MultipleATXs,
		}
		msg := types.MultiATXsMsg{
			InnerMsg: types.ATXMetadata{
				Target:  types.EpochID(3),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different epoch", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.MultipleATXs,
		}
		msg := types.MultiATXsMsg{
			InnerMsg: types.ATXMetadata{
				Target:  types.EpochID(3),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.Target = msg.InnerMsg.Target + 1
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.MultipleATXs,
		}
		msg := types.MultiATXsMsg{
			InnerMsg: types.ATXMetadata{
				Target:  types.EpochID(3),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		msg.Signature = sig2.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("valid", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.MultipleATXs,
		}
		msg := types.MultiATXsMsg{
			InnerMsg: types.ATXMetadata{
				Target:  types.EpochID(3),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.MsgHash = types.RandomHash()
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.EqualValues(t, proof, malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid.Add(11),
			Type:  types.MultipleATXs,
		}
		msg := types.MultiATXsMsg{
			InnerMsg: types.ATXMetadata{
				Target:  types.EpochID(4),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.MsgHash = types.RandomHash()
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqualValues(t, proof, malProof)
	})
}

func TestHandler_handleProof_multipleBallots(t *testing.T) {
	db := sql.InMemory()
	h := malfeasance.NewHandler(db, logtest.New(t))
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

	t.Run("same msg hash", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.MultipleBallots,
		}
		msg := types.MultiBallotsMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   lid.Sub(1),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different layer", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.MultipleBallots,
		}
		msg := types.MultiBallotsMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   lid.Sub(1),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.Layer = msg.InnerMsg.Layer.Add(1)
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.MultipleBallots,
		}
		msg := types.MultiBallotsMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   lid.Sub(1),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		msg.Signature = sig2.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("valid", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.MultipleBallots,
		}
		msg := types.MultiBallotsMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   lid.Sub(1),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.MsgHash = types.RandomHash()
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.EqualValues(t, proof, malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid.Add(10),
			Type:  types.MultipleBallots,
		}
		msg := types.MultiBallotsMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   lid.Add(9),
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.MsgHash = types.RandomHash()
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqualValues(t, proof, malProof)
	})
}

func TestHandler_handleProof_hareEquivocation(t *testing.T) {
	db := sql.InMemory()
	h := malfeasance.NewHandler(db, logtest.New(t))
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	lid := types.NewLayerID(11)

	t.Run("same msg hash", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.HareEquivocation,
		}
		msg := types.HareEquivocationMsg{
			InnerMsg: types.HareMetadata{
				Layer:   lid.Sub(1),
				Round:   3,
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different layer", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.HareEquivocation,
		}
		msg := types.HareEquivocationMsg{
			InnerMsg: types.HareMetadata{
				Layer:   lid.Sub(1),
				Round:   3,
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.Layer = msg.InnerMsg.Layer.Add(1)
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different round", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.HareEquivocation,
		}
		msg := types.HareEquivocationMsg{
			InnerMsg: types.HareMetadata{
				Layer:   lid.Sub(1),
				Round:   3,
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.Round = msg.InnerMsg.Round + 1
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("different signer", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.HareEquivocation,
		}
		msg := types.HareEquivocationMsg{
			InnerMsg: types.HareMetadata{
				Layer:   lid.Sub(1),
				Round:   3,
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		msg.Signature = sig2.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, malProof)
	})

	t.Run("valid", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid,
			Type:  types.HareEquivocation,
		}
		msg := types.HareEquivocationMsg{
			InnerMsg: types.HareMetadata{
				Layer:   lid.Sub(1),
				Round:   3,
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.MsgHash = types.RandomHash()
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationAccept, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.EqualValues(t, proof, malProof)
	})

	t.Run("proof equivalence", func(t *testing.T) {
		proof := &types.MalfeasanceProof{
			Layer: lid.Add(11),
			Type:  types.HareEquivocation,
		}
		msg := types.HareEquivocationMsg{
			InnerMsg: types.HareMetadata{
				Layer:   lid.Add(11),
				Round:   3,
				MsgHash: types.RandomHash(),
			},
		}
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		msg.InnerMsg.MsgHash = types.RandomHash()
		msg.Signature = sig.Sign(msg.SignedBytes())
		proof.Messages = append(proof.Messages, msg)
		data, err := codec.Encode(proof)
		require.NoError(t, err)
		require.Equal(t, pubsub.ValidationIgnore, h.HandleMalfeasanceProof(context.Background(), "peer", data))

		malProof, err := identities.GetMalfeasanceProof(db, sig.NodeID())
		require.NoError(t, err)
		require.NotEqualValues(t, proof, malProof)
	})
}
