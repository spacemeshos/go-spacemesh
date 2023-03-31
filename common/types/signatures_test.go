package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"
)

func FuzzBuilderStateConsistency(f *testing.F) {
	tester.FuzzConsistency[EdSignature](f)
}

func FuzzBuilderStateSafety(f *testing.F) {
	tester.FuzzSafety[EdSignature](f)
}

func Test_SignatureCompare(t *testing.T) {
	var s1 VrfSignature
	s1[79] = 0x01

	var s2 VrfSignature
	s2[79] = 0x02

	require.Equal(t, -1, s1.Cmp(&s2))

	var s3 VrfSignature
	s3[75] = 0x01

	var s4 VrfSignature
	s4[74] = 0x0f

	require.Equal(t, 1, s3.Cmp(&s4))
}

func Test_SignatureCompareNil(t *testing.T) {
	var s1 VrfSignature
	s1[79] = 0x01

	require.Equal(t, -1, s1.Cmp(nil))
}
