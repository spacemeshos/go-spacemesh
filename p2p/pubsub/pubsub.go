package pubsub

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"

	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	p2pmetrics "github.com/spacemeshos/go-spacemesh/p2p/metrics"
)

func init() {
	// configure larger overlay parameters
	pubsub.GossipSubD = 8
	pubsub.GossipSubDscore = 6
	pubsub.GossipSubDout = 3
	pubsub.GossipSubDlo = 6
	pubsub.GossipSubDhi = 12
	pubsub.GossipSubDlazy = 12
	pubsub.GossipSubDirectConnectInitialDelay = 30 * time.Second
	pubsub.GossipSubIWantFollowupTime = 5 * time.Second
	pubsub.GossipSubHistoryLength = 10
	pubsub.GossipSubGossipFactor = 0.1
}

const (
	GossipScoreThreshold             = -500
	PublishScoreThreshold            = -1000
	GraylistScoreThreshold           = -2500
	AcceptPXScoreThreshold           = 1000
	OpportunisticGraftScoreThreshold = 3.5
)

// DefaultConfig for PubSub.
func DefaultConfig() Config {
	return Config{Flood: true}
}

// Config for PubSub.
type Config struct {
	// TODO(dshulyak) change it to NoFlood
	Flood          bool
	IsBootnode     bool
	MaxMessageSize int
}

// New creates PubSub instance.
func New(ctx context.Context, logger log.Log, h host.Host, cfg Config) (*PubSub, error) {
	// TODO(dshulyak) refactor code to accept options
	opts := getOptions(cfg)
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
	hasher := hash.New()
	if msg.Topic != nil {
		hasher.Write([]byte(*msg.Topic))
	}
	hasher.Write(msg.Data)
	return string(hasher.Sum(nil))
}

func getOptions(cfg Config) []pubsub.Option {
	options := []pubsub.Option{
		// Gossipsubv1.1 configuration
		pubsub.WithFloodPublish(cfg.Flood),
		pubsub.WithMessageIdFn(msgID),
		pubsub.WithNoAuthor(),
		pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		pubsub.WithPeerOutboundQueueSize(8192),
		pubsub.WithValidateQueueSize(8192),
		pubsub.WithRawTracer(p2pmetrics.NewGoSIPCollector()),
		pubsub.WithPeerScore(
			&pubsub.PeerScoreParams{
				AppSpecificScore: func(p peer.ID) float64 {
					// TODO: add application specific score to provide feedback to the pubsub system
					//       based on observed behaviour
					return 0
				},
				AppSpecificWeight: 1,

				// TODO: consider setting IP co-location threshold before applying penalties

				// P7: behavioural penalties, decay after 1hr
				BehaviourPenaltyThreshold: 6,
				BehaviourPenaltyWeight:    -10,
				BehaviourPenaltyDecay:     pubsub.ScoreParameterDecay(time.Hour),

				DecayInterval: pubsub.DefaultDecayInterval,
				DecayToZero:   pubsub.DefaultDecayToZero,

				// this retains non-positive scores for 6 hours
				RetainScore: 6 * time.Hour,

				// TODO: add TopicScoreParams
			},
			&pubsub.PeerScoreThresholds{
				GossipThreshold:             GossipScoreThreshold,
				PublishThreshold:            PublishScoreThreshold,
				GraylistThreshold:           GraylistScoreThreshold,
				AcceptPXThreshold:           AcceptPXScoreThreshold,
				OpportunisticGraftThreshold: OpportunisticGraftScoreThreshold,
			},
		),
		// TODO: add peer scoring debugging with WithPeerScoreInspect
	}

	if cfg.MaxMessageSize != 0 {
		options = append(options, pubsub.WithMaxMessageSize(cfg.MaxMessageSize))
	}

	// enable Peer eXchange on bootstrappers
	if cfg.IsBootnode {
		// turn off the mesh for bootnodes -- only do gossip and PX
		pubsub.GossipSubD = 0
		pubsub.GossipSubDscore = 0
		pubsub.GossipSubDlo = 0
		pubsub.GossipSubDhi = 0
		pubsub.GossipSubDout = 0
		pubsub.GossipSubDlazy = 64
		pubsub.GossipSubGossipFactor = 0.25
		pubsub.GossipSubPruneBackoff = 5 * time.Minute
		// turn on PX
		options = append(options, pubsub.WithPeerExchange(true))
	}
	return options
}
