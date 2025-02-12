package v2beta1

import (
	"context"
	"fmt"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	identity "github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

const SmeshingIdentities = "smeshing_identities_v2beta1"

type identityState interface {
	All(ops builder.Operations) ([]identity.IdStateInfo, error)
	AllProposals() map[types.NodeID][]*types.Proposal
	AllEligibilities() map[types.NodeID]map[types.LayerID][]types.VotingEligibility
}

type SmeshingIdentitiesService struct {
	states      identityState
	poetClients []activation.PoetService
	poetConfig  activation.PoetConfig
}

func NewSmeshingIdentitiesService(
	states identityState,
	poetClients []activation.PoetService,
	poetConfig activation.PoetConfig,
) *SmeshingIdentitiesService {
	return &SmeshingIdentitiesService{
		states:      states,
		poetClients: poetClients,
		poetConfig:  poetConfig,
	}
}

func (s *SmeshingIdentitiesService) RegisterService(server *grpc.Server) {
	pb.RegisterSmeshingIdentitiesServiceServer(server, s)
}

func (s *SmeshingIdentitiesService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterSmeshingIdentitiesServiceHandlerServer(context.Background(), mux, s)
}

func (s *SmeshingIdentitiesService) Path() string {
	return "/spacemesh.v2beta1.SmeshingIdentitiesService/"
}

func (s *SmeshingIdentitiesService) States(
	ctx context.Context,
	request *pb.IdentityStatesRequest,
) (*pb.IdentityStatesResponse, error) {
	switch {
	case request.Limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}

	ops, err := toEventOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	var states []*pb.IdentityStateInfo
	events, err := s.states.All(ops)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	for _, info := range events {
		identityStateInfo := info.State.APIStateInfo()
		identityStateInfo.Time = timestamppb.New(info.Time)
		identityStateInfo.Smesher = info.ID.Bytes()

		states = append(states, identityStateInfo)
	}

	return &pb.IdentityStatesResponse{States: states}, nil
}

func toEventOperations(filter *pb.IdentityStatesRequest) (builder.Operations, error) {
	ops := builder.Operations{}
	if filter == nil {
		return ops, nil
	}

	if len(filter.States) > 0 {
		// convert []IdentityState to []int32
		states := make([]int32, len(filter.States))
		for i, state := range filter.States {
			states[i] = int32(state)
		}
		ops.Filter = append(ops.Filter, builder.Op{
			Field: "kind",
			Token: builder.In,
			Value: states,
		})
	}

	if filter.Limit != 0 {
		ops.Modifiers = append(ops.Modifiers, builder.Modifier{
			Key:   builder.Limit,
			Value: int64(filter.Limit),
		})
	}

	if filter.From != nil {
		if err := filter.From.CheckValid(); err != nil {
			return ops, fmt.Errorf("'from' is invalid: %w", err)
		}
		ops.Filter = append(ops.Filter, builder.Op{
			Field: "timestamp",
			Token: builder.Gte,
			Value: filter.From.AsTime().UnixMicro(),
		})
	}
	if filter.To != nil {
		if err := filter.To.CheckValid(); err != nil {
			return ops, fmt.Errorf("'to' is invalid: %w", err)
		}
		ops.Filter = append(ops.Filter, builder.Op{
			Field: "timestamp",
			Token: builder.Lt,
			Value: filter.To.AsTime().UnixMicro(),
		})
	}

	switch filter.Order {
	case pb.SortOrder_ASC:
		ops.Modifiers = append(ops.Modifiers, builder.Modifier{
			Key:   builder.OrderBy,
			Value: "timestamp asc",
		})
	case pb.SortOrder_DESC:
		ops.Modifiers = append(ops.Modifiers, builder.Modifier{
			Key:   builder.OrderBy,
			Value: "timestamp desc",
		})
	default:
		return ops, fmt.Errorf("unknown sort order: %d", filter.Order)
	}

	return ops, nil
}

func (s *SmeshingIdentitiesService) PoetInfo(
	ctx context.Context,
	_ *pb.PoetInfoRequest,
) (*pb.PoetInfoResponse, error) {
	resp := &pb.PoetInfoResponse{
		Poets: []string{},
		Config: &pb.PoetConfig{
			CycleGap:   durationpb.New(s.poetConfig.CycleGap),
			PhaseShift: durationpb.New(s.poetConfig.PhaseShift),
		},
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
	for nodeId, layersMap := range eligibilities {
		id := nodeId.String()
		epochs := make(map[uint32]*pb.Eligibilities)
		for layer, eligibilitiesInLayer := range layersMap {
			epoch := layer.GetEpoch().Uint32()
			if _, ok := epochs[epoch]; !ok {
				epochs[epoch] = new(pb.Eligibilities)
			}
			epochs[epoch].Eligibilities = append(
				epochs[epoch].Eligibilities,
				&pb.ProposalEligibility{
					Layer: layer.Uint32(),
					Count: uint32(len(eligibilitiesInLayer)),
				},
			)
		}
		pbEpochEligibilities[id] = &pb.EpochEligibilities{Epochs: epochs}
	}

	return &pb.EligibilitiesResponse{
		Identities: pbEpochEligibilities,
	}, nil
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
