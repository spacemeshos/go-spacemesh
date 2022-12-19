package types_test

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func FuzzActivationConsistency(f *testing.F) {
	tester.FuzzConsistency[types.ActivationTx](f)
}

func FuzzActivationTxStateSafety(f *testing.F) {
	tester.FuzzSafety[types.ActivationTx](f)
}

func TestActivationEncoding(t *testing.T) {
	types.CheckLayerFirstEncoding(t, func(object types.ActivationTx) types.LayerID { return object.PubLayerID })
}
