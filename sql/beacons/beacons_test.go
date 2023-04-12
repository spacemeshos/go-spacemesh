package beacons

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const baseEpoch = 3

func TestGet(t *testing.T) {
	db := sql.InMemory()

	beacons := []types.Beacon{
		types.HexToBeacon("0x1"),
		types.HexToBeacon("0x2"),
		types.HexToBeacon("0x3"),
	}

	for i, beacon := range beacons {
		require.NoError(t, Add(db, types.EpochID(baseEpoch+i), beacon))
	}

	for i := range beacons {
		beacon, err := Get(db, types.EpochID(baseEpoch+i))
		require.NoError(t, err)
		require.Equal(t, beacons[i], beacon)
	}

	_, err := Get(db, types.EpochID(0))
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()

	_, err := Get(db, types.EpochID(baseEpoch))
	require.ErrorIs(t, err, sql.ErrNotFound)

	beacon := types.HexToBeacon("0x1")
	require.NoError(t, Add(db, baseEpoch, beacon))
	require.ErrorIs(t, Add(db, baseEpoch, beacon), sql.ErrObjectExists)

	got, err := Get(db, baseEpoch)
	require.NoError(t, err)
	require.Equal(t, beacon, got)
}

func TestAddOverwrite(t *testing.T) {
	db := sql.InMemory()

	_, err := Get(db, types.EpochID(baseEpoch))
	require.ErrorIs(t, err, sql.ErrNotFound)

	beacon := types.HexToBeacon("0x1")
	require.NoError(t, Add(db, baseEpoch, beacon))
	require.ErrorIs(t, Add(db, baseEpoch, beacon), sql.ErrObjectExists)

	got, err := Get(db, baseEpoch)
	require.NoError(t, err)
	require.Equal(t, beacon, got)

	fallbackBeacon := types.HexToBeacon("0x2")
	require.NoError(t, AddOverwrite(db, baseEpoch, fallbackBeacon))
	got, err = Get(db, baseEpoch)
	require.NoError(t, err)
	require.Equal(t, fallbackBeacon, got)
}
