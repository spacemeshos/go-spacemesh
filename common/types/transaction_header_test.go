package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
)

func FuzzTxHeaderConsistency(f *testing.F) {
	tester.FuzzConsistency[TxHeader](f)
}

func FuzzTxHeaderSafety(f *testing.F) {
	tester.FuzzSafety[TxHeader](f)
}

func FuzzLayerLimitsConsistency(f *testing.F) {
	tester.FuzzConsistency[LayerLimits](f)
}

func FuzzLayerLimitsSafety(f *testing.F) {
	tester.FuzzSafety[LayerLimits](f)
}

func FuzzNonceConsistency(f *testing.F) {
	tester.FuzzConsistency[Nonce](f)
}

func FuzzNonceSafety(f *testing.F) {
	tester.FuzzSafety[Nonce](f)
}
