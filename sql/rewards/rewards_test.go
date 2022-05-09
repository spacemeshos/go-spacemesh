package rewards

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestFilter(t *testing.T) {
	db := sql.InMemory()

	var part uint64 = math.MaxUint64 / 2
	node1 := types.NodeID{1}
	node2 := types.NodeID{2}
	coinbase := types.Address{1}
	lid := types.NewLayerID(1)

	rewards := []types.AnyReward{
		{
			SmesherID: node1,
			Address:   coinbase,
			Amount:    part,
		},
		{
			SmesherID: node2,
			Address:   coinbase,
			Amount:    part,
		},
		{
			SmesherID: node2,
			Address:   coinbase,
			Amount:    part,
		},
		{
			SmesherID: node1,
			Address:   coinbase,
			Amount:    part,
		},
	}
	for _, reward := range rewards {
		require.NoError(t, Add(db, lid, &reward))
		require.NoError(t, Add(db, lid.Add(1), &reward))
	}

	for _, node := range []types.NodeID{node1, node2} {
		rst, err := FilterBySmesher(db, node.ToBytes())
		require.NoError(t, err)
		require.Len(t, rst, 2)
		require.Equal(t, part*2, rst[0].TotalReward)
	}

	rst, err := FilterByCoinbase(db, coinbase)
	require.NoError(t, err)
	require.Len(t, rst, 4)
	for _, reward := range rst {
		require.Equal(t, part*2, reward.TotalReward)
	}
}
