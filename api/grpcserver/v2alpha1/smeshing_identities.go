package v2alpha1

import (
	"context"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"golang.org/x/exp/maps"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

const SmeshingIdentities = "smeshing_identities_v2alpha1"

const poetsMismatchWarning = "poet is not configured, reconfiguration required"

type SmeshingIdentitiesService struct {
	db                     sql.Executor
	states                 identityState
	signers                map[types.NodeID]struct{}
	configuredPoetServices map[string]struct{}
}

func NewSmeshingIdentitiesService(
	db sql.Executor,
	configuredPoetServices map[string]struct{},
	states identityState,
	signers map[types.NodeID]struct{}) *SmeshingIdentitiesService {
	return &SmeshingIdentitiesService{
		db:                     db,
		configuredPoetServices: configuredPoetServices,
		states:                 states,
		signers:                signers,
	}
}

var statusMap = map[types.IdentityState]pb.IdentityStatus{
	types.WaitForATXSyncing:     pb.IdentityStatus_STATUS_IS_SYNCING,
	types.WaitForPoetRoundStart: pb.IdentityStatus_STATUS_WAIT_FOR_POET_ROUND_START,
	types.WaitForPoetRoundEnd:   pb.IdentityStatus_STATUS_WAIT_FOR_POET_ROUND_END,
	types.FetchingProofs:        pb.IdentityStatus_STATUS_FETCHING_PROOFS,
	types.PostProving:           pb.IdentityStatus_STATUS_POST_PROVING,
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

func (s *SmeshingIdentitiesService) PoetServices(_ context.Context, _ *pb.PoetServicesRequest) (*pb.PoetServicesResponse, error) {
	states := s.states.IdentityStates()

	pbIdentities := make(map[types.NodeID]*pb.PoetServicesResponse_Identity)
	nodeIdsToRequest := make([]types.NodeID, 0)

	for desc, state := range states {
		pbIdentities[desc.NodeID()] = &pb.PoetServicesResponse_Identity{
			SmesherIdHex: desc.NodeID().String(),
			Status:       statusMap[state],
		}

		if state != types.WaitForPoetRoundEnd {
			continue
		}
		nodeIdsToRequest = append(nodeIdsToRequest, desc.NodeID())
	}

	if len(nodeIdsToRequest) != 0 {
		regInfos, err := s.collectPoetInfos(nodeIdsToRequest)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		for nodeId, poetInfos := range regInfos {
			identity := pbIdentities[nodeId]
			identity.PoetInfos = maps.Values(poetInfos)
		}
	}
	return &pb.PoetServicesResponse{Identities: maps.Values(pbIdentities)}, nil
}

func (s *SmeshingIdentitiesService) collectPoetInfos(nodeIds []types.NodeID) (map[types.NodeID]map[string]*pb.PoetServicesResponse_Identity_PoetInfo, error) {
	dbRegs, err := nipost.PoetRegistrations(s.db, nodeIds...)
	if err != nil {
		return nil, err
	}

	identityRegInfos := make(map[types.NodeID]map[string]*pb.PoetServicesResponse_Identity_PoetInfo)
	for _, reg := range dbRegs {
		poetInfos, ok := identityRegInfos[reg.NodeId]
		if !ok {
			poetInfos = make(map[string]*pb.PoetServicesResponse_Identity_PoetInfo)
		}

		var (
			status  pb.RegistrationStatus
			warning string
		)

		_, ok = s.configuredPoetServices[reg.Address]
		switch {
		case !ok:
			status = pb.RegistrationStatus_STATUS_RESIDUAL_REG
			warning = poetsMismatchWarning
		case reg.RoundID == "":
			status = pb.RegistrationStatus_STATUS_FAILED_REG
		default:
			status = pb.RegistrationStatus_STATUS_SUCCESS_REG
		}

		poetInfos[reg.Address] = &pb.PoetServicesResponse_Identity_PoetInfo{
			Url:                reg.Address,
			PoetRoundEnd:       timestamppb.New(reg.RoundEnd),
			RegistrationStatus: status,
			Warning:            warning,
		}

		identityRegInfos[reg.NodeId] = poetInfos
	}

	for id, poets := range identityRegInfos {
		for poetAddr := range s.configuredPoetServices {
			if _, ok := poets[poetAddr]; !ok {
				// registration is missed
				identityRegInfos[id][poetAddr] = &pb.PoetServicesResponse_Identity_PoetInfo{
					Url:                poetAddr,
					RegistrationStatus: pb.RegistrationStatus_STATUS_NO_REG,
				}
			}
		}
	}
	return identityRegInfos, nil
}
