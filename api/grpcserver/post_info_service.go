package grpcserver

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var statusMap map[int]pb.PostState_State = map[int]pb.PostState_State{
	0: pb.PostState_IDLE,
	1: pb.PostState_PROVING,
}

// PostInfoService provides information about connected PostServices.
type PostInfoService struct {
	log *zap.Logger

	states postState
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
func NewPostInfoService(log *zap.Logger, states postState) *PostInfoService {
	return &PostInfoService{
		log:    log,
		states: states,
	}
}

func (s *PostInfoService) PostStates(context.Context, *pb.PostStatesRequest) (*pb.PostStatesResponse, error) {
	states := s.states.PostStates()
	pbState := make([]*pb.PostState, 0, len(states))
	for id, state := range states {
		nodeState, ok := statusMap[state]
		if !ok {
			s.log.Panic("unknown status", zap.Stringer("pubkey", id), zap.Int("status", int(state)))
		}

		pbState = append(pbState, &pb.PostState{
			Id:    id.Bytes(),
			State: nodeState,
		})
	}

	return &pb.PostStatesResponse{States: pbState}, nil
}
