package leader

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3"
)

type DefaultLeaderChecker struct {
	oracle          *eligibility.Oracle
	expectedLeaders int
	acviteSet       map[types.NodeID]struct{}
	layer           types.LayerID
}

func NewDefaultLeaderChecker(oracle *eligibility.Oracle,
	expectedLeaders int,
	acviteSet map[types.NodeID]struct{},
	layer types.LayerID,
) *DefaultLeaderChecker {
	return &DefaultLeaderChecker{
		oracle:          oracle,
		expectedLeaders: expectedLeaders,
		acviteSet:       acviteSet,
		layer:           layer,
	}
}

func (c *DefaultLeaderChecker) IsLeader(id types.NodeID, round hare3.AbsRound) bool {
	if _, ok := c.acviteSet[id]; !ok {
		return false
	}
	proof, err := c.oracle.Proof(context.Background(), c.layer, uint32(round))
	if err != nil {
		// TODO figure out, error can be that we don't have the beacon
		return false
	}

	size := c.expectedLeaders

	eligibilityCount, err := c.oracle.CalcEligibility(context.Background(), c.layer, uint32(round), size, id, proof)
	if err != nil {
		return false
	}
	return eligibilityCount > 0
}
