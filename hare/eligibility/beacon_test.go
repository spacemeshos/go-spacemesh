package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

type mockPatternProvider struct {
	val        map[types.BlockID]struct{}
	valGenesis map[types.BlockID]struct{}
	err        error
}

func (mpp *mockPatternProvider) ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	if layer == types.GetEffectiveGenesis() {
		return mpp.valGenesis, mpp.err
	}
	return mpp.val, mpp.err
}

func TestBeacon_Value(t *testing.T) {
	genesisBlock := types.NewExistingBlock(types.GetEffectiveGenesis(), []byte("genesis"))

	block1 := types.NewExistingBlock(0, []byte("asghsfgdhn"))
	block2 := types.NewExistingBlock(0, []byte("asghdhn"))
	block3 := types.NewExistingBlock(0, []byte("asghsfg"))

	r := require.New(t)

	b := NewBeacon(nil, 0, log.NewDefault(t.Name()))
	c := newMockCasher()
	b.cache = c

	genesisGoodPtrn := map[types.BlockID]struct{}{}
	genesisGoodPtrn[genesisBlock.ID()] = struct{}{}

	valGoodPtrn := map[types.BlockID]struct{}{}
	valGoodPtrn[block1.ID()] = struct{}{}
	valGoodPtrn[block2.ID()] = struct{}{}
	valGoodPtrn[block3.ID()] = struct{}{}

	b.patternProvider = &mockPatternProvider{valGoodPtrn, genesisGoodPtrn, errFoo}
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
	b := NewBeacon(p, 10, log.NewDefault(t.Name()))
	r.Equal(p, b.patternProvider)
	r.Equal(uint64(10), b.confidenceParam)
	r.NotNil(p, b.cache)
}
