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

func FuzzActivationConsistency(f *testing.F) {
	tester.FuzzConsistency[types.ActivationTx](f)
}

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

func FuzzActivationTxStateSafety(f *testing.F) {
	tester.FuzzSafety[types.ActivationTx](f)
}

func TestActivationEncoding(t *testing.T) {
	types.CheckLayerFirstEncoding(t, func(object types.ActivationTx) types.LayerID { return object.PubLayerID })
}
