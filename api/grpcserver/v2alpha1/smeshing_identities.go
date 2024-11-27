package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
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

var statusMap = map[activation.IdentityState]pb.IdentityState{
	activation.IdentityStateNotSet:                           pb.IdentityState_UNSPECIFIED,
	activation.IdentityStateWaitForATXSynced:                 pb.IdentityState_WAIT_FOR_ATX_SYNCED,
	activation.IdentityStateRetrying:                         pb.IdentityState_RETRYING,
	activation.IdentityStateWaitingForPoetRegistrationWindow: pb.IdentityState_WAITING_FOR_POET_REGISTRATION_WINDOW,
	activation.IdentityStatePoetChallengeReady:               pb.IdentityState_POET_CHALLENGE_READY,
	activation.IdentityStatePoetRegistered:                   pb.IdentityState_POET_REGISTERED,
	activation.IdentityStateWaitForPoetRoundEnd:              pb.IdentityState_WAIT_FOR_POET_ROUND_END,
	activation.IdentityStatePoetProofReceived:                pb.IdentityState_POET_PROOF_RECEIVED,
	activation.IdentityStateGeneratingPostProof:              pb.IdentityState_GENERATING_POST_PROOF,
	activation.IdentityStatePostProofReady:                   pb.IdentityState_POST_PROOF_READY,
	activation.IdentityStateATXReady:                         pb.IdentityState_ATX_READY,
	activation.IdentityStateATXBroadcasted:                   pb.IdentityState_ATX_BROADCASTED,
	activation.IdentityStateProposalPublished:                pb.IdentityState_PROPOSAL_PUBLISHED,
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
			ts := timestamppb.New(info.Time)
			identityStateInfo := &pb.IdentityStateInfo{
				State:   statusMap[info.State],
				Time:    ts,
				Message: info.Message,
			}
			if info.PublishEpoch != nil {
				epoch := info.PublishEpoch.Uint32()
				identityStateInfo.PublishEpoch = &epoch
			}
			pbIdentities[nodeId.String()].History = append(pbIdentities[nodeId.String()].History, identityStateInfo)
		}
	}

	return &pb.IdentityStatesResponse{Identities: pbIdentities}, nil
}

func (s *SmeshingIdentitiesService) PoetInfo(
	ctx context.Context,
	_ *pb.PoetInfoRequest,
) (*pb.PoetInfoResponse, error) {
	poets := make(map[string]*pb.PoetInfo)
	for _, poet := range s.poetClients {
		info, err := poet.Info(ctx)
		if err != nil {
			return nil, err
		}

		poets[poet.Address()] = &pb.PoetInfo{
			PhaseShift: durationpb.New(info.PhaseShift),
			CycleGap:   durationpb.New(info.CycleGap),
		}
	}
	return &pb.PoetInfoResponse{
		Poets: poets,
	}, nil
}

func (s *SmeshingIdentitiesService) Eligibilities(
	ctx context.Context,
	_ *pb.EligibilitiesRequest,
) (*pb.EligibilitiesResponse, error) {
	eligibilities := s.states.AllEligibilities()

	pbEligibilities := make(map[string]*pb.EpochEligibilities)
	for nodeId, epochMap := range eligibilities {
		pbEligibilities[nodeId.String()] = &pb.EpochEligibilities{
			Epochs: make(map[uint32]*pb.Eligibilities),
		}
		for epoch, eli := range epochMap {
			pbEligibilities[nodeId.String()].Epochs[epoch.Uint32()] = &pb.Eligibilities{
				Eligibilities: castEligibilities(eli),
			}
		}
	}

	return &pb.EligibilitiesResponse{
		Eligibilities: pbEligibilities,
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
