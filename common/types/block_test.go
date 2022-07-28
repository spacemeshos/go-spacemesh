package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types/address"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

func TestBlock_IDSize(t *testing.T) {
	var id BlockID
	assert.Len(t, id.Bytes(), BlockIDSize)
}

func TestRewardCodec(t *testing.T) {
	weight := util.WeightFromUint64(1234).Div(util.WeightFromUint64(7))
	r := &AnyReward{
		Coinbase: address.GenerateAddress(RandomBytes(address.AddressLength)),
		Weight:   RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()},
	}

	data, err := codec.Encode(r)
	require.NoError(t, err)

	var got AnyReward
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, r, &got)
}

func FuzzAnyRewardConsistency(f *testing.F) {
	tester.FuzzConsistency[AnyReward](f)
}

func FuzzAnyRewardSafety(f *testing.F) {
	tester.FuzzSafety[AnyReward](f)
}

func FuzzBlockIDConsistency(f *testing.F) {
	tester.FuzzConsistency[BlockID](f)
}

func FuzzBlockIDSafety(f *testing.F) {
	tester.FuzzSafety[BlockID](f)
}

func FuzzRatNumConsistency(f *testing.F) {
	tester.FuzzConsistency[RatNum](f)
}

func FuzzRatNumSafety(f *testing.F) {
	tester.FuzzSafety[RatNum](f)
}

func FuzzBlockConsistency(f *testing.F) {
	tester.FuzzConsistency[Block](f)
}

func FuzzBlockSafety(f *testing.F) {
	tester.FuzzSafety[Block](f)
}

func FuzzBlockContextualValidityConsistency(f *testing.F) {
	tester.FuzzConsistency[BlockContextualValidity](f)
}

func FuzzBlockContextualValiditySafety(f *testing.F) {
	tester.FuzzSafety[BlockContextualValidity](f)
}

func FuzzInnerBlockConsistency(f *testing.F) {
	tester.FuzzConsistency[InnerBlock](f)
}

func FuzzInnerBlockSafety(f *testing.F) {
	tester.FuzzSafety[InnerBlock](f)
}
