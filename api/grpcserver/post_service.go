package grpcserver

import (
	"fmt"
	"sync"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
)

// PostService is a grpc server that PoST nodes can connect to in order to register.
// The bidirectional stream established between the node and the PoST node can be used
// to send challenges and receive proofs.
type PostService struct {
	log       *zap.Logger
	callbacks []postConnectionListener

	clientMtx sync.Mutex
	client    *postClient
}

// RegisterService registers this service with a grpc server instance.
func (s *PostService) RegisterService(server *Server) {
	pb.RegisterPostServiceServer(server.GrpcServer, s)
}

// NewPostService creates a new grpc service using config data.
func NewPostService(log *zap.Logger, callbacks ...postConnectionListener) *PostService {
	return &PostService{
		log:       log,
		callbacks: callbacks,
	}
}

// Register is called by the PoST service to connect with the node.
// It creates a bidirectional stream that is kept open until either side closes it.
// The other functions on this service are called by services of the node to send
// requests to the PoST node and receive responses.
func (s *PostService) Register(stream pb.PostService_RegisterServer) error {
	con := make(chan postCommand)
	s.setConnection(con)
	defer s.dropConnection()

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

func (s *PostService) setConnection(con chan postCommand) error {
	s.clientMtx.Lock()
	defer s.clientMtx.Unlock()

	if s.client != nil {
		return fmt.Errorf("post service already registered")
	}

	s.client = newPostClient(con)
	s.log.Info("post service registered")

	for _, cb := range s.callbacks {
		cb.Connected(s.client)
	}

	return nil
}

func (s *PostService) dropConnection() error {
	s.clientMtx.Lock()

	err := s.client.Close()
	c := s.client
	s.client = nil

	s.clientMtx.Unlock()

	for _, cb := range s.callbacks {
		cb.Disconnected(c)
	}

	return err
}

type postCommand struct {
	req  *pb.NodeRequest
	resp chan<- *pb.ServiceResponse
}
