package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlock_IDSize(t *testing.T) {
	var id BlockID
	assert.Len(t, id.Bytes(), BlockIDSize)
}

func TestDBBlock(t *testing.T) {
	layer := NewLayerID(100)
	b := GenLayerBlock(layer, RandomTXSet(199))
	assert.Equal(t, layer, b.LayerIndex)
	assert.NotEqual(t, b.ID(), EmptyProposalID)
	dbb := &DBBlock{
		ID:         b.ID(),
		InnerBlock: b.InnerBlock,
	}
	got := dbb.ToBlock()
	assert.Equal(t, b, got)
}
