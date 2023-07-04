package events

import (
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type UserEvent struct {
	Event *pb.Event
}

func EmitBeacon(epoch types.EpochID, beacon types.Beacon) {
	const help = "Node computed randomness beacon, it will be used to determine eligibility to participate in the consensus."
	emitUserEvent(
		help,
		false,
		&pb.Event_Beacon{
			Beacon: &pb.EventBeacon{
				Epoch:  epoch.Uint32(),
				Beacon: beacon[:],
			},
		},
	)
}

func EmitInitStart(smesher types.NodeID, commitment types.ATXID) {
	const help = "Node started post data initialization. Note that init is noop if node restarted when init was ready."
	emitUserEvent(
		help,
		false,
		&pb.Event_InitStart{
			InitStart: &pb.EventInitStart{
				Smesher:    smesher[:],
				Commitment: commitment[:],
			},
		},
	)
}

func EmitInitComplete(failure bool) {
	const help = "Node completed post data initialization."
	emitUserEvent(
		help,
		failure,
		&pb.Event_InitComplete{
			InitComplete: &pb.EventInitComplete{},
		},
	)
}

func EmitPoetWait(current, publish types.EpochID, wait time.Duration) {
	const help = "Node needs to wait for poet registration window in current epoch to open. " +
		"Once opened it will submit challenge and wait till poet round ends in publish epoch."
	emitUserEvent(
		help,
		false,
		&pb.Event_PoetWait{PoetWait: &pb.EventPoetWait{
			Current: current.Uint32(),
			Public:  current.Uint32(),
			Wait:    durationpb.New(wait),
		}},
	)
}

type EventPoetWaitEnd struct {
	Publish types.EpochID `json:"publish"`
	Target  types.EpochID `json:"target"`
	Wait    time.Duration `json:"wait"`
}

func EmitPoetWaitEnd(publish, target types.EpochID, wait time.Duration) {
	const help = "Node needs to wait for poet to complete in publish epoch." +
		"Once completed, node fetches challenge from poet and run post on that challenge. " +
		"After that publish an ATX that will be eligible for rewards in target epoch."
	emitUserEvent(
		help,
		false,
		&pb.Event_PoetWaitChallenge{
			PoetWaitChallenge: &pb.EventPoetWaitChallenge{
				Publish: publish.Uint32(),
				Target:  target.Uint32(),
				Wait:    durationpb.New(wait),
			},
		},
	)
}

func EmitPostStart(challenge []byte) {
	const help = "Node started post execution for the challenge from poet."
	emitUserEvent(
		help,
		false,
		&pb.Event_PostStart{PostStart: &pb.EventPostStart{Challenge: challenge}},
	)
}

func EmitPostComplete(challenge []byte) {
	const help = "Node finished post execution for challenge."
	emitUserEvent(
		help,
		false,
		&pb.Event_PostComplete{PostComplete: &pb.EventPostComplete{Challenge: challenge}},
	)
}

func EmitPostFailure() {
	const help = "Failed to compute post."
	emitUserEvent(
		help,
		true,
		&pb.Event_PostFailed{PostFailed: &pb.EventPostFailed{}},
	)
}

func EmitAtxPublished(
	current, target types.EpochID,
	id types.ATXID,
	wait time.Duration,
) {
	const help = "Published activation for the current epoch. " +
		"Node needs to wait till the start of the target epoch in order to be eligible for rewards."
	emitUserEvent(
		help,
		false,
		&pb.Event_AtxPublished{
			AtxPublished: &pb.EventAtxPubished{
				Current: current.Uint32(),
				Target:  target.Uint32(),
				Id:      id[:],
				Wait:    durationpb.New(wait),
			},
		},
	)
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
	emitUserEvent(
		help,
		false,
		&pb.Event_Eligibilities{
			Eligibilities: &pb.EventEligibilities{
				Epoch:         epoch.Uint32(),
				Beacon:        beacon[:],
				Atx:           atx[:],
				ActiveSetSize: activeSetSize,
				Eligibilities: castEligibilities(eligibilities),
			},
		},
	)
}

func castEligibilities(eligs []types.ProposalEligibility) []*pb.ProposalEligibility {
	rst := make([]*pb.ProposalEligibility, len(eligs))
	for i := range eligs {
		rst[i] = &pb.ProposalEligibility{
			Layer: eligs[i].Layer.Uint32(),
			Count: eligs[i].Count,
		}
	}
	return rst
}

func EmitProposal(layer types.LayerID, proposal types.ProposalID) {
	const help = "Published proposal. Rewards will be received, once proposal is included into the block."
	emitUserEvent(
		help,
		false,
		&pb.Event_Proposal{
			Proposal: &pb.EventProposal{
				Layer:    layer.Uint32(),
				Proposal: proposal[:],
			},
		},
	)
}

func emitUserEvent(help string, failure bool, details pb.IsEventDetails) {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		if err := reporter.eventsEmitter.Emit(UserEvent{Event: &pb.Event{
			Timestamp: timestamppb.New(time.Now()),
			Help:      help,
			Failure:   failure,
			Details:   details,
		}}); err != nil {
			log.With().Error("failed to emit event", log.Err(err))
		}
	}
}
