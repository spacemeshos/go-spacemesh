package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
)

func FuzzTransactionResultConsistency(f *testing.F) {
	tester.FuzzConsistency[TransactionResult](f)
}

func FuzzTransactionResultSafety(f *testing.F) {
	tester.FuzzSafety[TransactionResult](f)
}

func FuzzTransactionWithResultConsistency(f *testing.F) {
	tester.FuzzConsistency[TransactionWithResult](f)
}

func FuzzTransactionWithResultSafety(f *testing.F) {
	tester.FuzzSafety[TransactionWithResult](f)
}
