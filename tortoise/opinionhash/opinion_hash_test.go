package opinionhash

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestOpinionHasher(t *testing.T) {
	type support struct {
		id     types.BlockID
		height uint64
	}
	type abstain struct{}

	for _, tc := range []struct {
		desc     string
		expected string
		seq      []any
	}{
		{
			desc: "support height 10",
			seq: []any{
				support{id: types.BlockID{1}, height: 10},
			},
			expected: "9792d675f9",
		},
		{
			desc: "support height 100",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
			},
			expected: "fdae510822",
		},
		{
			desc: "multiple blocks",
			seq: []any{
				[]support{{id: types.BlockID{1}, height: 100}, {id: types.BlockID{2}, height: 100}},
			},
			expected: "c0d65bb2f6",
		},
		{
			desc: "single against",
			seq: []any{
				nil,
			},
			expected: "af1349b9f5",
		},
		{
			desc: "abstain",
			seq: []any{
				abstain{},
			},
			expected: "2d3adedff1",
		},
		{
			desc: "abstain abstain",
			seq: []any{
				abstain{},
				abstain{},
			},
			expected: "58716d2acc",
		},
		{
			desc: "against against",
			seq: []any{
				nil,
				nil,
			},
			expected: "82878ed8a4",
		},
		{
			desc: "support support",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
				support{id: types.BlockID{2}, height: 100},
			},
			expected: "88f75279f7",
		},
		{
			desc: "support against",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
				nil,
			},
			expected: "ae4b107c66",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			var prev *types.Hash32
			for _, item := range tc.seq {
				hasher := New()
				if prev != nil {
					hasher.WritePrevious(*prev)
				}
				switch value := item.(type) {
				case nil:
				case support:
					hasher.WriteSupport(value.id, value.height)
				case []support:
					for _, block := range value {
						hasher.WriteSupport(block.id, block.height)
					}
				case abstain:
					hasher.WriteAbstain()
				default:
					require.Failf(t, "unknown value", "%v", value)
				}
				rst := hasher.Hash()
				prev = &rst
			}
			require.Equal(t, tc.expected, prev.String())
		})
	}
}
