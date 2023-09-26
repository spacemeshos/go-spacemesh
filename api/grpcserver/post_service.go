package grpcserver

import (
	"context"
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

	// connections is a map of IDs -> channel of commands for every connected node.
	// the node calls functions on this service which build API requests for every
	// connected node and send them on the channel. The PoST node receives the requests
	// and sends responses back on the response channel of the command.
	conMtx sync.Mutex
	con    chan postCommand
	client *postClient
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
	ch := make(chan postCommand)
	s.setConnection(ch)
	defer s.dropConnection()

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case cmd := <-ch:
			if err := stream.SendMsg(cmd.req); err != nil {
				s.log.Error("failed to send request", zap.Error(err))
				continue
			}

			resp, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("failed to receive response: %w", err)
			}

			cmd.ch <- resp
		}
	}
}

func (s *PostService) setConnection(ch chan postCommand) error {
	s.conMtx.Lock()
	defer s.conMtx.Unlock()

	if s.con != nil {
		return fmt.Errorf("connection already established")
	}

	s.con = ch
	s.client = newPostClient(s)
	s.log.Info("post service registered")

	for _, cb := range s.callbacks {
		cb.Connected(s.client)
	}

	return nil
}

func (s *PostService) Connected() bool {
	s.conMtx.Lock()
	defer s.conMtx.Unlock()

	return s.con != nil
}

func (s *PostService) dropConnection() error {
	s.conMtx.Lock()
	defer s.conMtx.Unlock()

	if s.con == nil {
		return fmt.Errorf("connection not established")
	}

	close(s.con)
	s.con = nil
	s.client.Close()

	for _, cb := range s.callbacks {
		cb.Disconnected(s.client)
	}

	s.client = nil
	return nil
}

func (s *PostService) Send(ctx context.Context, cmd postCommand) error {
	s.conMtx.Lock()
	defer s.conMtx.Unlock()

	if s.con == nil {
		return fmt.Errorf("connection not established")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.con <- cmd:
		return nil
	}
}

type postCommand struct {
	req *pb.NodeRequest
	ch  chan<- *pb.ServiceResponse
}
