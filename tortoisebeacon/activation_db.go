package tortoisebeacon

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockery -name activationDB -case underscore -inpkg
type activationDB interface {
	GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error)
	GetNodeAtxIDForEpoch(nodePK string, targetEpoch types.EpochID) (types.ATXID, error)
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
	GetAtxTimestamp(id types.ATXID) (time.Time, error)
}
