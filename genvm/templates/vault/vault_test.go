package vault

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

func TestAvailable(t *testing.T) {
	for _, tc := range []struct {
		desc                   string
		start, end, lid        uint32
		total, initial, expect uint64
	}{
		{
			desc:    "zero before start",
			start:   2,
			lid:     1,
			initial: 10,
			total:   100,
		},
		{
			desc:    "initial at the start",
			start:   2,
			end:     10,
			lid:     2,
			initial: 10,
			total:   100,
			expect:  10,
		},
		{
			desc:    "total at the end",
			start:   2,
			end:     10,
			lid:     10,
			initial: 10,
			total:   100,
			expect:  100,
		},
		{
			desc:    "total after the end",
			start:   2,
			end:     10,
			lid:     11,
			initial: 10,
			total:   100,
			expect:  100,
		},
		{
			desc:    "increment one layer",
			start:   2,
			end:     10,
			lid:     3,
			initial: 10,
			total:   100,
			expect:  10 + (100-10)/8,
		},
		{
			desc:    "increment part layers",
			start:   2,
			end:     10,
			lid:     5,
			initial: 10,
			total:   100,
			expect:  10 + (100-10)*3/8,
		},
		{
			desc:    "increment almost all",
			start:   2,
			end:     10,
			lid:     9,
			initial: 10,
			total:   100,
			expect:  10 + (100-10)*7/8,
		},
		{
			desc:   "use big int to avoid overflow",
			start:  2,
			end:    10,
			lid:    4,
			total:  math.MaxUint64,
			expect: math.MaxUint64 / 4, // math.MaxUint64 * 2 / 8
		},
		{
			desc:    "initial max uint64",
			start:   2,
			end:     10,
			lid:     2,
			initial: math.MaxUint64,
			total:   math.MaxUint64,
			expect:  math.MaxUint64,
		},
		{
			desc:    "total max uint64",
			start:   2,
			end:     10,
			lid:     1000,
			initial: math.MaxUint64,
			total:   math.MaxUint64,
			expect:  math.MaxUint64,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			v := Vault{
				TotalAmount:         tc.total,
				InitialUnlockAmount: tc.initial,
				VestingStart:        types.LayerID(tc.start),
				VestingEnd:          types.LayerID(tc.end),
			}
			available := v.Available(types.LayerID(tc.lid))
			require.Equal(t, int(tc.expect), int(available))
		})
	}
}

func TestOwnership(t *testing.T) {
	owner := core.Address{'o'}
	ctx := core.Context{}
	vault := Vault{
		Owner: owner,
	}
	t.Run("auth failed", func(t *testing.T) {
		require.ErrorIs(t, ErrNotOwner, vault.Spend(&ctx, core.Address{1}, 100))
	})
	t.Run("auth passed", func(t *testing.T) {
		ctx.PrincipalAccount.Address = owner
		require.ErrorIs(t, ErrAmountNotAvailable, vault.Spend(&ctx, core.Address{1}, 100))
	})
}
