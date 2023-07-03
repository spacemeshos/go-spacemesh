package events

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type EventType string

const (
	TypeBeacon        = "Beacon"
	TypeWaitAtxs      = "Download ATXs"
	TypeInitStart     = "Init Start"
	TypeInitComplete  = "Init Complete"
	TypePost          = "Post Start"
	TypePostComplete  = "Post Complete"
	TypePoetWait      = "Poet Wait"
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

type EventWaitSyncATXs struct{}

func EmitWaitSyncATXs() {
	const help = "Node downloading latest atxs, in order to select optimal ATX for protocol."
	emitUsersEvents(
		TypeWaitAtxs,
		help,
		false,
		EventWaitSyncATXs{},
	)
}

type EventInitStart struct {
	Smesher    types.NodeID `json:"smesher"`
	Genesis    types.Hash20 `json:"genesis"`
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
	Wait time.Duration `json:"wait"`
}

func EmitPoetWait(wait time.Duration) {
	const help = "Node needs to wait for poet registration window to open."
	emitUsersEvents(
		TypePoetWait,
		help,
		false,
		EventPoetWait{Wait: wait},
	)
}

func EmitPoetWaitEnd(wait time.Duration) {
	const help = "Node needs to wait for poet to complete."
	emitUsersEvents(
		TypePoetWaitEnd,
		help,
		false,
		EventPoetWait{Wait: wait},
	)
}

type EventPost struct{}

func EmitPost() {
	const help = "Node started post execution."
	emitUsersEvents(
		TypePost,
		help,
		false,
		EventPost{},
	)
}

type EventPostComplete struct {
	Challenge []byte
}

func EmitPostComplete(challenge []byte) {
	const help = "Node finished post execution."
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
	Layer   types.LayerID `json:"layer"`
	ID      types.ATXID   `json:"id"`
	Weight  uint64        `json:"weight"`
	Height  uint64        `json:"height"`
	Wait    time.Duration `json:"wait"`
}

func EmitAtxPublished(
	layer types.LayerID,
	current, target types.EpochID,
	id types.ATXID,
	weight, height uint64,
	wait time.Duration,
) {
	const help = "Published activation. " +
		"Node needs to wait till the start of target epoch in order to be eligible for rewards."
	emitUsersEvents(
		TypeATXPublished,
		help,
		false,
		&EventAtxPublished{
			Current: current,
			Target:  target,
			Layer:   layer,
			ID:      id,
			Weight:  weight,
			Height:  height,
			Wait:    wait,
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
	const help = "Computed eligibilities for the epoch. " +
		"Rewards will be received after publishing proposals at specified layers. " +
		"Total amount of rewards will be based on other participants in the layer."
	emitUsersEvents(
		TypeEligibilities,
		help,
		false,
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
	ATX      types.ATXID      `json:"atx"`
}

func EmitProposal(layer types.LayerID, proposal types.ProposalID, atx types.ATXID) {
	const help = "Published proposal. Rewards will be received, once proposal is included into the block."
	emitUsersEvents(
		TypeProposal,
		help,
		false,
		EventProposalCreated{
			Layer:    layer,
			Proposal: proposal,
			ATX:      atx,
		},
	)
}

func emitUsersEvents(typ EventType, help string, failure bool, details any) {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		if err := reporter.eventsEmitter.Emit(&UserEvent{
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
