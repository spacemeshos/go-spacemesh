package types_test

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

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
