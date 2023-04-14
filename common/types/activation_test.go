package types_test

import (
	"bytes"
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
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
	_, err := object.EncodeScale(enc)
	require.NoError(t, err)

	var epoch types.EpochID
	require.NoError(t, codec.DecodeSome(buf.Bytes(), &epoch))
	require.Equal(t, object.PublishEpoch, epoch)
}

func TestActivation_BadMsgHash(t *testing.T) {
	challenge := types.NIPostChallenge{
		PublishEpoch: types.EpochID(11),
	}
	atx := types.NewActivationTx(challenge, types.Address{}, nil, 1, nil, nil)
	atx.Signature = types.RandomEdSignature()
	atx.SmesherID = types.RandomNodeID()
	atx.SetID(types.RandomATXID())
	require.Error(t, atx.Initialize())
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

func FuzzVRFPostIndexConsistency(f *testing.F) {
	tester.FuzzConsistency[types.VRFPostIndex](f)
}

func FuzzVRFPostIndexTxStateSafety(f *testing.F) {
	tester.FuzzSafety[types.VRFPostIndex](f)
}

func FuzzPostConsistency(f *testing.F) {
	tester.FuzzConsistency[types.Post](f)
}

func FuzzPostStateSafety(f *testing.F) {
	tester.FuzzSafety[types.Post](f)
}
