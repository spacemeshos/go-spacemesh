package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

type mockPatternProvider struct {
	val uint32
	err error
}

func (mpp *mockPatternProvider) GetPatternId(layer types.LayerID) (uint32, error) {
	return mpp.val, mpp.err
}

func TestBeacon_Value(t *testing.T) {
	b := beacon{}
	b.patternProvider = &mockPatternProvider{1, someErr}
	_, err := b.Value(5)
	assert.NotNil(t, err)
	b.patternProvider = &mockPatternProvider{3, nil}
	val, err := b.Value(5)
	assert.Nil(t, err)
	assert.Equal(t, uint32(3), val)
}
