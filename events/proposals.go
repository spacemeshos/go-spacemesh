package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// ProposalStatus is a type for proposal status.
type ProposalStatus int

func (s ProposalStatus) String() string {
	switch s {
	case ProposalCreated:
		return "created"
	case ProposalIncluded:
		return "included"
	default:
		panic("unknown status")
	}
}

const (
	// ProposalCreated is a status of the proposal that was created.
	ProposalCreated ProposalStatus = iota
	// ProposalIncluded is a status of the proposal when it is included into the block.
	ProposalIncluded
)

// EventProposal includes proposal and proposal status.
type EventProposal struct {
	Status   ProposalStatus
	Proposal *types.Proposal
}

// ReportProposal reports a proposal.
func ReportProposal(status ProposalStatus, proposal *types.Proposal) {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		if err := reporter.proposalsEmitter.Emit(EventProposal{Status: status, Proposal: proposal}); err != nil {
			// TODO(dshulyak) check why it can error. i think it is reasonable to panic here
			log.With().Error("failed to emit proposal", log.Err(err))
		}
	}
}

// SubcribeProposals subscribes to the proposals.
func SubcribeProposals() Subscription {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(EventProposal))
		if err != nil {
			log.With().Panic("Failed to subscribe to proposals")
		}
		return sub
	}
	return nil
}
