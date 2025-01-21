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
	ErrorMsg string
}

func (s *Retrying) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_RETRYING,
		Metadata: &pb.IdentityStateInfo_Retrying{
			Retrying: &pb.RetryingState{
				Message: s.ErrorMsg,
			},
		},
	}
}

// poet.
type WaitingForPoetRegistrationWindow struct {
	Publish types.EpochID
}

func (s *WaitingForPoetRegistrationWindow) APIStateInfo() *pb.IdentityStateInfo {
	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_WAITING_FOR_POET_REGISTRATION_WINDOW,
		PublishEpoch: &epoch,
	}
}

// building nipost challenge.
type PoetChallengeReady struct {
	Publish types.EpochID
}

func (s *PoetChallengeReady) APIStateInfo() *pb.IdentityStateInfo {
	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_POET_CHALLENGE_READY,
		PublishEpoch: &epoch,
	}
}

type PoetRegistered struct {
	Publish       types.EpochID
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

	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_POET_REGISTERED,
		PublishEpoch: &epoch,
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
	Publish         types.EpochID
}

func (s *WaitForPoetRoundEnd) APIStateInfo() *pb.IdentityStateInfo {
	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_WAIT_FOR_POET_ROUND_END,
		PublishEpoch: &epoch,
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
	Publish types.EpochID
}

func (s *PoetProofReceived) APIStateInfo() *pb.IdentityStateInfo {
	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_POET_PROOF_RECEIVED,
		PublishEpoch: &epoch,
		Metadata: &pb.IdentityStateInfo_PoetProofReceived{
			PoetProofReceived: &pb.PoetProofReceivedState{
				PoetUrl: s.PoetUrl,
			},
		},
	}
}

// post.
type GeneratingPostProof struct {
	Publish types.EpochID
}

func (s *GeneratingPostProof) APIStateInfo() *pb.IdentityStateInfo {
	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_GENERATING_POST_PROOF,
		PublishEpoch: &epoch,
	}
}

type PostProofReady struct {
	Publish types.EpochID
}

func (s *PostProofReady) APIStateInfo() *pb.IdentityStateInfo {
	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_POST_PROOF_READY,
		PublishEpoch: &epoch,
	}
}

// atx.
type ATXReady struct {
	Publish types.EpochID
}

func (s *ATXReady) APIStateInfo() *pb.IdentityStateInfo {
	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_ATX_READY,
		PublishEpoch: &epoch,
	}
}

type ATXBroadcasted struct {
	AtxId   types.ATXID
	Publish types.EpochID
}

func (s *ATXBroadcasted) APIStateInfo() *pb.IdentityStateInfo {
	epoch := s.Publish.Uint32()
	return &pb.IdentityStateInfo{
		State:        pb.IdentityState_ATX_BROADCASTED,
		PublishEpoch: &epoch,
		Metadata: &pb.IdentityStateInfo_AtxBroadcasted{
			AtxBroadcasted: &pb.AtxBroadcastedState{
				AtxId: s.AtxId.Bytes(),
			},
		},
	}
}

// proposal.
type ProposalBuildFailed struct {
	ErrorMsg string
	Layer    types.LayerID
}

func (s *ProposalBuildFailed) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_PROPOSAL_BUILD_FAILED,
		Metadata: &pb.IdentityStateInfo_ProposalBuildFailed{
			ProposalBuildFailed: &pb.ProposalBuildFailedState{
				Message: s.ErrorMsg,
				Layer:   s.Layer.Uint32(),
			},
		},
	}
}

type ProposalPublishFailed struct {
	ErrorMsg string
	Proposal types.ProposalID
	Layer    types.LayerID
}

func (s *ProposalPublishFailed) APIStateInfo() *pb.IdentityStateInfo {
	return &pb.IdentityStateInfo{
		State: pb.IdentityState_PROPOSAL_PUBLISH_FAILED,
		Metadata: &pb.IdentityStateInfo_ProposalPublishFailed{
			ProposalPublishFailed: &pb.ProposalPublishFailedState{
				Message:  s.ErrorMsg,
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
				Layers: rst,
			},
		},
	}
}
