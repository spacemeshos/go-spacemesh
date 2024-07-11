package malfeasance

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
)

//go:generate mockgen -typed -package=malfeasance -destination=./mocks.go -source=./interface.go

type tortoise interface {
	OnMalfeasance(types.NodeID)
}

type HandlerV1 interface {
	Validate(ctx context.Context, data wire.ProofData) (types.NodeID, error)
	ReportProof(vec *prometheus.CounterVec)
	ReportInvalidProof(vec *prometheus.CounterVec)
}
