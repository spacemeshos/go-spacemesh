package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

type mockPatternProvider struct {
	val        uint32
	valGenesis uint32
	err        error
}

func (mpp *mockPatternProvider) GetGoodPattern(layer types.LayerID) (uint32, error) {
	if layer == config.Genesis {
		return mpp.valGenesis, mpp.err
	}
	return mpp.val, mpp.err
}

func TestBeacon_Value(t *testing.T) {
	b := beacon{}
	b.patternProvider = &mockPatternProvider{1, 5, someErr}
	b.confidenceParam = cfg.ConfidenceParam
	_, err := b.Value(100)
	assert.NotNil(t, err)
	b.patternProvider = &mockPatternProvider{3, 5, nil}
	val, err := b.Value(100)
	assert.Nil(t, err)
	assert.Equal(t, uint32(3), val)
	val, err = b.Value(1)
	assert.Nil(t, err)
	assert.Equal(t, uint32(5), val)
}
