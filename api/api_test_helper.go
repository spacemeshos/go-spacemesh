package api

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

// const for test api gossip proto
const APIGossipProtocol = "api_test_gossip"

// ApproveAPIGossipMessages registers the gossip api test protocol and approves every message as valid
func ApproveAPIGossipMessages(ctx context.Context, s Service) {
	gm := s.RegisterGossipProtocol(APIGossipProtocol, priorityq.Low)
	go func() {
		for {
			select {
			case m := <-gm:
				_input := string(m.Bytes())
				if _input == "" {
					log.Warning("api_test_gossip: got an empty message")
					continue
				}
				m.ReportValidation(APIGossipProtocol)
			case <-ctx.Done():
				return
			}
		}
	}()
}
