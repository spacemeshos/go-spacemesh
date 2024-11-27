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
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/log"
	p2pmetrics "github.com/spacemeshos/go-spacemesh/p2p/metrics"
)

type PubSub interface {
	Register(topic string, handler GossipHandler, opts ...ValidatorOpt)
	Publish(ctx context.Context, topic string, msg []byte) error
	ProtocolPeers(protocol string) []peer.ID
}

type NullPubSub struct{}

var _ PubSub = &NullPubSub{}

// Register implements PubSub.
func (*NullPubSub) Register(
	topic string,
	handler func(context.Context, peer.ID, []byte) error,
	opts ...pubsub.ValidatorOpt,
) {
}

// Publish implements PubSub.
func (*NullPubSub) Publish(ctx context.Context, topic string, msg []byte) error {
	return nil
}

// ProtocolPeers implements PubSub.
func (*NullPubSub) ProtocolPeers(protocol string) []peer.ID {
	return nil
}

// GossipPubSub is a spacemesh-specific wrapper around gossip protocol.
type GossipPubSub struct {
	logger *zap.Logger
	pubsub *pubsub.PubSub
	host   host.Host

	mu     sync.RWMutex
	topics map[string]*pubsub.Topic
}

var _ PubSub = &GossipPubSub{}

// Register handler for topic.
func (ps *GossipPubSub) Register(topic string, handler GossipHandler, opts ...ValidatorOpt) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, exist := ps.topics[topic]; exist {
		ps.logger.Sugar().Panicf("already registered a topic %s", topic)
	}
	// Drop peers on ValidationRejectErr
	handler = DropPeerOnValidationReject(handler, ps.host, ps.logger)
	ps.pubsub.RegisterTopicValidator(
		topic,
		func(ctx context.Context, pid peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
			start := time.Now()
			err := handler(log.WithNewRequestID(ctx), pid, msg.Data)
			p2pmetrics.ProcessedMessagesDuration.WithLabelValues(topic, castResult(err)).
				Observe(float64(time.Since(start)))
			if err != nil {
				ps.logger.Debug("topic validation failed", zap.String("topic", topic), zap.Error(err))
			}
			switch {
			case errors.Is(err, ErrValidationReject):
				return pubsub.ValidationReject
			case err != nil:
				return pubsub.ValidationIgnore
			default:
				return pubsub.ValidationAccept
			}
		},
		opts...)
	topich, err := ps.pubsub.Join(topic)
	if err != nil {
		ps.logger.Panic("failed to join a topic", zap.String("topic", topic), zap.Error(err))
	}
	ps.topics[topic] = topich
	_, err = topich.Relay()
	if err != nil {
		ps.logger.Panic("failed to enable relay for topic", zap.String("topic", topic), zap.Error(err))
	}
}

// Publish message to the topic.
func (ps *GossipPubSub) Publish(ctx context.Context, topic string, msg []byte) error {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	topich := ps.topics[topic]
	if topich == nil {
		ps.logger.Sugar().Panicf("Publish is called before Register for topic %s", topic)
	}
	if err := topich.Publish(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish to topic %s: %w", topic, err)
	}
	return nil
}

// ProtocolPeers returns list of peers that are communicating in a given protocol.
func (ps *GossipPubSub) ProtocolPeers(protocol string) []peer.ID {
	return ps.pubsub.ListPeers(protocol)
}
