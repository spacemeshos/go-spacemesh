package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
)

func FuzzTransactionConsistency(f *testing.F) {
	tester.FuzzConsistency[Transaction](f)
}

func FuzzTransactionSafety(f *testing.F) {
	tester.FuzzSafety[Transaction](f)
}

func FuzzRewardConsistency(f *testing.F) {
	tester.FuzzConsistency[Reward](f)
}

func FuzzRewardSafety(f *testing.F) {
	tester.FuzzSafety[Reward](f)
}

func FuzzRawTxConsistency(f *testing.F) {
	tester.FuzzConsistency[RawTx](f)
}

func FuzzRawTxSafety(f *testing.F) {
	tester.FuzzSafety[RawTx](f)
}
