package grpc

import (
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spacemeshos/poet/broadcaster/pb"
)

// GetStateRoot returns current state root
func (s NodeService) GetStateRoot(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetStateRoot msg")
	return &pb.SimpleMessage{Value: s.Tx.GetStateRoot().String()}, nil
}
