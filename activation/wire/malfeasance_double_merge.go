package wire

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:generate scalegen

type ProofDoubleMerge struct {
	// NodeID is the node ID that referenced the same previous ATX twice.
	NodeID types.NodeID
}

var _ Proof = &ProofDoubleMerge{}

// Valid implements Proof.Valid.
func (p *ProofDoubleMerge) Valid(_ *signing.EdVerifier) (types.NodeID, error) {
	return p.NodeID, nil
}

func NewDoubleMergeProof(db sql.Executor, atx1, atx2 *ActivationTxV2, nodeID types.NodeID) (*ProofDoubleMerge, error) {
	return &ProofDoubleMerge{
		NodeID: nodeID,
	}, nil
}
