package recovery_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/recovery"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestRecoveryInfo(t *testing.T) {
	db := statesql.InMemory()
	restore := types.LayerID(12)

	got, err := recovery.CheckpointInfo(db)
	require.NoError(t, err)
	require.Zero(t, got)

	require.NoError(t, recovery.SetCheckpoint(db, restore))
	got, err = recovery.CheckpointInfo(db)
	require.NoError(t, err)
	require.Equal(t, restore, got)

	require.Error(t, recovery.SetCheckpoint(db, restore))
}
