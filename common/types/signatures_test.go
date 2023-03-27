package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
)

func FuzzBuilderStateConsistency(f *testing.F) {
	tester.FuzzConsistency[EdSignature](f)
}

func FuzzBuilderStateSafety(f *testing.F) {
	tester.FuzzSafety[EdSignature](f)
}
