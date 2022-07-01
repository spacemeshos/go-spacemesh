package epoch_test

import (
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/epoch"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestEpochService_GetEpochStatus(t *testing.T) {
	db := sql.InMemory()

	epochService := epoch.NewEpochService(db)
	state := map[types.EpochID]types.EpochStatus{
		1: types.EpochStatusSmeshing,
		2: types.EpochStatusGeneratingPoS,
		3: types.EpochStatusGeneratingPoS,
	}

	setState(t, state, epochService)
	epochStatuses, err := epochService.GetEpochStatus([]types.EpochID{1, 2, 3})
	require.NoError(t, err)
	checkState(t, state, epochStatuses)

	state[3] = types.EpochStatusWaitingNextEpoch
	setState(t, state, epochService)

	epochStatuses, err = epochService.GetEpochStatus([]types.EpochID{1, 2, 3})
	require.NoError(t, err)
	checkState(t, state, epochStatuses)
}

func setState(t *testing.T, state map[types.EpochID]types.EpochStatus, service *epoch.EpochService) {
	for epochID, status := range state {
		require.NoError(t, service.UpdateEpochStatus(epochID, status))
	}
}

func checkState(t *testing.T, expect, result map[types.EpochID]types.EpochStatus) {
	for epochID, status := range expect {
		require.Equal(t, status, result[epochID], "epochID: %d. Expected `%d`, got `%d`", epochID, status, result[epochID])
	}
}
