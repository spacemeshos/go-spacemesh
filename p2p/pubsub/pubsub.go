package pubsub

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"

	"github.com/spacemeshos/go-spacemesh/log"
	p2pmetrics "github.com/spacemeshos/go-spacemesh/p2p/metrics"
)

// DefaultConfig for PubSub.
func DefaultConfig() Config {
	return Config{Flood: true}
}

// Config for PubSub.
type Config struct {
	// TODO(dshulyak) change it to NoFlood
	Flood          bool
	MaxMessageSize int
}

// New creates PubSub instance.
func New(ctx context.Context, logger log.Log, h host.Host, cfg Config) (*PubSub, error) {
	// TODO(dshulyak) refactor code to accept options
	opts := []pubsub.Option{
		pubsub.WithFloodPublish(cfg.Flood),
		pubsub.WithMessageIdFn(msgID),
		pubsub.WithNoAuthor(),
		pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		pubsub.WithPeerOutboundQueueSize(8192),
		pubsub.WithValidateQueueSize(8192),
		pubsub.WithRawTracer(p2pmetrics.NewGoSIPCollector()),
	}
	if cfg.MaxMessageSize != 0 {
		opts = append(opts, pubsub.WithMaxMessageSize(cfg.MaxMessageSize))
	}
	ps, err := pubsub.NewGossipSub(ctx, h, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize gossipsub instance: %w", err)
	}
	return &PubSub{
		logger: logger,
		pubsub: ps,
		topics: map[string]*pubsub.Topic{},
	}, nil
}

//go:generate mockgen -package=mocks -destination=./mocks/publisher.go -source=./pubsub.go

// Publisher interface for publishing messages.
type Publisher interface {
	Publish(context.Context, string, []byte) error
}

// Subscriber is an interface for subcribing to messages.
type Subscriber interface {
	Register(string, GossipHandler)
}

// PublishSubsciber common interface for publisher and subscribing.
type PublishSubsciber interface {
	Publisher
	Subscriber
}

// GossipHandler is a function that is for receiving messages.
type GossipHandler = func(context.Context, peer.ID, []byte) ValidationResult

// ValidationResult is a one of the validation result constants.
type ValidationResult = pubsub.ValidationResult

const (
	// ValidationAccept should be returned if message is good and can be broadcasted.
	ValidationAccept = pubsub.ValidationAccept
	// ValidationIgnore should be returned if message might be good, but outdated
	// and shouldn't be broadcasted.
	ValidationIgnore = pubsub.ValidationIgnore
	// ValidationReject should be returned if message is malformed or malicious
	// and shouldn't be broadcasted. Peer might be potentially get banned when on this result.
	ValidationReject = pubsub.ValidationReject
)

// ChainGossipHandler helper to chain multiple GossipHandler together. Called synchronously and in the order.
func ChainGossipHandler(handlers ...GossipHandler) GossipHandler {
	return func(ctx context.Context, pid peer.ID, msg []byte) ValidationResult {
		for _, h := range handlers {
			if rst := h(ctx, pid, msg); rst != pubsub.ValidationAccept {
				return rst
			}
		}
		return pubsub.ValidationAccept
	}
}

func msgID(msg *pb.Message) string {
	hasher := sha256.New()
	if msg.Topic != nil {
		hasher.Write([]byte(*msg.Topic))
	}
	hasher.Write(msg.Data)
	return string(hasher.Sum(nil))
}
