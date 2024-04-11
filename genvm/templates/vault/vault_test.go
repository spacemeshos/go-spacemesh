package vault

import (
	"math"
	"testing"

	"github.com/spacemeshos/economics/constants"
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
			desc:    "zero at the start",
			start:   2,
			end:     10,
			lid:     2,
			initial: 10,
			total:   100,
			expect:  0,
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
			expect:  100 / 8,
		},
		{
			desc:    "increment part layers",
			start:   2,
			end:     10,
			lid:     5,
			initial: 10,
			total:   100,
			expect:  100 * 3 / 8,
		},
		{
			desc:    "increment almost all",
			start:   2,
			end:     10,
			lid:     9,
			initial: 10,
			total:   100,
			expect:  100 * 7 / 8,
		},
		{
			desc:    "one layer before actual end",
			start:   constants.VestStart,
			end:     constants.VestEnd,
			lid:     constants.VestEnd - 1,
			initial: 0.25 * constants.TotalVaulted,
			total:   constants.TotalVaulted,
			expect:  149999524353120243,
		},
		{
			desc:   "max values don't overflow",
			start:  constants.VestStart,
			end:    constants.VestEnd,
			lid:    constants.VestEnd - 1,
			total:  constants.TotalVaulted,
			expect: 149999524353120243,
		},
		{
			desc:   "after vest end",
			start:  constants.VestStart,
			end:    constants.VestEnd,
			lid:    2 * constants.VestEnd,
			total:  constants.TotalVaulted,
			expect: constants.TotalVaulted,
		},
		{
			desc:    "initial max uint64",
			start:   2,
			end:     10,
			lid:     2,
			initial: math.MaxUint64,
			total:   math.MaxUint64,
			expect:  0,
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
