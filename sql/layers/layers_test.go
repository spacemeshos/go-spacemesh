package layers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestEmpty(t *testing.T) {
	db := sql.InMemory()
	lid := types.NewLayerID(100)
	require.NoError(t, SetEmpty(db, lid))

	empty, err := IsEmpty(db, lid)
	require.NoError(t, err)
	require.True(t, empty)

	empty, err = IsEmpty(db, lid.Add(1))
	require.NoError(t, err)
	require.False(t, empty)
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
