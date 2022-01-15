package rewards

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestAdd(t *testing.T) {
	db := sql.InMemory()

	rewards := []types.AnyReward{
		{
			Address: types.Address{1, 1},
		},
		{
			Address: types.Address{1, 1},
		},
		{
			Address: types.Address{1, 1},
		},
		{
			Address: types.Address{1, 1},
		},
	}
	for _, reward := range rewards {
		require.NoError(t, Add(db, types.NewLayerID(1), &reward))
	}
}
