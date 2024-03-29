package types_test

import (
	"bytes"
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/wire"
)

func TestRoundEndSerialization(t *testing.T) {
	end := types.RoundEnd(time.Now())
	var data bytes.Buffer
	_, err := end.EncodeScale(scale.NewEncoder(&data))
	require.NoError(t, err)

	var deserialized types.RoundEnd
	_, err = deserialized.DecodeScale(scale.NewDecoder(&data))
	require.NoError(t, err)

	require.EqualValues(t, end.IntoTime().Unix(), deserialized.IntoTime().Unix())
}

func TestActivationEncoding(t *testing.T) {
	var object types.ActivationTx
	f := fuzz.NewWithSeed(1001)
	f.Fuzz(&object)

	buf := bytes.NewBuffer(nil)
	enc := scale.NewEncoder(buf)
	_, err := object.ToWireV1().EncodeScale(enc)
	require.NoError(t, err)

	var epoch types.EpochID
	_, err = epoch.DecodeScale(scale.NewDecoder(buf))
	require.NoError(t, err)
	require.Equal(t, object.PublishEpoch, epoch)
}

func TestActivation_BadMsgHash(t *testing.T) {
	challenge := types.NIPostChallenge{
		PublishEpoch: types.EpochID(11),
	}
	atx := types.NewActivationTx(challenge, types.Address{}, types.NIPost{}, 1, nil)
	atx.Signature = types.RandomEdSignature()
	atx.SmesherID = types.RandomNodeID()
	atx.SetID(types.RandomATXID())
	require.Error(t, atx.Initialize())
}

func TestActivationTxFromWireV1(t *testing.T) {
	t.Run("parse valid initial ATX", func(t *testing.T) {
		t.Parallel()
		atxV1 := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NodeID: &types.Hash32{4, 5, 6},
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PositioningATX: types.Hash32{1, 2, 3},
					InitialPost:    &wire.PostV1{},
					CommitmentATX:  &types.Hash32{1, 2, 3},
				},
				NIPost: &wire.NIPostV1{
					Post:         &wire.PostV1{},
					PostMetadata: &wire.PostMetadataV1{},
				},
			},
		}
		_, err := types.ActivationTxFromWireV1(&atxV1)
		require.NoError(t, err)
	})
	t.Run("parse valid non-initial ATX", func(t *testing.T) {
		t.Parallel()
		atxV1 := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PositioningATX: types.Hash32{1, 2, 3},
					PrevATXID:      types.Hash32{1, 2, 3},
					Sequence:       1,
				},
				NIPost: &wire.NIPostV1{
					Post:         &wire.PostV1{},
					PostMetadata: &wire.PostMetadataV1{},
				},
			},
		}
		_, err := types.ActivationTxFromWireV1(&atxV1)
		require.NoError(t, err)
	})
	t.Run("prevAtx declared but NodeID is included", func(t *testing.T) {
		t.Parallel()

		atxV1 := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NodeID: &types.Hash32{4, 5, 6},
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PrevATXID: types.Hash32{1, 2, 3},
				},
			},
		}
		_, err := types.ActivationTxFromWireV1(&atxV1)
		require.ErrorContains(t, err, "non-nil NodeID in non-initial ATX")
	})
	t.Run("prevAtx NOT declared but NodeID is nil", func(t *testing.T) {
		t.Parallel()

		atxV1 := wire.ActivationTxV1{}
		_, err := types.ActivationTxFromWireV1(&atxV1)
		require.ErrorContains(t, err, "nil NodeID in initial ATX")
	})
	t.Run("nipost not present", func(t *testing.T) {
		t.Parallel()
		atxV1 := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PositioningATX: types.Hash32{1, 2, 3},
					PrevATXID:      types.Hash32{1, 2, 3},
					Sequence:       1,
				},
			},
		}
		_, err := types.ActivationTxFromWireV1(&atxV1)
		require.ErrorContains(t, err, "nil nipost")
	})
	t.Run("nipost.post not present", func(t *testing.T) {
		t.Parallel()
		atxV1 := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PositioningATX: types.Hash32{1, 2, 3},
					PrevATXID:      types.Hash32{1, 2, 3},
					Sequence:       1,
				},
				NIPost: &wire.NIPostV1{
					PostMetadata: &wire.PostMetadataV1{},
				},
			},
		}
		_, err := types.ActivationTxFromWireV1(&atxV1)
		require.ErrorContains(t, err, "nil nipost.post")
	})
	t.Run("nipost.postmetadata not present", func(t *testing.T) {
		t.Parallel()
		atxV1 := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PositioningATX: types.Hash32{1, 2, 3},
					PrevATXID:      types.Hash32{1, 2, 3},
					Sequence:       1,
				},
				NIPost: &wire.NIPostV1{
					Post: &wire.PostV1{},
				},
			},
		}
		_, err := types.ActivationTxFromWireV1(&atxV1)
		require.ErrorContains(t, err, "nil nipost.postmetadata")
	})
}

func FuzzEpochIDConsistency(f *testing.F) {
	tester.FuzzConsistency[types.EpochID](f)
}

func FuzzEpochIDStateSafety(f *testing.F) {
	tester.FuzzSafety[types.EpochID](f)
}

func FuzzATXIDConsistency(f *testing.F) {
	tester.FuzzConsistency[types.ATXID](f)
}

func FuzzATXIDStateSafety(f *testing.F) {
	tester.FuzzSafety[types.ATXID](f)
}

func FuzzMemberConsistency(f *testing.F) {
	tester.FuzzConsistency[types.Member](f)
}

func FuzzMemberStateSafety(f *testing.F) {
	tester.FuzzSafety[types.Member](f)
}

func FuzzRoundEndConsistency(f *testing.F) {
	tester.FuzzConsistency[types.RoundEnd](f)
}

func FuzzRoundEndStateSafety(f *testing.F) {
	tester.FuzzSafety[types.RoundEnd](f)
}
