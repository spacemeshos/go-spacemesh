package wire_test

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"

	"github.com/spacemeshos/go-spacemesh/common/types/wire"
)

func FuzzVRFPostIndexConsistency(f *testing.F) {
	tester.FuzzConsistency[wire.VRFPostIndex](f)
}

func FuzzVRFPostIndexTxStateSafety(f *testing.F) {
	tester.FuzzSafety[wire.VRFPostIndex](f)
}
