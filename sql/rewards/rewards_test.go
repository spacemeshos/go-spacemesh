package rewards

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestAdd(t *testing.T) {
	db := sql.InMemory()
	var part uint64 = math.MaxUint64 / 4

	rewards := []Reward{
		{
			Address: types.Address{1, 1},
			Layer:   types.NewLayerID(1),
			Value:   part,
		},
		{
			Address: types.Address{1, 1},
			Layer:   types.NewLayerID(1),
			Value:   part,
		},
		{
			Address: types.Address{1, 1},
			Layer:   types.NewLayerID(1),
			Value:   part,
		},
		{
			Address: types.Address{1, 1},
			Layer:   types.NewLayerID(1),
			Value:   part,
		},
	}
	for _, reward := range rewards {
		require.NoError(t, Add(db, &reward))
	}
	total, err := CoinbaseTotal(db, types.Address{1, 1})
	require.NoError(t, err)
	require.Equal(t, part*4, total)
}
