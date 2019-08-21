package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

type mockPatternProvider struct {
	val        map[types.BlockID]struct{}
	valGenesis map[types.BlockID]struct{}
	err        error
}

func (mpp *mockPatternProvider) GetGoodPattern(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	if layer == config.Genesis {
		return mpp.valGenesis, mpp.err
	}
	return mpp.val, mpp.err
}

func TestBeacon_Value(t *testing.T) {
	r := require.New(t)

	b := beacon{}
	c := newMockCasher()
	b.cache = c

	genesisGoodPtrn := map[types.BlockID]struct{}{}
	genesisGoodPtrn[420] = struct{}{}

	valGoodPtrn := map[types.BlockID]struct{}{}
	valGoodPtrn[1] = struct{}{}
	valGoodPtrn[4] = struct{}{}
	valGoodPtrn[6] = struct{}{}

	b.patternProvider = &mockPatternProvider{valGoodPtrn, genesisGoodPtrn, someErr}
	b.confidenceParam = cfg.ConfidenceParam
	_, err := b.Value(100)
	r.NotNil(err)
	b.patternProvider = &mockPatternProvider{valGoodPtrn, genesisGoodPtrn, nil}
	val, err := b.Value(100)
	r.Nil(err)
	r.Equal(calcValue(valGoodPtrn), val)
	r.Equal(2, c.numGet)
	r.Equal(1, c.numAdd)

	// ensure cache
	val, err = b.Value(100)
	assert.Nil(t, err)
	assert.Equal(t, calcValue(valGoodPtrn), val)
	r.Equal(3, c.numGet)
	r.Equal(1, c.numAdd)

	val, err = b.Value(1)
	assert.Nil(t, err)
	assert.Equal(t, calcValue(genesisGoodPtrn), val)
}

func TestNewBeacon(t *testing.T) {
	r := require.New(t)
	p := &mockPatternProvider{}
	b := NewBeacon(p, 10)
	r.Equal(p, b.patternProvider)
	r.Equal(uint64(10), b.confidenceParam)
	r.NotNil(p, b.cache)
}
