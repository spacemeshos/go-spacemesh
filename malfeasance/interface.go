package malfeasance

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=malfeasance -destination=./mocks.go -source=./interface.go

type consensusProtocol interface {
	HandleEligibility(context.Context, *types.HareEligibilityGossip)
}
