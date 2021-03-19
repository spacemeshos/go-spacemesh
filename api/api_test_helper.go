package api

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

// apiGossipProtocol is the gossip protocol name for the test api
// For purposes of p2p/gossip testing, we override the PoetProofProtocol and approve all
// poet messages regardless of content.
const apiGossipProtocol = activation.PoetProofProtocol

// Service is an interface for receiving messages via gossip
type Service interface {
	RegisterGossipProtocol(string, priorityq.Priority) chan service.GossipMessage
}

// ApproveAPIGossipMessages registers the gossip api test protocol and approves every message as valid
func ApproveAPIGossipMessages(ctx context.Context, s Service) {
	gm := s.RegisterGossipProtocol(apiGossipProtocol, priorityq.Low)
	go func() {
		for {
			select {
			case m := <-gm:
				msglen := len(m.Bytes())
				log.With().Info("api_test_gossip: got test gossip message", log.Int("len", msglen))
				m.ReportValidation(ctx, apiGossipProtocol)
			case <-ctx.Done():
				return
			}
		}
	}()
}
