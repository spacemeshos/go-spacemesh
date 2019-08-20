package api

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"sync"
)

const APIGossipProtocol = "api_test_gossip"

// ApproveAPIGossipMessages registers the gossip api test protocol and approves every message as valid
func ApproveAPIGossipMessages(ctx context.Context, s Service) {
	gm := s.RegisterGossipProtocol(APIGossipProtocol)
	go func() {
		for {
			select {
			case m := <-gm:
				m.ReportValidation(APIGossipProtocol)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// P2P TESTS API

var randMtx sync.Mutex
var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandString(n int) string {
	randMtx.Lock()
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	randMtx.Unlock()
	return string(b)
}

func (s SpacemeshGrpcService) Broadcast(ctx context.Context, in *pb.BroadcastMessage) (*pb.SimpleMessage, error) {
	log.Info("GRPC Broadcast msg")
	if in.Data == "nil" {
		in.Data = RandString(int(in.Size))
	}
	log.Info("Sending message of size %v ", len([]byte(in.Data)))
	err := s.Network.Broadcast(APIGossipProtocol, []byte(in.Data))
	if err != nil {
		log.Warning("RPC Broadcast failed please check that `test-mode` is on in order to use RPC Broadcast.")
		return &pb.SimpleMessage{Value: err.Error()}, err
	}
	log.Info("GRPC Broadcast msg ok")
	return &pb.SimpleMessage{Value: "ok"}, nil
}
