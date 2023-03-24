package signing

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
)

func FuzzEdSignatureConsistency(f *testing.F) {
	tester.FuzzConsistency[EdSignature](f)
}

func FuzzEdSignatureSafety(f *testing.F) {
	tester.FuzzSafety[EdSignature](f)
}

func FuzzVrfSignatureConsistency(f *testing.F) {
	tester.FuzzConsistency[VrfSignature](f)
}

func FuzzVrfSignatureSafety(f *testing.F) {
	tester.FuzzSafety[VrfSignature](f)
}
