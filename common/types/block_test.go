package types

import (
	"testing"

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

func TestReward(t *testing.T) {
	addr, err := address.GenerateAddress(address.TestnetID, RandomBytes(address.AddressLength))
	require.NoError(t, err)
	weight := util.WeightFromUint64(1234).Div(util.WeightFromUint64(7))
	r := AnyReward{
		Coinbase: addr,
		Weight:   RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()},
	}
	data, err := codec.Encode(r)
	require.NoError(t, err)

	var got AnyReward
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, r, got)
}
