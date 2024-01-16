package grpcserver

import (
	"context"
	"fmt"
	"sync"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// PostService is a grpc server that PoST nodes can connect to in order to register.
// The bidirectional stream established between the node and the PoST node can be used
// to send challenges and receive proofs.
type PostService struct {
	log *zap.Logger

	clientMtx sync.Mutex
	client    map[types.NodeID]*postClient
}

type postCommand struct {
	req  *pb.NodeRequest
	resp chan<- *pb.ServiceResponse
}

// RegisterService registers this service with a grpc server instance.
func (s *PostService) RegisterService(server *grpc.Server) {
	pb.RegisterPostServiceServer(server, s)
}

func (s *PostService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterPostServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of this service.
func (s *PostService) String() string {
	return "PostService"
}

// NewPostService creates a new grpc service using config data.
func NewPostService(log *zap.Logger) *PostService {
	return &PostService{
		log: log,
	}
}

// Register is called by the PoST service to connect with the node.
// It creates a bidirectional stream that is kept open until either side closes it.
// The other functions on this service are called by services of the node to send
// requests to the PoST node and receive responses.
func (s *PostService) Register(stream pb.PostService_RegisterServer) error {
	con := make(chan postCommand)
	nodeId, err := s.setConnection(con)
	if err != nil {
		return err
	}
	defer s.dropConnection(nodeId)

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case cmd := <-con:
			if err := stream.SendMsg(cmd.req); err != nil {
				s.log.Error("failed to send request", zap.Error(err))
				continue
			}

			resp, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("failed to receive response: %w", err)
			}

			cmd.resp <- resp
		}
	}
}

func (s *PostService) setConnection(con chan postCommand) (types.NodeID, error) {
	s.clientMtx.Lock()
	defer s.clientMtx.Unlock()

	client := newPostClient(con)
	info, err := client.Info(context.Background())
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("failed to get node info: %w", err)
	}

	if _, ok := s.client[info.NodeID]; ok {
		return types.EmptyNodeID, fmt.Errorf("post service already registered")
	}

	s.log.Info("post service registered", zap.Stringer("node_id", info.NodeID))
	s.client[info.NodeID] = client
	return info.NodeID, nil
}

func (s *PostService) dropConnection(nodeId types.NodeID) error {
	s.clientMtx.Lock()
	defer s.clientMtx.Unlock()

	err := s.client[nodeId].Close()
	delete(s.client, nodeId)
	return err
}

func (s *PostService) Client(nodeId types.NodeID) (activation.PostClient, error) {
	s.clientMtx.Lock()
	defer s.clientMtx.Unlock()

	client, ok := s.client[nodeId]
	if !ok {
		return nil, fmt.Errorf("post service not registered")
	}

	return client, nil
}
