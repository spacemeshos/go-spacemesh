package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

const SmeshingIdentities = "smeshing_identities_v2alpha1"

type SmeshingIdentitiesService struct {
	states      identityState
	poetClients []activation.PoetService
}

func NewSmeshingIdentitiesService(
	states identityState,
	poetClients []activation.PoetService,
) *SmeshingIdentitiesService {
	return &SmeshingIdentitiesService{
		states:      states,
		poetClients: poetClients,
	}
}

var statusMap = map[identity.State]pb.IdentityState{
	identity.StateNotSet:                           pb.IdentityState_UNSPECIFIED,
	identity.StateWaitForATXSynced:                 pb.IdentityState_WAIT_FOR_ATX_SYNCED,
	identity.StateRetrying:                         pb.IdentityState_RETRYING,
	identity.StateWaitingForPoetRegistrationWindow: pb.IdentityState_WAITING_FOR_POET_REGISTRATION_WINDOW,
	identity.StatePoetChallengeReady:               pb.IdentityState_POET_CHALLENGE_READY,
	identity.StatePoetRegistered:                   pb.IdentityState_POET_REGISTERED,
	identity.StateWaitForPoetRoundEnd:              pb.IdentityState_WAIT_FOR_POET_ROUND_END,
	identity.StatePoetProofReceived:                pb.IdentityState_POET_PROOF_RECEIVED,
	identity.StateGeneratingPostProof:              pb.IdentityState_GENERATING_POST_PROOF,
	identity.StatePostProofReady:                   pb.IdentityState_POST_PROOF_READY,
	identity.StateATXReady:                         pb.IdentityState_ATX_READY,
	identity.StateATXBroadcasted:                   pb.IdentityState_ATX_BROADCASTED,
	identity.StateProposalPublished:                pb.IdentityState_PROPOSAL_PUBLISHED,
}

func (s *SmeshingIdentitiesService) RegisterService(server *grpc.Server) {
	pb.RegisterSmeshingIdentitiesServiceServer(server, s)
}

func (s *SmeshingIdentitiesService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterSmeshingIdentitiesServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of this service.
func (s *SmeshingIdentitiesService) String() string {
	return "SmeshingIdentitiesService"
}

func (s *SmeshingIdentitiesService) States(
	ctx context.Context,
	_ *pb.IdentityStatesRequest,
) (*pb.IdentityStatesResponse, error) {
	pbIdentities := make(map[string]*pb.Identity)

	for nodeId, history := range s.states.All() {
		pbIdentities[nodeId.String()] = &pb.Identity{
			History: []*pb.IdentityStateInfo{},
		}

		for i := len(history) - 1; i >= 0; i-- {
			info := history[i]
			identityStateInfo := &pb.IdentityStateInfo{
				State: statusMap[info.State],
				Time:  timestamppb.New(info.Time),
			}
			if info.PublishEpoch != nil {
				epoch := info.PublishEpoch.Uint32()
				identityStateInfo.PublishEpoch = &epoch
			}

			if info.State == identity.StateRetrying && info.RetryingState != nil {
				identityStateInfo.Metadata = &pb.IdentityStateInfo_Retrying{
					Retrying: &pb.RetryingState{
						Message: info.RetryingState.Error.Error(),
					},
				}
			}

			if info.State == identity.StatePoetRegistered && info.PoetRegisteredState != nil {
				identityStateInfo.Metadata = &pb.IdentityStateInfo_PoetRegistered{
					PoetRegistered: &pb.PoetRegisteredState{
						Registrations: castRegistrations(info.PoetRegisteredState.Registrations),
					},
				}
			}

			if info.State == identity.StateWaitForPoetRoundEnd && info.WaitForPoetRoundEndState != nil {
				identityStateInfo.Metadata = &pb.IdentityStateInfo_WaitForPoetRoundEnd{
					WaitForPoetRoundEnd: &pb.WaitForPoetRoundEndState{
						RoundEnd:        timestamppb.New(info.WaitForPoetRoundEndState.RoundEnd),
						PublishEpochEnd: timestamppb.New(info.WaitForPoetRoundEndState.PublishEpochEnd),
					},
				}
			}

			if info.State == identity.StatePoetProofReceived && info.PoetProofReceivedState != nil {
				identityStateInfo.Metadata = &pb.IdentityStateInfo_PoetProofReceived{
					PoetProofReceived: &pb.PoetProofReceivedState{
						PoetUrl: info.PoetProofReceivedState.PoetUrl,
					},
				}
			}

			if info.State == identity.StateATXBroadcasted && info.AtxBroadcastedState != nil {
				identityStateInfo.Metadata = &pb.IdentityStateInfo_AtxBroadcasted{
					AtxBroadcasted: &pb.AtxBroadcastedState{
						AtxId: info.AtxBroadcastedState.AtxId.Bytes(),
					},
				}
			}

			if info.State == identity.StateProposalPublished && info.ProposalPublishedState != nil {
				identityStateInfo.Metadata = &pb.IdentityStateInfo_ProposalPublished{
					ProposalPublished: &pb.ProposalPublishedState{
						Proposal: info.ProposalPublishedState.Proposal.Bytes(),
						Layer:    info.ProposalPublishedState.Layer.Uint32(),
					},
				}
			}

			pbIdentities[nodeId.String()].History = append(pbIdentities[nodeId.String()].History, identityStateInfo)
		}
	}

	return &pb.IdentityStatesResponse{Identities: pbIdentities}, nil
}

func castRegistrations(regs []nipost.PoETRegistration) []*pb.PoETRegistration {
	rst := make([]*pb.PoETRegistration, 0, len(regs))
	for _, reg := range regs {
		rst = append(rst, &pb.PoETRegistration{
			ChallengeHash: reg.ChallengeHash.Bytes(),
			Address:       reg.Address,
			RoundId:       reg.RoundID,
			RoundEnd:      timestamppb.New(reg.RoundEnd),
		})
	}
	return rst
}

func (s *SmeshingIdentitiesService) PoetInfo(
	ctx context.Context,
	_ *pb.PoetInfoRequest,
) (*pb.PoetInfoResponse, error) {
	resp := &pb.PoetInfoResponse{
		Poets: []string{},
	}
	for _, poet := range s.poetClients {
		resp.Poets = append(resp.Poets, poet.Address())
	}

	return resp, nil
}

func (s *SmeshingIdentitiesService) Eligibilities(
	ctx context.Context,
	_ *pb.EligibilitiesRequest,
) (*pb.EligibilitiesResponse, error) {
	eligibilities := s.states.AllEligibilities()

	pbEpochEligibilities := make(map[string]*pb.EpochEligibilities)
	for nodeId, epochMap := range eligibilities {
		pbEpochEligibilities[nodeId.String()] = &pb.EpochEligibilities{
			Epochs: make(map[uint32]*pb.Eligibilities),
		}
		for epoch, eli := range epochMap {
			pbEpochEligibilities[nodeId.String()].Epochs[epoch.Uint32()] = &pb.Eligibilities{
				Eligibilities: castEligibilities(eli),
			}
		}
	}

	return &pb.EligibilitiesResponse{
		Identities: pbEpochEligibilities,
	}, nil
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

func (s *SmeshingIdentitiesService) Proposals(
	ctx context.Context,
	_ *pb.ProposalsRequest,
) (*pb.ProposalsResponse, error) {
	proposals := s.states.AllProposals()

	pbProposals := make(map[string]*pb.Proposals)
	for nodeId, prop := range proposals {
		pbProposals[nodeId.String()] = &pb.Proposals{Proposals: castProposals(prop)}
	}

	return &pb.ProposalsResponse{
		Proposals: pbProposals,
	}, nil
}

func castProposals(proposals []*types.Proposal) []*pb.Proposal {
	rst := make([]*pb.Proposal, 0, len(proposals))
	for _, prop := range proposals {
		rst = append(rst, &pb.Proposal{
			Layer:    prop.Layer.Uint32(),
			Proposal: prop.ID().Bytes(),
		})
	}
	return rst
}
