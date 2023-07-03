package events

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type UserEvent struct {
	Timestamp time.Time `json:"time"`
	Help      string    `jsonn:"help"`
	Details   any       `json:"details"`
}

type EventBeacon struct {
	Epoch  types.EpochID `json:"epoch"`
	Beacon types.Beacon  `json:"beacon"`
}

func EmitBeacon(epoch types.EpochID, beacon types.Beacon) {
	emitUsersEvents(
		"Node computed randomness beacon, it will be used to determine eligibility to participate in the consensus.",
		EventBeacon{Epoch: epoch, Beacon: beacon},
	)
}

type EventInitStart struct {
	Smesher    types.NodeID `json:"smesher"`
	Genesis    types.Hash20 `json:"genesis"`
	Commitment types.ATXID  `json:"commitment"`
}

func EmitInitStart(smesher types.NodeID, genesis types.Hash20, commitment types.ATXID) {
	emitUsersEvents(
		"Node started post data initialization.",
		EventInitStart{
			Smesher:    smesher,
			Genesis:    genesis,
			Commitment: commitment,
		},
	)
}

type EventInitComplete struct{}

func EmitInitComplete(duration time.Duration) {
	emitUsersEvents(
		"Node completed post data initialization.",
		EventInitComplete{},
	)
}

type EventPoetWait struct {
	Wait time.Duration `json:"wait"`
}

func EmitPoetWait(wait time.Duration) {
	emitUsersEvents(
		"Node needs to wait for poet registration window to open.",
		EventPoetWait{Wait: wait},
	)
}

type EventPost struct{}

func EmitPost() {
	emitUsersEvents(
		"Node started post execution.",
		EventPost{},
	)
}

type EventAtxPublished struct {
	Current types.EpochID `json:"current"`
	Target  types.EpochID `json:"target"`
	Layer   types.LayerID `json:"layer"`
	ID      types.ATXID   `json:"id"`
	Weight  uint64        `json:"weight"`
	Height  uint64        `json:"height"`
}

func EmitAtxPublished(
	layer types.LayerID,
	current, target types.EpochID,
	id types.ATXID,
	weight, height uint64,
) {
	emitUsersEvents(
		"Published activation. Node needs to wait till the start of target epoch in order to be eligible for rewards.",
		&EventAtxPublished{
			Current: current,
			Target:  target,
			Layer:   layer,
			ID:      id,
			Weight:  weight,
			Height:  height,
		},
	)
}

type EventEligibilities struct {
	Epoch         types.EpochID               `json:"epoch"`
	Beacon        types.Beacon                `json:"beacon"`
	ATX           types.ATXID                 `json:"atx"`
	ActiveSetHash types.Hash32                `json:"active-set-hash"`
	Eligibilities []types.ProposalEligibility `json:"eligibilities"`
}

func EmitEligibilities(
	epoch types.EpochID,
	beacon types.Beacon,
	atx types.ATXID,
	activeset types.Hash32,
	eligibilities []types.ProposalEligibility,
) {
	emitUsersEvents(
		"Computed eligibilities for the epoch. Rewards will be received based on them.",
		EventEligibilities{
			Epoch:         epoch,
			Beacon:        beacon,
			ATX:           atx,
			ActiveSetHash: activeset,
			Eligibilities: eligibilities,
		},
	)
}

type EventProposalCreated struct {
	Layer    types.LayerID    `json:"layer"`
	Proposal types.ProposalID `json:"proposal"`
	ATX      types.ATXID      `json:"atxid"`
}

func EmitProposal(layer types.LayerID, proposal types.ProposalID, atx types.ATXID) {
	emitUsersEvents(
		"Proposal was created. Once proposal is included into the block rewards will be received into coinbase.",
		EventProposalCreated{
			Layer:    layer,
			Proposal: proposal,
			ATX:      atx,
		},
	)
}

func emitUsersEvents(help string, details any) {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		if err := reporter.eventsEmitter.Emit(&UserEvent{
			Timestamp: time.Now(),
			Help:      help,
			Details:   details,
		}); err != nil {
			log.With().Error("failed to emit event", log.Err(err))
		}
	}
}
