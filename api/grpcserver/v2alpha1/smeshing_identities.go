package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"golang.org/x/exp/maps"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

const SmeshingIdentities = "smeshing_identities_v2alpha1"

const poetsMismatchWarning = "poet is not configured, proof will not be fetched"

type SmeshingIdentitiesService struct {
	db                     sql.Database
	states                 identityState
	configuredPoetServices map[string]struct{}
}

func NewSmeshingIdentitiesService(
	db sql.Database,
	configuredPoetServices map[string]struct{},
	states identityState,
) *SmeshingIdentitiesService {
	return &SmeshingIdentitiesService{
		db:                     db,
		configuredPoetServices: configuredPoetServices,
		states:                 states,
	}
}

var statusMap = map[activation.IdentityState]pb.IdentityStatus{
	activation.IdentityStateWaitForATXSyncing:     pb.IdentityStatus_IS_SYNCING,
	activation.IdentityStateWaitForPoetRoundStart: pb.IdentityStatus_WAIT_FOR_POET_ROUND_START,
	activation.IdentityStateWaitForPoetRoundEnd:   pb.IdentityStatus_WAIT_FOR_POET_ROUND_END,
	activation.IdentityStateFetchingProofs:        pb.IdentityStatus_FETCHING_PROOFS,
	activation.IdentityStatePostProving:           pb.IdentityStatus_POST_PROVING,
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

func (s *SmeshingIdentitiesService) PoetServices(
	ctx context.Context,
	_ *pb.PoetServicesRequest,
) (*pb.PoetServicesResponse, error) {
	pbIdentities := make(map[types.NodeID]*pb.PoetServicesResponse_Identity)

	err := s.db.WithTx(ctx, func(tx sql.Transaction) error {
		for desc, state := range s.states.IdentityStates() {
			pbIdentities[desc.NodeID()] = &pb.PoetServicesResponse_Identity{
				SmesherId: desc.NodeID().Bytes(),
				Status:    statusMap[state],
			}

			if state != activation.IdentityStateWaitForPoetRoundEnd {
				continue
			}

			regInfos, err := s.collectPoetInfos(tx, desc.NodeID())
			if err != nil {
				return status.Error(codes.Internal, err.Error())
			}
			pbIdentities[desc.NodeID()].PoetInfos = maps.Values(regInfos)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &pb.PoetServicesResponse{Identities: maps.Values(pbIdentities)}, nil
}

func (s *SmeshingIdentitiesService) collectPoetInfos(db sql.Executor, nodeId types.NodeID) (
	map[string]*pb.PoetServicesResponse_Identity_PoetInfo,
	error,
) {
	dbRegs, err := nipost.PoetRegistrations(db, nodeId)
	if err != nil {
		return nil, err
	}

	identityRegInfos := make(map[string]*pb.PoetServicesResponse_Identity_PoetInfo)
	for _, reg := range dbRegs {
		var (
			status  pb.RegistrationStatus
			warning string
		)

		_, ok := s.configuredPoetServices[reg.Address]
		switch {
		case !ok:
			status = pb.RegistrationStatus_RESIDUAL_REG
			warning = poetsMismatchWarning
		case reg.RoundID == "":
			status = pb.RegistrationStatus_FAILED_REG
		default:
			status = pb.RegistrationStatus_SUCCESS_REG
		}

		identityRegInfos[reg.Address] = &pb.PoetServicesResponse_Identity_PoetInfo{
			Url:                reg.Address,
			PoetRoundEnd:       timestamppb.New(reg.RoundEnd),
			RegistrationStatus: status,
			Warning:            warning,
		}
	}

	for poetAddr := range s.configuredPoetServices {
		if _, ok := identityRegInfos[poetAddr]; !ok {
			identityRegInfos[poetAddr] = &pb.PoetServicesResponse_Identity_PoetInfo{
				Url:                poetAddr,
				RegistrationStatus: pb.RegistrationStatus_NO_REG,
			}
		}
	}
	return identityRegInfos, nil
}
