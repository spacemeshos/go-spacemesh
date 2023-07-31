package miner

import (
	"errors"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// AtxGrade describes the grade of an ATX as described in
// https://community.spacemesh.io/t/grading-atxs-for-the-active-set/335
//
// let s be the start of the epoch, and δ the network propagation time.
// grade 0: ATX was received at time t >= s-3δ, or an equivocation proof was received by time s-δ.
// grade 1: ATX was received at time t < s-3δ before the start of the epoch, and no equivocation proof was received by time s-δ.
// grade 2: ATX was received at time t < s-4δ, and no equivocation proof was received for that id until time s.
type AtxGrade int

const (
	Evil AtxGrade = iota
	Acceptable
	Good
)

func GradeAtx(msh mesh, nodeID types.NodeID, atxReceived, epochStart time.Time, delta time.Duration) (AtxGrade, error) {
	proof, err := msh.GetMalfeasanceProof(nodeID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return Good, err
	}
	if atxReceived.Before(epochStart.Add(-4*delta)) && (proof == nil || !proof.Received().Before(epochStart)) {
		return Good, nil
	}
	if atxReceived.Before(epochStart.Add(-3*delta)) && (proof == nil || !proof.Received().Before(epochStart.Add(-delta))) {
		return Acceptable, nil
	}
	return Evil, nil
}
