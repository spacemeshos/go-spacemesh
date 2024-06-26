package events

import (
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
)

type UserEvent struct {
	Event *pb.Event
}

func EmitBeacon(epoch types.EpochID, beacon types.Beacon) {
	const help = "Node computed randomness beacon, which will be used to determine consensus eligibility."
	emitUserEvent(
		help,
		false,
		&pb.Event_Beacon{
			Beacon: &pb.EventBeacon{
				Epoch:  epoch.Uint32(),
				Beacon: beacon.Bytes(),
			},
		},
	)
}

func EmitInitStart(nodeID types.NodeID, commitment types.ATXID) {
	const help = "Node started PoST data initialization. Initialization will not be performed again if " +
		"already completed."
	emitUserEvent(
		help,
		false,
		&pb.Event_InitStart{
			InitStart: &pb.EventInitStart{
				Smesher:    nodeID.Bytes(),
				Commitment: commitment.Bytes(),
			},
		},
	)
}

func EmitInitFailure(nodeID types.NodeID, commitment types.ATXID, err error) {
	const help = "Node failed PoST data initialization."
	emitUserEvent(
		help,
		true,
		&pb.Event_InitFailed{
			InitFailed: &pb.EventInitFailed{
				Smesher:    nodeID.Bytes(),
				Commitment: commitment.Bytes(),
				Error:      err.Error(),
			},
		},
	)
}

func EmitInitComplete(nodeID types.NodeID) {
	const help = "Node successfully completed PoST data initialization."
	emitUserEvent(
		help,
		false,
		&pb.Event_InitComplete{
			InitComplete: &pb.EventInitComplete{
				Smesher: nodeID.Bytes(),
			},
		},
	)
}

func EmitPoetWaitRound(nodeID types.NodeID, current, publish types.EpochID, wait time.Time) {
	const help = "Node is waiting for PoET registration window in current epoch to open. " +
		"After this it will submit challenge and wait until PoET round ends in publish epoch."
	emitUserEvent(
		help,
		false,
		&pb.Event_PoetWaitRound{PoetWaitRound: &pb.EventPoetWaitRound{
			Current: current.Uint32(),
			Publish: publish.Uint32(),
			Wait:    durationpb.New(time.Until(wait)),
			Until:   timestamppb.New(wait),
			Smesher: nodeID.Bytes(),
		}},
	)
}

func EmitPoetWaitProof(nodeID types.NodeID, publish types.EpochID, wait time.Time) {
	const help = "Node is waiting for PoET to complete. " +
		"After it's complete, the node will fetch the PoET proof, generate a PoST proof, " +
		"and finally publish an ATX to establish eligibility for rewards in the target epoch."
	emitUserEvent(
		help,
		false,
		&pb.Event_PoetWaitProof{
			PoetWaitProof: &pb.EventPoetWaitProof{
				Publish: publish.Uint32(),
				Target:  publish.Add(1).Uint32(),
				Wait:    durationpb.New(time.Until(wait)),
				Until:   timestamppb.New(wait),
				Smesher: nodeID.Bytes(),
			},
		},
	)
}

func EmitPostServiceStarted() {
	const help = "Node started local PoST service."
	emitUserEvent(
		help,
		false,
		&pb.Event_PostServiceStarted{},
	)
}

func EmitPostServiceStopped() {
	const help = "Node stopped local PoST service."
	emitUserEvent(
		help,
		false,
		&pb.Event_PostServiceStopped{},
	)
}

func EmitPostStart(nodeID types.NodeID, challenge []byte) {
	const help = "Node started PoST execution using the challenge from PoET."
	emitUserEvent(
		help,
		false,
		&pb.Event_PostStart{
			PostStart: &pb.EventPostStart{
				Challenge: challenge,
				Smesher:   nodeID.Bytes(),
			},
		},
	)
}

func EmitPostComplete(nodeID types.NodeID, challenge []byte) {
	const help = "Node finished PoST execution using PoET challenge."
	emitUserEvent(
		help,
		false,
		&pb.Event_PostComplete{
			PostComplete: &pb.EventPostComplete{
				Challenge: challenge,
				Smesher:   nodeID.Bytes(),
			},
		},
	)
}

func EmitPostFailure(nodeID types.NodeID) {
	const help = "Node failed PoST execution."
	emitUserEvent(
		help,
		true,
		&pb.Event_PostComplete{
			PostComplete: &pb.EventPostComplete{
				Smesher: nodeID.Bytes(),
			},
		},
	)
}

func EmitInvalidPostProof(nodeID types.NodeID) {
	const help = "Node generated invalid POST proof. Please verify your POST data."
	emitUserEvent(
		help,
		true,
		&pb.Event_PostComplete{PostComplete: &pb.EventPostComplete{
			Smesher: nodeID.Bytes(),
		}},
	)
}

func EmitAtxPublished(
	nodeID types.NodeID,
	current, target types.EpochID,
	atxID types.ATXID,
	wait time.Time,
) {
	const help = "Node published activation for the current epoch. " +
		"It now needs to wait until the target epoch when it will be eligible for rewards."
	emitUserEvent(
		help,
		false,
		&pb.Event_AtxPublished{
			AtxPublished: &pb.EventAtxPubished{
				Current: current.Uint32(),
				Target:  target.Uint32(),
				Id:      atxID.Bytes(),
				Wait:    durationpb.New(time.Until(wait)),
				Until:   timestamppb.New(wait),
				Smesher: nodeID.Bytes(),
			},
		},
	)
}

func EmitEligibilities(
	nodeID types.NodeID,
	epoch types.EpochID,
	beacon types.Beacon,
	atxID types.ATXID,
	activeSetSize uint32,
	eligibilities map[types.LayerID][]types.VotingEligibility,
) {
	const help = "Node computed eligibilities for the epoch. " +
		"Rewards will be received after successfully publishing proposals at specified layers. " +
		"The rewards actually received will be based on the number of other participants in each layer."
	emitUserEvent(
		help,
		false,
		&pb.Event_Eligibilities{
			Eligibilities: &pb.EventEligibilities{
				Epoch:         epoch.Uint32(),
				Beacon:        beacon.Bytes(),
				Atx:           atxID.Bytes(),
				ActiveSetSize: activeSetSize,
				Eligibilities: castEligibilities(eligibilities),
				Smesher:       nodeID.Bytes(),
			},
		},
	)
}

func castEligibilities(proofs map[types.LayerID][]types.VotingEligibility) []*pb.ProposalEligibility {
	rst := make([]*pb.ProposalEligibility, 0, len(proofs))
	for lid, eligs := range proofs {
		rst = append(rst, &pb.ProposalEligibility{
			Layer: lid.Uint32(),
			Count: uint32(len(eligs)),
		})
	}
	return rst
}

func EmitProposal(nodeID types.NodeID, layer types.LayerID, proposal types.ProposalID) {
	const help = "Node published proposal. Rewards will be received once proposal is included in the block."
	emitUserEvent(
		help,
		false,
		&pb.Event_Proposal{
			Proposal: &pb.EventProposal{
				Layer:    layer.Uint32(),
				Proposal: proposal[:],
				Smesher:  nodeID.Bytes(),
			},
		},
	)
}

func EmitOwnMalfeasanceProof(nodeID types.NodeID, mp *wire.MalfeasanceProof) {
	const help = "Node committed malicious behavior. Identity will be canceled."
	emitUserEvent(
		help,
		false,
		&pb.Event_Malfeasance{
			Malfeasance: &pb.EventMalfeasance{
				Proof: ToMalfeasancePB(nodeID, mp, false),
			},
		},
	)
}

func emitUserEvent(help string, failure bool, details pb.IsEventDetails) {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		if err := reporter.emitUserEvent(UserEvent{Event: &pb.Event{
			Timestamp: timestamppb.New(time.Now()),
			Help:      help,
			Failure:   failure,
			Details:   details,
		}}); err != nil {
			log.With().Error("failed to emit event", log.Err(err))
		}
	}
}

func ToMalfeasancePB(nodeID types.NodeID, mp *wire.MalfeasanceProof, includeProof bool) *pb.MalfeasanceProof {
	if mp == nil {
		return &pb.MalfeasanceProof{}
	}
	kind := pb.MalfeasanceProof_MALFEASANCE_UNSPECIFIED
	switch mp.Proof.Type {
	case wire.MultipleATXs:
		kind = pb.MalfeasanceProof_MALFEASANCE_ATX
	case wire.MultipleBallots:
		kind = pb.MalfeasanceProof_MALFEASANCE_BALLOT
	case wire.HareEquivocation:
		kind = pb.MalfeasanceProof_MALFEASANCE_HARE
	case wire.InvalidPostIndex:
		kind = pb.MalfeasanceProof_MALFEASANCE_POST_INDEX
	case wire.InvalidPrevATX:
		kind = pb.MalfeasanceProof_MALFEASANCE_INCORRECT_PREV_ATX
	}
	result := &pb.MalfeasanceProof{
		SmesherId: &pb.SmesherId{Id: nodeID.Bytes()},
		Layer:     &pb.LayerNumber{Number: mp.Layer.Uint32()},
		Kind:      kind,
		DebugInfo: wire.MalfeasanceInfo(nodeID, mp),
	}
	if includeProof {
		data, _ := codec.Encode(mp)
		result.Proof = data
	}
	return result
}
