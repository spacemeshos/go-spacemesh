package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/sync.go -source=./sync.go

// SyncStateProvider defines the interface that provides the node's sync state.
type SyncStateProvider interface {
	IsSynced(context.Context) bool
	IsBeaconSynced(types.EpochID) bool

	// IsSyncedAtEpoch returns IsSynced && IsBeaconSynced for the given epoch.
	IsSyncedAtEpoch(context.Context, types.EpochID) bool
}
