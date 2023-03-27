package beacon

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
)

func FuzzProposalConsistency(f *testing.F) {
	tester.FuzzConsistency[Proposal](f)
}

func FuzzProposalSafety(f *testing.F) {
	tester.FuzzSafety[Proposal](f)
}
