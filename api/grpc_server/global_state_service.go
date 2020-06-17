package grpc_server

import (
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/poet/broadcaster/pb"
	"golang.org/x/net/context"
)

// GetStateRoot returns current state root
func (s NodeService) GetStateRoot(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetStateRoot msg")
	return &pb.SimpleMessage{Value: s.Tx.GetStateRoot().String()}, nil
}
