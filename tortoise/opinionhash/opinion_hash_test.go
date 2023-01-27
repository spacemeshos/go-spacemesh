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
			expected: "0x9792d675f91bda454356c00761f0ee7e4fad33f075f5c02cfd11730f0199f3ed",
		},
		{
			desc: "support height 100",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
			},
			expected: "0xfdae510822088f67d47f8229d7971212973688d310c87144a154e8b51d787b4e",
		},
		{
			desc: "multiple blocks",
			seq: []any{
				[]support{{id: types.BlockID{1}, height: 100}, {id: types.BlockID{2}, height: 100}},
			},
			expected: "0xc0d65bb2f64a870c3f067bbc6e06ded4437ec95616b7bc61ee52fd02fe9468d2",
		},
		{
			desc: "single against",
			seq: []any{
				nil,
			},
			expected: "0xaf1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262",
		},
		{
			desc: "abstain",
			seq: []any{
				abstain{},
			},
			expected: "0x2d3adedff11b61f14c886e35afa036736dcd87a74d27b5c1510225d0f592e213",
		},
		{
			desc: "abstain abstain",
			seq: []any{
				abstain{},
				abstain{},
			},
			expected: "0x58716d2accccc68182ffb06de20c77010061d694056defb23d2ad881d9367e16",
		},
		{
			desc: "against against",
			seq: []any{
				nil,
				nil,
			},
			expected: "0x82878ed8a480ee41775636820e05a934ca5c747223ca64306658ee5982e6c227",
		},
		{
			desc: "support support",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
				support{id: types.BlockID{2}, height: 100},
			},
			expected: "0x88f75279f76898e958fba3409c60cee10967e04910bb97730e286531bbe7a757",
		},
		{
			desc: "support against",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
				nil,
			},
			expected: "0xae4b107c6601034d18eb7b7817a330abe6ba8a82e58265ef503928050730cd14",
		},
	} {
		tc := tc
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
