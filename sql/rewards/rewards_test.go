package rewards

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestRewards(t *testing.T) {
	db := sql.InMemory()

	var part uint64 = math.MaxUint64 / 2
	lyrReward := part / 2
	coinbase1 := types.Address{1}
	coinbase2 := types.Address{2}
	smesherID1 := types.NodeID{1}
	smesherID2 := types.NodeID{2}
	smesherID3 := types.NodeID{3}

	lid1 := types.LayerID(1)
	rewards1 := []types.Reward{
		{
			Layer:       lid1,
			Coinbase:    coinbase1,
			SmesherID:   smesherID1,
			TotalReward: part,
			LayerReward: lyrReward,
		},
		{
			Layer:       lid1,
			Coinbase:    coinbase1,
			SmesherID:   smesherID2,
			TotalReward: part,
			LayerReward: lyrReward,
		},
		{
			Layer:       lid1,
			Coinbase:    coinbase2,
			SmesherID:   smesherID3,
			TotalReward: part,
			LayerReward: lyrReward,
		},
	}
	lid2 := lid1.Add(1)
	rewards2 := []types.Reward{
		{
			Layer:       lid2,
			Coinbase:    coinbase2,
			SmesherID:   smesherID2,
			TotalReward: part,
			LayerReward: lyrReward,
		},
	}
	for _, reward := range append(rewards1, rewards2...) {
		require.NoError(t, Add(db, &reward))
	}

	got, err := ListByCoinbase(db, coinbase1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID1, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)
	require.Equal(t, coinbase1, got[1].Coinbase)
	require.Equal(t, lid1, got[1].Layer)
	require.Equal(t, smesherID2, got[1].SmesherID)
	require.Equal(t, part, got[1].TotalReward)
	require.Equal(t, lyrReward, got[1].LayerReward)

	got, err = ListByCoinbase(db, coinbase2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, coinbase2, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID3, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)
	require.Equal(t, coinbase2, got[1].Coinbase)
	require.Equal(t, lid2, got[1].Layer)
	require.Equal(t, smesherID2, got[1].SmesherID)
	require.Equal(t, part, got[1].TotalReward)
	require.Equal(t, lyrReward, got[1].LayerReward)

	got, err = ListBySmesherId(db, smesherID1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID1, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	got, err = ListBySmesherId(db, smesherID2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID2, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)
	require.Equal(t, coinbase2, got[1].Coinbase)
	require.Equal(t, lid2, got[1].Layer)
	require.Equal(t, smesherID2, got[1].SmesherID)
	require.Equal(t, part, got[1].TotalReward)
	require.Equal(t, lyrReward, got[1].LayerReward)

	got, err = ListBySmesherId(db, smesherID3)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase2, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID3, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	unknownAddr := types.Address{1, 2, 3}
	got, err = ListByCoinbase(db, unknownAddr)
	require.NoError(t, err)
	require.Len(t, got, 0)

	unknownSmesher := types.NodeID{1, 2, 3}
	got, err = ListBySmesherId(db, unknownSmesher)
	require.NoError(t, err)
	require.Len(t, got, 0)

	require.NoError(t, Revert(db, lid1))
	got, err = ListByCoinbase(db, coinbase1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID1, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)
	require.Equal(t, coinbase1, got[1].Coinbase)
	require.Equal(t, lid1, got[1].Layer)
	require.Equal(t, smesherID2, got[1].SmesherID)
	require.Equal(t, part, got[1].TotalReward)
	require.Equal(t, lyrReward, got[1].LayerReward)

	got, err = ListByCoinbase(db, coinbase2)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase2, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID3, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	got, err = ListBySmesherId(db, smesherID1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID1, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	got, err = ListBySmesherId(db, smesherID2)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase1, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID2, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	got, err = ListBySmesherId(db, smesherID3)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, coinbase2, got[0].Coinbase)
	require.Equal(t, lid1, got[0].Layer)
	require.Equal(t, smesherID3, got[0].SmesherID)
	require.Equal(t, part, got[0].TotalReward)
	require.Equal(t, lyrReward, got[0].LayerReward)

	// This should fail: there cannot be two (smesherID, layer) rows.
	require.ErrorIs(t, Add(db, &types.Reward{
		Layer:     lid1,
		SmesherID: smesherID2,
	}), sql.ErrObjectExists)

	// This should succeed. SmesherID can be NULL.
	require.NoError(t, Add(db, &types.Reward{
		Layer: lid1,
	}))

	// This should fail: there cannot be two (NULL, layer) rows.
	require.ErrorIs(t, Add(db, &types.Reward{
		Layer: lid1,
	}), sql.ErrObjectExists)
}
