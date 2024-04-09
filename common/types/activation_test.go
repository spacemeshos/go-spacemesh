package types_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

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
