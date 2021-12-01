package system

import "context"

//go:generate mockgen -package=mocks -destination=./mocks/sync.go -source=./sync.go

// SyncStateProvider defines the interface that provides the node's sync state.
type SyncStateProvider interface {
	IsSynced(context.Context) bool
}
