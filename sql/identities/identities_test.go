package identities

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestMalicious(t *testing.T) {
	db := sql.InMemory()

	pub := []byte{1, 1, 1, 1}
	mal, err := IsMalicious(db, pub)
	require.NoError(t, err)
	require.False(t, mal)

	require.NoError(t, SetMalicious(db, pub))
	mal, err = IsMalicious(db, pub)
	require.NoError(t, err)
	require.True(t, mal)
}
