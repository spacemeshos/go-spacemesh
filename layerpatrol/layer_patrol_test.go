package layerpatrol

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Test_GetAndSetHareInCharge(t *testing.T) {
	patrol := New()
	lyr := types.LayerID(10)
	for i := lyr; !i.After(lyr.Add(bufferSize)); i = i.Add(1) {
		patrol.SetHareInCharge(i)
		assert.True(t, patrol.IsHareInCharge(i))
		assert.False(t, patrol.IsHareInCharge(i.Add(1)))
	}
	// the original layer should no longer be in the buffer
	assert.False(t, patrol.IsHareInCharge(lyr))
	// but the next one is still there
	exists := lyr.Add(1)
	assert.True(t, patrol.IsHareInCharge(exists))

	patrol.CompleteHare(exists)
	assert.False(t, patrol.IsHareInCharge(exists))
}
