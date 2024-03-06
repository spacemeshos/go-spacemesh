package grpcserver

import (
	"context"
	"fmt"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var statusMap map[int]pb.ServiceState_Status = map[int]pb.ServiceState_Status{
	1: pb.ServiceState_READY_PROOFING,
	2: pb.ServiceState_PROOF_RECEIVED,
}

// PostInfoService provides information about connected PostServices.
type PostInfoService struct {
	log *zap.Logger

	builder atxBuilder
}

// RegisterService registers this service with a grpc server instance.
func (s *PostInfoService) RegisterService(server *grpc.Server) {
	pb.RegisterPostInfoServiceServer(server, s)
}

func (s *PostInfoService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterPostInfoServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of this service.
func (s *PostInfoService) String() string {
	return "PostInfoService"
}

// NewPostInfoService creates a new instance of the post info grpc service.
func NewPostInfoService(log *zap.Logger, builder atxBuilder) *PostInfoService {
	return &PostInfoService{
		log:     log,
		builder: builder,
	}
}

func (s *PostInfoService) ServiceStates(context.Context, *pb.ServiceStatesRequest) (*pb.ServiceStatesResponse, error) {
	state := s.builder.PostState()
	pbState := make([]*pb.ServiceState, 0, len(state))
	for nodeID, state := range state {
		nodeState, ok := statusMap[state]
		if !ok {
			s.log.Warn("unknown status",
				zap.Stringer("node_id", nodeID),
				zap.Int("status", state),
			)
			return nil, status.Error(codes.Internal, fmt.Sprintf("identity %s is in unknown state", nodeID.String()))
		}

		pbState = append(pbState, &pb.ServiceState{
			NodeId: nodeID.Bytes(),
			Status: nodeState,
		})
	}

	return &pb.ServiceStatesResponse{States: pbState}, nil
}
