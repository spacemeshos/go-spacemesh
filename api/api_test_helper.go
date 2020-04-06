package api

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

// apiGossipProtocol is the gossip protocol name for the test api
const apiGossipProtocol = "api_test_gossip"

// ApproveAPIGossipMessages registers the gossip api test protocol and approves every message as valid
func ApproveAPIGossipMessages(ctx context.Context, s Service) {
	gm := s.RegisterGossipProtocol(apiGossipProtocol, priorityq.Low)
	go func() {
		for {
			select {
			case m := <-gm:
				_input := string(m.Bytes())
				if _input == "" {
					log.Warning("api_test_gossip: got an empty message")
					continue
				}
				m.ReportValidation(apiGossipProtocol)
			case <-ctx.Done():
				return
			}
		}
	}()
}
