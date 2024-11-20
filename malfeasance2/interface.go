package malfeasance2

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=malfeasance2 -destination=./mocks.go -source=./interface.go

type tortoise interface {
	OnMalfeasance(types.NodeID)
}

type MalfeasanceHandler interface {
	Validate(ctx context.Context, data []byte) (types.NodeID, error)
	Info(data []byte) (map[string]string, error)
	ReportProof(vec *prometheus.CounterVec)        // TODO(mafa): don't pass vectors along, use one defined in package
	ReportInvalidProof(vec *prometheus.CounterVec) // TODO(mafa): don't pass vectors along, use one defined in package
}
