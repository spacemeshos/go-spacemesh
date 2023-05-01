package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
)

func FuzzNodeIDConsistency(f *testing.F) {
	tester.FuzzConsistency[NodeID](f)
}

func FuzzNodeIDSafety(f *testing.F) {
	tester.FuzzSafety[NodeID](f)
}
