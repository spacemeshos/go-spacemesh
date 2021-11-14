package tortoise

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestPrioritizeBlocks(t *testing.T) {
	blocks := []types.BlockID{
		{1},
		{2},
		{3},
		{4},
	}
	for _, tc := range []struct {
		desc         string
		goodBlocks   map[types.BlockID]bool
		disagrements map[types.BlockID]types.LayerID
		blockLayer   map[types.BlockID]types.LayerID
		expect       []types.BlockID
	}{
		{
			desc:   "SortLexically",
			expect: blocks,
		},
		{
			desc:       "PrioritizeGoodBlocks",
			expect:     append([]types.BlockID{blocks[3]}, blocks[:3]...),
			goodBlocks: map[types.BlockID]bool{blocks[3]: false},
		},
		{
			desc:   "PrioritizeWithHigherDisagreementLayer",
			expect: append([]types.BlockID{blocks[3], blocks[2]}, blocks[:2]...),
			goodBlocks: map[types.BlockID]bool{
				blocks[2]: false,
				blocks[3]: false,
			},
			disagrements: map[types.BlockID]types.LayerID{
				blocks[2]: types.NewLayerID(9),
				blocks[3]: types.NewLayerID(10),
			},
		},
		{
			desc:   "PrioritizeByHigherLayer",
			expect: append([]types.BlockID{blocks[3], blocks[2]}, blocks[:2]...),
			goodBlocks: map[types.BlockID]bool{
				blocks[2]: false,
				blocks[3]: false,
			},
			disagrements: map[types.BlockID]types.LayerID{
				blocks[2]: types.NewLayerID(9),
				blocks[3]: types.NewLayerID(9),
			},
			blockLayer: map[types.BlockID]types.LayerID{
				blocks[2]: types.NewLayerID(9),
				blocks[3]: types.NewLayerID(10),
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			rst := make([]types.BlockID, len(blocks))
			copy(rst, blocks)

			rng := rand.New(rand.NewSource(10001))
			rng.Shuffle(len(rst), func(i, j int) {
				rst[i], rst[j] = rst[j], rst[i]
			})

			prioritizeBlocks(rst, tc.goodBlocks, tc.disagrements, tc.blockLayer)
			require.Equal(t, tc.expect, rst)
		})
	}
}
