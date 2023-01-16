package malfeasance

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=malfeasance -destination=./mocks.go -source=./interface.go

type eligibilityValidator interface {
	Validate(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, proof []byte, eligibilityCount uint16) (bool, error)
}
