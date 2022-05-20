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

func TestReward(t *testing.T) {
	addr := BytesToAddress(RandomBytes(AddressLength))
	weight := util.WeightFromUint64(1234).Div(util.WeightFromUint64(7))
	weightB, err := weight.GobEncode()
	require.NoError(t, err)
	r := AnyReward{
		Coinbase: addr,
		Weight:   weightB,
	}
	data, err := codec.Encode(r)
	require.NoError(t, err)

	var got AnyReward
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, r, got)

	decodedW := util.WeightFromUint64(0)
	require.NoError(t, decodedW.GobDecode(got.Weight))
	require.Equal(t, 0, weight.Cmp(decodedW))
}
