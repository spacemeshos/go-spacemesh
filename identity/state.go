package identity

import (
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

type State interface {
	APIStateInfo() *pb.IdentityStateInfo
}

type WaitForATXSynced struct{}

func (s *WaitForATXSynced) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_WAIT_FOR_ATX_SYNCED,
	}
}

type Retrying struct {
	Error error
}

func (s *Retrying) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_RETRYING,
		Metadata: &pb.IdentityStateInfo_Retrying{
			Retrying: &pb.RetryingState{
				Message: s.Error.Error(),
			},
		},
	}
}

// poet.
type WaitingForPoetRegistrationWindow struct{}

func (s *WaitingForPoetRegistrationWindow) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_WAITING_FOR_POET_REGISTRATION_WINDOW,
	}
}

// building nipost challenge.
type PoetChallengeReady struct{}

func (s *PoetChallengeReady) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_POET_CHALLENGE_READY,
	}
}

type PoetRegistered struct {
	Registrations []nipost.PoETRegistration
}

func (s *PoetRegistered) APIStateInfo() *pb.IdentityStateInfo {
	rst := make([]*pb.PoETRegistration, 0, len(s.Registrations))
	for _, reg := range s.Registrations {
		rst = append(rst, &pb.PoETRegistration{
			ChallengeHash: reg.ChallengeHash.Bytes(),
			Address:       reg.Address,
			RoundId:       reg.RoundID,
			RoundEnd:      timestamppb.New(reg.RoundEnd),
		})
	}

	return &pb.IdentityStateInfo{
		State: pb.IdentityState_POET_REGISTERED,
		Metadata: &pb.IdentityStateInfo_PoetRegistered{
			PoetRegistered: &pb.PoetRegisteredState{
				Registrations: rst,
			},
		},
	}
}

// 2w pass...
type WaitForPoetRoundEnd struct {
	RoundEnd        time.Time
	PublishEpochEnd time.Time
}

func (s *WaitForPoetRoundEnd) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_WAIT_FOR_POET_ROUND_END,
		Metadata: &pb.IdentityStateInfo_WaitForPoetRoundEnd{
			WaitForPoetRoundEnd: &pb.WaitForPoetRoundEndState{
				RoundEnd:        timestamppb.New(s.RoundEnd),
				PublishEpochEnd: timestamppb.New(s.PublishEpochEnd),
			},
		},
	}
}

type PoetProofReceived struct {
	PoetUrl string
}

func (s *PoetProofReceived) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_POET_PROOF_RECEIVED,
		Metadata: &pb.IdentityStateInfo_PoetProofReceived{
			PoetProofReceived: &pb.PoetProofReceivedState{
				PoetUrl: s.PoetUrl,
			},
		},
	}
}

// post.
type GeneratingPostProof struct{}

func (s *GeneratingPostProof) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_GENERATING_POST_PROOF,
	}
}

type PostProofReady struct{}

func (s *PostProofReady) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_POST_PROOF_READY,
	}
}

// atx.
type ATXReady struct{}

func (s *ATXReady) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_ATX_READY,
	}
}

type ATXBroadcasted struct {
	AtxId types.ATXID
}

func (s *ATXBroadcasted) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_ATX_BROADCASTED,
		Metadata: &pb.IdentityStateInfo_AtxBroadcasted{
			AtxBroadcasted: &pb.AtxBroadcastedState{
				AtxId: s.AtxId.Bytes(),
			},
		},
	}
}

// proposal.
type ProposalPublishFailed struct {
	Error    error
	Proposal types.ProposalID
	Layer    types.LayerID
}

func (s *ProposalPublishFailed) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_PROPOSAL_PUBLISH_FAILED,
		Metadata: &pb.IdentityStateInfo_ProposalPublishFailed{
			ProposalPublishFailed: &pb.ProposalPublishFailedState{
				Message:  s.Error.Error(),
				Proposal: s.Proposal.Bytes(),
				Layer:    s.Layer.Uint32(),
			},
		},
	}
}

type ProposalPublished struct {
	Proposal types.ProposalID
	Layer    types.LayerID
}

func (s *ProposalPublished) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_PROPOSAL_PUBLISHED,
		Metadata: &pb.IdentityStateInfo_ProposalPublished{
			ProposalPublished: &pb.ProposalPublishedState{
				Proposal: s.Proposal.Bytes(),
				Layer:    s.Layer.Uint32(),
			},
		},
	}
}

type Eligible struct {
	Epoch  uint32
	Layers map[types.LayerID][]types.VotingEligibility
}

func (s *Eligible) APIStateInfo() *pb.IdentityStateInfo {
	rst := make([]*pb.Eligibility, 0, len(s.Layers))
	for lid, eligs := range s.Layers {
		rst = append(rst, &pb.Eligibility{
			Layer: lid.Uint32(),
			Count: uint32(len(eligs)),
		})
	}

	return &pb.IdentityStateInfo{
		State: pb.IdentityState_ELIGIBLE,
		Metadata: &pb.IdentityStateInfo_Eligible{
			Eligible: &pb.Eligible{
				Epoch:  s.Epoch,
				Layers: rst,
			},
		},
	}
}
