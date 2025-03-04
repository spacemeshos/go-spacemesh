package v2alpha1

import (
	"context"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

//go:generate mockgen -typed -package=v2alpha1 -destination=./mocks.go -source=./interface.go

type malfeasanceInfo interface {
	Info(ctx context.Context, nodeID types.NodeID) (map[string]string, error)
}

type subscription interface {
	Out() <-chan events.EventMalfeasance
	Full() <-chan struct{}
	Close()
}

type eventProvider interface {
	SubscribeMatched(request *spacemeshv2alpha1.MalfeasanceStreamRequest) (subscription, error)
}
