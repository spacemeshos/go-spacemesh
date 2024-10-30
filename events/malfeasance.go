package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// EventMalfeasance includes the malfeasance proof.
type EventMalfeasance struct {
	Smesher types.NodeID
	Proof   []byte
}

// SubscribeMalfeasance subscribes malfeasance events.
func SubscribeMalfeasance() Subscription {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(EventMalfeasance))
		if err != nil {
			log.With().Panic("Failed to subscribe to malfeasance proof")
		}
		return sub
	}
	return nil
}

// ReportMalfeasance reports a malfeasance proof.
func ReportMalfeasance(nodeID types.NodeID, proof []byte) {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		if err := reporter.malfeasanceEmitter.Emit(EventMalfeasance{Smesher: nodeID, Proof: proof}); err != nil {
			log.With().Error("failed to emit malfeasance proof", log.Err(err))
		}
	}
}
