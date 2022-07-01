package epoch

import "github.com/spacemeshos/go-spacemesh/common/types"

type Epocher interface {
	UpdateEpochStatus(epoch types.EpochID, status types.EpochStatus) error
	GetEpochStatus(epoch []types.EpochID) (map[types.EpochID]types.EpochStatus, error)
}
