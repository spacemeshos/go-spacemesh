package events

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type EventType string

const (
	TypeBeacon        = "Beacon"
	TypeInitStart     = "Init Start"
	TypeInitComplete  = "Init Complete"
	TypePostStart     = "Post Start"
	TypePostComplete  = "Post Complete"
	TypePoetWaitStart = "Poet Wait Start"
	TypePoetWaitEnd   = "Poet Wait End"
	TypeATXPublished  = "ATX Published"
	TypeEligibilities = "Eligibilities"
	TypeProposal      = "Proposal"
)

type UserEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Help      string    `json:"help"`
	Failure   bool      `json:"failure"`
	Type      EventType `json:"type"`
	Details   any       `json:"details"`
}

type EventBeacon struct {
	Epoch  types.EpochID `json:"epoch"`
	Beacon types.Beacon  `json:"beacon"`
}

func EmitBeacon(epoch types.EpochID, beacon types.Beacon) {
	const help = "Node computed randomness beacon, it will be used to determine eligibility to participate in the consensus."
	emitUsersEvents(
		TypeBeacon,
		help,
		false,
		EventBeacon{Epoch: epoch, Beacon: beacon},
	)
}

type EventInitStart struct {
	Smesher    types.NodeID `json:"smesher"`
	Commitment types.ATXID  `json:"commitment"`
}

func EmitInitStart(smesher types.NodeID, commitment types.ATXID) {
	const help = "Node started post data initialization. Note that init is noop if node restarted when init was ready."
	emitUsersEvents(
		TypeInitStart,
		help,
		false,
		EventInitStart{
			Smesher:    smesher,
			Commitment: commitment,
		},
	)
}

func EmitInitComplete(failure bool) {
	const help = "Node completed post data initialization. On failure examine logs."
	emitUsersEvents(
		TypeInitComplete,
		help,
		failure,
		struct{}{},
	)
}

type EventPoetWait struct {
	Current types.EpochID `json:"current"`
	Publish types.EpochID `json:"publish"`
	Wait    time.Duration `json:"wait"`
}

func EmitPoetWait(current, publish types.EpochID, wait time.Duration) {
	const help = "Node needs to wait for poet registration window in current epoch to open." +
		"Once opened it will submit challenge and wait till poet round ends in publish epoch."
	emitUsersEvents(
		TypePoetWaitStart,
		help,
		false,
		EventPoetWait{Current: current, Publish: publish, Wait: wait},
	)
}

type EventPoetWaitEnd struct {
	Publish types.EpochID `json:"publish"`
	Target  types.EpochID `json:"target"`
	Wait    time.Duration `json:"wait"`
}

func EmitPoetWaitEnd(publish, target types.EpochID, wait time.Duration) {
	const help = "Node needs to wait for poet to complete in publish epoch." +
		"Once completed it will compute post and publish an ATX that will be eligible for " +
		"rewards in target epoch."
	emitUsersEvents(
		TypePoetWaitEnd,
		help,
		false,
		EventPoetWaitEnd{Publish: publish, Target: target, Wait: wait},
	)
}

type EventPost struct {
	Challenge []byte `json:"challenge"`
}

func EmitPostStart(challennge []byte) {
	const help = "Node started post execution for the challenge from poet."
	emitUsersEvents(
		TypePostStart,
		help,
		false,
		EventPost{Challenge: challennge},
	)
}

type EventPostComplete struct {
	Challenge []byte `json:"challenge"`
}

func EmitPostComplete(challenge []byte) {
	const help = "Node finished post execution for challenge."
	emitUsersEvents(
		TypePostComplete,
		help,
		false,
		EventPostComplete{Challenge: challenge},
	)
}

func EmitPostFailure() {
	const help = "Failed to compute post."
	emitUsersEvents(
		TypePostComplete,
		help,
		true,
		EventPostComplete{},
	)
}

type EventAtxPublished struct {
	Current types.EpochID `json:"current"`
	Target  types.EpochID `json:"target"`
	ID      types.ATXID   `json:"id"`
	Wait    time.Duration `json:"wait"`
}

func EmitAtxPublished(
	current, target types.EpochID,
	id types.ATXID,
	wait time.Duration,
) {
	const help = "Published activation for the current epoch. " +
		"Node needs to wait till the start of the target epoch in order to be eligible for rewards."
	emitUsersEvents(
		TypeATXPublished,
		help,
		false,
		&EventAtxPublished{
			Current: current,
			Target:  target,
			ID:      id,
			Wait:    wait,
		},
	)
}

type EventEligibilities struct {
	Epoch         types.EpochID               `json:"epoch"`
	Beacon        types.Beacon                `json:"beacon"`
	ATX           types.ATXID                 `json:"atx"`
	ActiveSetSize uint32                      `json:"active-set-size"`
	Eligibilities []types.ProposalEligibility `json:"eligibilities"`
}

func EmitEligibilities(
	epoch types.EpochID,
	beacon types.Beacon,
	atx types.ATXID,
	activeSetSize uint32,
	eligibilities []types.ProposalEligibility,
) {
	const help = "Computed eligibilities for the epoch. " +
		"Rewards will be received after publishing proposals at specified layers. " +
		"Total amount of rewards in SMH will be based on other participants in the layer."
	emitUsersEvents(
		TypeEligibilities,
		help,
		false,
		EventEligibilities{
			Epoch:         epoch,
			Beacon:        beacon,
			ATX:           atx,
			ActiveSetSize: activeSetSize,
			Eligibilities: eligibilities,
		},
	)
}

type EventProposalCreated struct {
	Layer    types.LayerID    `json:"layer"`
	Proposal types.ProposalID `json:"proposal"`
}

func EmitProposal(layer types.LayerID, proposal types.ProposalID) {
	const help = "Published proposal. Rewards will be received, once proposal is included into the block."
	emitUsersEvents(
		TypeProposal,
		help,
		false,
		EventProposalCreated{
			Layer:    layer,
			Proposal: proposal,
		},
	)
}

func emitUsersEvents(typ EventType, help string, failure bool, details any) {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		if err := reporter.eventsEmitter.Emit(UserEvent{
			Timestamp: time.Now(),
			Type:      typ,
			Help:      help,
			Failure:   failure,
			Details:   details,
		}); err != nil {
			log.With().Error("failed to emit event", log.Err(err))
		}
	}
}
