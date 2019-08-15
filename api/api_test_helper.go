package api

import "context"

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
