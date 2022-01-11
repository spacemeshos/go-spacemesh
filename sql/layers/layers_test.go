package layers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestHareOutput(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)

	_, err := GetHareOutput(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetStatus(db, lid, Latest))

	_, err = GetHareOutput(db, lid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, SetHareOutput(db, lid, types.BlockID{}))
	output, err := GetHareOutput(db, lid)
	require.NoError(t, err)
	require.Equal(t, types.BlockID{}, output)

	expected := types.BlockID{1, 1, 1}
	require.NoError(t, SetHareOutput(db, lid, expected))
	output, err = GetHareOutput(db, lid)
	require.NoError(t, err)
	require.Equal(t, expected, output)
}

func TestStatus(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(10)

	require.NoError(t, SetStatus(db, lid, Applied))
	require.NoError(t, SetStatus(db, lid.Add(1), Processed))
	require.NoError(t, SetStatus(db, lid.Add(2), Processed))
	require.NoError(t, SetStatus(db, lid.Add(3), Latest))

	processed, err := GetByStatus(db, Processed)
	require.NoError(t, err)
	require.Equal(t, lid.Add(2), processed)

	latest, err := GetByStatus(db, Latest)
	require.NoError(t, err)
	require.Equal(t, lid.Add(3), latest)

	applied, err := GetByStatus(db, Applied)
	require.NoError(t, err)
	require.Equal(t, lid, applied)
}
