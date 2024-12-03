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
				Time: timestamppb.New(info.Time),
			}
			if info.PublishEpoch != nil {
				epoch := info.PublishEpoch.Uint32()
				identityStateInfo.PublishEpoch = &epoch
			}

			switch s := info.State.(type) {
			case *identity.WaitForATXSynced:
				identityStateInfo.State = pb.IdentityState_WAIT_FOR_ATX_SYNCED
			case *identity.Retrying:
				identityStateInfo.State = pb.IdentityState_RETRYING
				identityStateInfo.Metadata = &pb.IdentityStateInfo_Retrying{
					Retrying: &pb.RetryingState{
						Message: s.Error.Error(),
					},
				}
			case *identity.WaitingForPoetRegistrationWindow:
				identityStateInfo.State = pb.IdentityState_WAITING_FOR_POET_REGISTRATION_WINDOW
			case *identity.PoetChallengeReady:
				identityStateInfo.State = pb.IdentityState_POET_CHALLENGE_READY
			case *identity.PoetRegistered:
				identityStateInfo.State = pb.IdentityState_POET_REGISTERED
				identityStateInfo.Metadata = &pb.IdentityStateInfo_PoetRegistered{
					PoetRegistered: &pb.PoetRegisteredState{
						Registrations: castRegistrations(s.Registrations),
					},
				}
			case *identity.WaitForPoetRoundEnd:
				identityStateInfo.State = pb.IdentityState_WAIT_FOR_POET_ROUND_END
				identityStateInfo.Metadata = &pb.IdentityStateInfo_WaitForPoetRoundEnd{
					WaitForPoetRoundEnd: &pb.WaitForPoetRoundEndState{
						RoundEnd:        timestamppb.New(s.RoundEnd),
						PublishEpochEnd: timestamppb.New(s.PublishEpochEnd),
					},
				}
			case *identity.PoetProofReceived:
				identityStateInfo.State = pb.IdentityState_POET_PROOF_RECEIVED
				identityStateInfo.Metadata = &pb.IdentityStateInfo_PoetProofReceived{
					PoetProofReceived: &pb.PoetProofReceivedState{
						PoetUrl: s.PoetUrl,
					},
				}
			case *identity.GeneratingPostProof:
				identityStateInfo.State = pb.IdentityState_GENERATING_POST_PROOF
			case *identity.PostProofReady:
				identityStateInfo.State = pb.IdentityState_POST_PROOF_READY
			case *identity.ATXReady:
				identityStateInfo.State = pb.IdentityState_ATX_READY
			case *identity.ATXBroadcasted:
				identityStateInfo.State = pb.IdentityState_ATX_BROADCASTED
				identityStateInfo.Metadata = &pb.IdentityStateInfo_AtxBroadcasted{
					AtxBroadcasted: &pb.AtxBroadcastedState{
						AtxId: s.AtxId.Bytes(),
					},
				}
			case *identity.ProposalPublished:
				identityStateInfo.State = pb.IdentityState_PROPOSAL_PUBLISHED
				identityStateInfo.Metadata = &pb.IdentityStateInfo_ProposalPublished{
					ProposalPublished: &pb.ProposalPublishedState{
						Proposal: s.Proposal.Bytes(),
						Layer:    s.Layer.Uint32(),
					},
				}
			default:
				identityStateInfo.State = pb.IdentityState_UNSPECIFIED
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
