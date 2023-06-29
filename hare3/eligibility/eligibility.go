package eligibility

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	heligibility "github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/log"
)

type ActiveCheck struct {
	layer         types.LayerID
	id            types.NodeID
	committeeSize int
	oracle        *heligibility.Oracle
	l             log.Logger
}

func NewActiveCheck(layer types.LayerID, id types.NodeID, committeeSize int, oracle *heligibility.Oracle, l log.Logger) *ActiveCheck {
	return &ActiveCheck{
		layer:         layer,
		id:            id,
		committeeSize: committeeSize,
		oracle:        oracle,
		l:             l,
	}
}

func (ac *ActiveCheck) Active(ctx context.Context, round hare3.AbsRound) bool {
	proof, err := ac.oracle.Proof(ctx, ac.layer, uint32(round))
	if err != nil {
		ac.l.With().Error("failed to get eligibility proof from oracle", log.Err(err))
		return false
	}

	eligibilityCount, err := ac.oracle.CalcEligibility(ctx, ac.layer, uint32(round), ac.committeeSize, ac.id, proof)
	if err != nil {
		ac.oracle.With().Error("failed to check eligibility", log.Err(err))
		return false
	}
	return eligibilityCount > 0
}
