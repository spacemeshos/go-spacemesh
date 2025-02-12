package v2alpha1

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=v2alpha1 -destination=./mocks.go -source=./interface.go

type malfeasanceInfo interface {
	Info(ctx context.Context, nodeID types.NodeID) (map[string]string, error)
}

// syncer is an API to get sync status.
type syncer interface {
	IsSynced(context.Context) bool
}
