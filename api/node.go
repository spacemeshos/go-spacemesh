package api

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"math/big"
)

const APIGossipProtocol = "api_test_gossip"

type Service interface {
	RegisterGossipProtocol(string) chan service.GossipMessage
}

// ApproveAPIGossipMessages registers the gossip api test protocol and approves every message as valid
func ApproveAPIGossipMessages(ctx context.Context, s Service) {
	gm := s.RegisterGossipProtocol(APIGossipProtocol)
	go func() {
		for {
			select {
			case m := <-gm:
				m.ReportValidation(APIGossipProtocol, true)
			case <-ctx.Done():
				return
			}
		}
	}()
}

type StateAPI interface {
	GetBalance(address address.Address) *big.Int

	GetNonce(address address.Address) uint64

	Exist(address address.Address) bool
}

type NetworkAPI interface {
	Broadcast(channel string, data []byte) error
}
