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
			expected: "0x32361d7c383f07915925828662cd482d9ded9773506e1de614be66b25b6aeb53",
		},
		{
			desc: "support height 100",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
			},
			expected: "0x0117c9973e8684cf1b9fc6a3d883fe0adce03027b0fef68b6b5b46af265b8775",
		},
		{
			desc: "multiple blocks",
			seq: []any{
				[]support{{id: types.BlockID{1}, height: 100}, {id: types.BlockID{2}, height: 100}},
			},
			expected: "0x5f7d9ac8209de9cd0558ff2bedeb522795603c4a66a3b0859a4256e4ad38426c",
		},
		{
			desc: "single against",
			seq: []any{
				nil,
			},
			expected: "0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			desc: "abstain",
			seq: []any{
				abstain{},
			},
			expected: "0x6e340b9cffb37a989ca544e6bb780a2c78901d3fb33738768511a30617afa01d",
		},
		{
			desc: "abstain abstain",
			seq: []any{
				abstain{},
				abstain{},
			},
			expected: "0x345e431cadda34d3862fb4a060f18ce7f4d70738b3d5560235a97a584a6bb15f",
		},
		{
			desc: "against against",
			seq: []any{
				nil,
				nil,
			},
			expected: "0x5df6e0e2761359d30a8275058e299fcc0381534545f55cf43e41983f5d4c9456",
		},
		{
			desc: "support support",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
				support{id: types.BlockID{2}, height: 100},
			},
			expected: "0xdf06f149e6a3c33d28df7e91c4e96b95e8a60021caf782c96682917fff5d95c9",
		},
		{
			desc: "support against",
			seq: []any{
				support{id: types.BlockID{1}, height: 100},
				nil,
			},
			expected: "0xe59dafb9506937f3e0dc3fcdfd25731a14aefebad44d93f282eee3a2d45b6d55",
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
