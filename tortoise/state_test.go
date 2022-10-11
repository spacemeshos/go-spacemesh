package tortoise

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestVotesUpdate(t *testing.T) {
	t.Run("no copies", func(t *testing.T) {
		original := votes{}
		const last = 10
		for i := 0; i < last; i++ {
			original.append(&layerVote{layerInfo: &layerInfo{lid: types.NewLayerID(uint32(i))}})
		}
		cp := original.update(types.NewLayerID(last), nil)
		c1 := original.tail
		c2 := cp.tail
		for c1 != nil || c2 != nil {
			require.True(t, c1 == c2, "pointers should be equal")
			c1 = c1.prev
			c2 = c2.prev
		}
	})
	t.Run("copy before last", func(t *testing.T) {
		original := votes{}
		const last = 10
		for i := 0; i < last; i++ {
			original.append(&layerVote{layerInfo: &layerInfo{lid: types.NewLayerID(uint32(i))}})
		}
		const modified = last - 2
		cp := original.update(types.NewLayerID(modified), nil)
		c1 := original.tail
		c2 := cp.tail
		for c1 != nil || c2 != nil {
			if c1.lid.Value >= modified {
				require.False(t, c1 == c2)
			} else {
				require.True(t, c1 == c2)
			}
			c1 = c1.prev
			c2 = c2.prev
		}
	})
	t.Run("update abstain", func(t *testing.T) {
		original := votes{}
		const last = 10
		update := map[types.LayerID]map[types.BlockID]sign{}
		for i := 0; i < last; i++ {
			original.append(&layerVote{
				vote:      against,
				layerInfo: &layerInfo{lid: types.NewLayerID(uint32(i))},
			})
			update[types.NewLayerID(uint32(i))] = map[types.BlockID]sign{}
		}
		cp := original.update(types.NewLayerID(0), update)
		for c := original.tail; c != nil; c = c.prev {
			require.Equal(t, against, c.vote)
		}
		for c := cp.tail; c != nil; c = c.prev {
			require.Equal(t, abstain, c.vote)
		}
	})
	t.Run("update blocks", func(t *testing.T) {
		original := votes{}
		const last = 10
		update := map[types.LayerID]map[types.BlockID]sign{}
		for i := 0; i < last; i++ {
			original.append(&layerVote{
				layerInfo: &layerInfo{
					lid: types.NewLayerID(uint32(i)),
					blocks: []*blockInfo{{
						id: types.BlockID{byte(i)},
					}},
				},
			})
			update[types.NewLayerID(uint32(i))] = map[types.BlockID]sign{
				{byte(i)}: support,
			}
		}
		cp := original.update(types.NewLayerID(0), update)
		for c := original.tail; c != nil; c = c.prev {
			require.Len(t, c.supported, 0)
		}
		for c := cp.tail; c != nil; c = c.prev {
			require.Len(t, c.supported, 1)
			require.Equal(t, support, c.getVote(c.supported[0].id))
		}
	})
}
