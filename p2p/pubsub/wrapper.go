package pubsub

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
)

// PubSub is a spacemesh-specific wrapper around gossip protocol.
type PubSub struct {
	logger log.Log
	pubsub *pubsub.PubSub
	host   host.Host

	mu     sync.RWMutex
	topics map[string]*pubsub.Topic
}

// Register handler for topic.
func (ps *PubSub) Register(topic string, handler GossipHandler) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, exist := ps.topics[topic]; exist {
		ps.logger.Panic("already registered a topic %s", topic)
	}
	// Drop peers on ValidationRejectErr
	handler = DropPeerOnValidationReject(handler, ps.host, ps.logger)
	ps.pubsub.RegisterTopicValidator(topic, func(ctx context.Context, pid peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		start := time.Now()
		err := handler(log.WithNewRequestID(ctx), pid, msg.Data)
		metrics.ProcessedMessagesDuration.WithLabelValues(topic, castResult(err)).
			Observe(float64(time.Since(start)))
		switch {
		case errors.Is(err, ErrValidationReject):
			return pubsub.ValidationReject
		case err != nil:
			return pubsub.ValidationIgnore
		default:
			return pubsub.ValidationAccept
		}
	})
	topich, err := ps.pubsub.Join(topic)
	if err != nil {
		ps.logger.With().Panic("failed to join a topic", log.String("topic", topic), log.Err(err))
	}
	ps.topics[topic] = topich
	_, err = topich.Relay()
	if err != nil {
		ps.logger.With().Panic("failed to enable relay for topic",
			log.String("topic", topic),
			log.Err(err),
		)
	}
}

// Publish message to the topic.
func (ps *PubSub) Publish(ctx context.Context, topic string, msg []byte) error {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	topich := ps.topics[topic]
	if topich == nil {
		ps.logger.Panic("Publish is called before Register for topic %s", topic)
	}
	if err := topich.Publish(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish to topic %v: %w", topic, err)
	}
	return nil
}

// ProtocolPeers returns list of peers that are communicating in a given protocol.
func (ps *PubSub) ProtocolPeers(protocol string) []peer.ID {
	return ps.pubsub.ListPeers(protocol)
}
