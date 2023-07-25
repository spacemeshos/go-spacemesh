package eligibility

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	heligibility "github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/log"
)

// TotalEligibilitiesSelector selects the total number of eligibilities based on
// the hare round. Currently the number of eligibilities for the propose round
// is set low in order to reduce the load on network bandwidth, all other
// rounds use the default committee size.
type TotalEligibilitiesSelector struct {
	committeeSize int
	numProposers  int
}

func NewTotalEligibilitiesSelector(committeeSize, numProposers int) *TotalEligibilitiesSelector {
	return &TotalEligibilitiesSelector{committeeSize: committeeSize, numProposers: numProposers}
}

func (s *TotalEligibilitiesSelector) totalEligibilitiesForRound(round hare3.AbsRound) int {
	if round.Round() == 2 {
		return s.numProposers
	}
	return s.committeeSize
}

// Calculator provides functionality to calculate the eligibilities at each
// round for a given node and layer.
type Calculator struct {
	id                   types.NodeID
	layer                types.LayerID
	eligibilitesSelector *TotalEligibilitiesSelector
	oracle               *heligibility.Oracle
	l                    log.Logger
}

func NewCalculator(
	id types.NodeID,
	layer types.LayerID,
	eligibilitesSelector *TotalEligibilitiesSelector,
	oracle *heligibility.Oracle,
	l log.Logger,
) *Calculator {
	return &Calculator{
		id:                   id,
		layer:                layer,
		eligibilitesSelector: eligibilitesSelector,
		oracle:               oracle,
		l:                    l,
	}
}

// func (ac *ActiveCheck) Active(ctx context.Context, round hare3.AbsRound) bool {
// 	proof, err := ac.oracle.Proof(ctx, ac.layer, uint32(round))
// 	if err != nil {
// 		ac.l.With().Error("failed to get eligibility proof from oracle", log.Err(err))
// 		return false
// 	}

// 	eligibilityCount, err := ac.oracle.CalcEligibility(ctx, ac.layer, uint32(round), ac.committeeSize, ac.id, proof)
// 	if err != nil {
// 		ac.oracle.With().Error("failed to check eligibility", log.Err(err))
// 		return false
// 	}
// 	return eligibilityCount > 0
// }

// Eligibilities returns the number of eligibilities for the given round.
func (ac *Calculator) Eligibilities(ctx context.Context, round hare3.AbsRound) (proof types.VrfSignature, count uint16, err error) {
	// First check that node is part of active set
	active, err := ac.oracle.IsIdentityActiveOnConsensusView(ctx, ac.id, ac.layer)
	if !active || err != nil {
		return types.VrfSignature{}, 0, err
	}
	// Now calculate eligibilities
	proof, err = ac.oracle.Proof(ctx, ac.layer, uint32(round))
	if err != nil {
		ac.l.With().Error("failed to get eligibility proof from oracle", log.Err(err))
		return types.VrfSignature{}, 0, err
	}

	count, err = ac.oracle.CalcEligibility(ctx, ac.layer, uint32(round), int(ac.eligibilitesSelector.totalEligibilitiesForRound(round)), ac.id, proof)
	if err != nil {
		ac.oracle.With().Error("failed to check eligibility", log.Err(err))
		return types.VrfSignature{}, 0, err
	}
	return proof, count, nil
}

// Validator provides functionality to validate the reported eligibilities of a
// given node in a given layer at each round. The reason for not simply
// checking the reported eligibilities against the output of the Calculator is
// that calculating eligibilities can be an expensive iterative process.
type Validator struct {
	eligibilitesSelector *TotalEligibilitiesSelector
	oracle               *heligibility.Oracle
	l                    log.Logger
}

func NewValidator(
	eligibilitesSelector *TotalEligibilitiesSelector,
	oracle *heligibility.Oracle,
	l log.Logger,
) *Validator {
	return &Validator{
		eligibilitesSelector: eligibilitesSelector,
		oracle:               oracle,
		l:                    l,
	}
}

// Validate returns true if the reported eligibilities are correct.
func (v *Validator) Validate(ctx context.Context, proof types.VrfSignature, id types.NodeID, layer types.LayerID, round hare3.AbsRound, nodeEligibilities uint16) (bool, error) {
	// First check that node is part of active set
	active, err := v.oracle.IsIdentityActiveOnConsensusView(ctx, id, layer)
	if !active || err != nil {
		return false, err
	}
	// Now validate the eligibilities
	return v.oracle.Validate(ctx, layer, uint32(round), int(v.eligibilitesSelector.totalEligibilitiesForRound(round)), id, proof, nodeEligibilities)
}
