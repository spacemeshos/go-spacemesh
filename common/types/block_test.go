package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

func TestBlock_IDSize(t *testing.T) {
	var id BlockID
	assert.Len(t, id.Bytes(), BlockIDSize)
}

func TestRewardCodec(t *testing.T) {
	addr := BytesToAddress(RandomBytes(AddressLength))
	weight := util.WeightFromUint64(1234).Div(util.WeightFromUint64(7))
	r := &AnyReward{
		Coinbase: addr,
		Weight:   RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()},
	}

	data, err := codec.Encode(r)
	require.NoError(t, err)

	var got AnyReward
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, r, &got)
}

func TestBlockIDCodec(t *testing.T) {
	want := RandomBlockID()

	data, err := codec.Encode(want)
	require.NoError(t, err)

	var got BlockID
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, want, got)
}

func TestRatNumCodec(t *testing.T) {
	weight := util.WeightFromFloat64(94.78)
	want := &RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}

	data, err := codec.Encode(want)
	require.NoError(t, err)

	var got RatNum
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, want, &got)
}

func TestInnerBlockCodec(t *testing.T) {
	weight := util.WeightFromFloat64(312.13)
	want := &InnerBlock{
		LayerIndex: NewLayerID(5),
		Rewards: []AnyReward{
			{
				Coinbase: GenerateAddress(RandomBytes(20)),
				Weight:   RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()},
			},
		},
		TxIDs: RandomTXSet(10),
	}

	data, err := codec.Encode(want)
	require.NoError(t, err)

	var got InnerBlock
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, want, &got)
}

func TestBlockCodec(t *testing.T) {
	weight := util.WeightFromFloat64(312.13)
	want := NewExistingBlock(
		EmptyBlockID,
		InnerBlock{
			LayerIndex: NewLayerID(1),
			Rewards: []AnyReward{
				{
					Coinbase: GenerateAddress(RandomBytes(20)),
					Weight:   RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()},
				},
			},
			TxIDs: RandomTXSet(3),
		},
	)

	data, err := codec.Encode(want)
	require.NoError(t, err)

	var got Block
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, want, &got)
}

func TestBlockContextualValidityCodec(t *testing.T) {
	want := &BlockContextualValidity{
		ID:       RandomBlockID(),
		Validity: true,
	}

	data, err := codec.Encode(want)
	require.NoError(t, err)

	var got BlockContextualValidity
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, want, &got)
}
