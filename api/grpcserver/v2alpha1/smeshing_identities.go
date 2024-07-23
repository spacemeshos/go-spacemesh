package v2alpha1

import (
	"context"
	"errors"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"golang.org/x/exp/maps"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

const NodeIdentities = "node_identities_v2alpha1"

const poetsMismatchWarning = "critical mismatch: more poets known than configured"

var ErrInvalidSmesherId = errors.New("smesher id is invalid")

type SmeshingIdentitiesService struct {
	db                     sql.Executor
	configuredPoetServices map[string]struct{}
}

func NewSmeshingIdentitiesService(db sql.Executor, configuredPoetServices map[string]struct{}) *SmeshingIdentitiesService {
	return &SmeshingIdentitiesService{
		db:                     db,
		configuredPoetServices: configuredPoetServices,
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

func (s *SmeshingIdentitiesService) PoetServices(_ context.Context, req *pb.PoetServicesRequest) (*pb.PoetServicesResponse, error) {
	nodeIdBytes := util.FromHex(req.SmesherIdHex)
	if len(nodeIdBytes) == 0 {
		return nil, status.Error(codes.InvalidArgument, ErrInvalidSmesherId.Error())
	}

	nodeId := types.BytesToNodeID(nodeIdBytes)

	knownPoetsRegs, err := nipost.PoetRegistrations(s.db, nodeId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	var criticalMismatchFound bool

	knownAddrs := make([]string, len(knownPoetsRegs))
	for i := range knownPoetsRegs {
		knownAddrs[i] = knownPoetsRegs[i].Address
		if _, ok := s.configuredPoetServices[knownPoetsRegs[i].Address]; !ok {
			criticalMismatchFound = true
		}
	}

	resp := &pb.PoetServicesResponse{
		RegisteredPoets: knownAddrs,
		ConfiguredPoets: maps.Keys(s.configuredPoetServices),
	}

	if criticalMismatchFound {
		resp.Warning = poetsMismatchWarning
	}
	return resp, nil
}
