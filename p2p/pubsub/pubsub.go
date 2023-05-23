package pubsub

import (
	"context"
	"errors"
	"fmt"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	p2pmetrics "github.com/spacemeshos/go-spacemesh/p2p/metrics"
)

func init() {
	pubsub.GossipSubD = 6
	pubsub.GossipSubDscore = 4
	pubsub.GossipSubDout = 3
	pubsub.GossipSubDlo = 4
	pubsub.GossipSubDhi = 8
	pubsub.GossipSubDlazy = 8
	pubsub.GossipSubDirectConnectInitialDelay = 30 * time.Second
	pubsub.GossipSubIWantFollowupTime = 5 * time.Second
	pubsub.GossipSubHistoryLength = 10
	pubsub.GossipSubGossipFactor = 0.1
}

const (
	// score thresholds.
	// see details https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md#score-thresholds

	// GossipScoreThreshold when a peer's score drops below this threshold, no gossip is emitted towards that peer
	// and gossip from that peer is ignored.
	GossipScoreThreshold = -500
	// PublishScoreThreshold when a peer's score drops below this threshold, self published messages are not propagated
	// towards this peer when (flood) publishing.
	PublishScoreThreshold = -1000
	// GraylistScoreThreshold when a peer's score drops below this threshold, the peer is graylisted and its RPCs are ignored.
	GraylistScoreThreshold = -2500
	// AcceptPXScoreThreshold when a peer sends us PX information with a prune, we only accept it and connect to the
	// supplied peers if the originating peer's score exceeds this threshold.
	AcceptPXScoreThreshold = 1000
	// OpportunisticGraftScoreThreshold when the median peer score in the mesh drops below this value, the router
	// may select more peers with score above the median to opportunistically graft on the mesh.
	OpportunisticGraftScoreThreshold = 3.5

	// AtxProtocol is the protocol id for ATXs.
	AtxProtocol = "ax1"
	// ProposalProtocol is the protocol id for block proposals.
	ProposalProtocol = "pp1"
	// TxProtocol iis the protocol id for transactions.
	TxProtocol = "tx1"

	// HareProtocol is the protocol id for hare messages.
	HareProtocol = "hr1"

	// BlockCertify is the protocol id for block certification.
	BlockCertify = "bc1"

	// BeaconProtocol is used currently only for recording metrics, but
	// potentially will become used as an actual protocol if we decide to merge
	// the beacon protocols.
	// https://github.com/spacemeshos/go-spacemesh/issues/4207
	BeaconProtocol = "b1"
	// BeaconWeakCoinProtocol is the protocol id for beacon weak coin.
	BeaconWeakCoinProtocol = "bw1"
	// BeaconProposalProtocol is the protocol id for beacon proposals.
	BeaconProposalProtocol = "bp1"
	// BeaconFirstVotesProtocol is the protocol id for beacon first vote.
	BeaconFirstVotesProtocol = "bf1"
	// BeaconFollowingVotesProtocol is the protocol id for beacon following votes.
	BeaconFollowingVotesProtocol = "bo1"

	MalfeasanceProof = "mp1"
)

// DefaultConfig for PubSub.
func DefaultConfig() Config {
	return Config{Flood: true}
}

// Config for PubSub.
type Config struct {
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
		host:   h,
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

// GossipHandler is a function that is for receiving p2p messages.
type GossipHandler = func(context.Context, peer.ID, []byte) error

// ErrValidationReject is returned by a GossipHandler to indicate that the
// pubsub validation result is ValidationReject. ValidationAccept is indicated
// by a nil error and ValidationIgnore is indicated by any error that is not a
// ErrValidationReject.
var ErrValidationReject = errors.New("validation reject")

// ChainGossipHandler helper to chain multiple GossipHandler together. Called synchronously and in the order.
func ChainGossipHandler(handlers ...GossipHandler) GossipHandler {
	return func(ctx context.Context, pid peer.ID, msg []byte) error {
		for _, h := range handlers {
			if err := h(ctx, pid, msg); err != nil {
				return err
			}
		}
		return nil
	}
}

// DropPeerValidationReject wraps a gossip handler to provide a handler that drops a
// peer if the wrapped handler returns ErrValidationReject.
func DropPeerOnValidationReject(handler GossipHandler, h host.Host, logger log.Log) GossipHandler {
	return func(ctx context.Context, peer peer.ID, data []byte) error {
		err := handler(ctx, peer, data)
		if errors.Is(err, ErrValidationReject) {
			p2pmetrics.DroppedConnectionsValidationReject.Inc()
			err := h.Network().ClosePeer(peer)
			if err != nil {
				logger.With().Debug("failed to close peer",
					log.String("peer", peer.ShortString()),
					log.Err(err),
				)
			}
		}
		return err
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
					//       based on observed behavior
					return 0
				},
				AppSpecificWeight: 1,

				// TODO: consider setting IP co-location threshold before applying penalties

				// P7: behavioral penalties, decay after 1hr
				BehaviourPenaltyThreshold: 6,
				BehaviourPenaltyWeight:    -10,
				BehaviourPenaltyDecay:     pubsub.ScoreParameterDecay(time.Hour),

				DecayInterval: pubsub.DefaultDecayInterval,
				DecayToZero:   pubsub.DefaultDecayToZero,

				// this retains non-positive scores for 6 hours
				RetainScore: 6 * time.Hour,

				Topics: map[string]*pubsub.TopicScoreParams{
					AtxProtocol:      defaultTopicParam(),
					ProposalProtocol: defaultTopicParam(),
				},
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

func defaultTopicParam() *pubsub.TopicScoreParams {
	return &pubsub.TopicScoreParams{
		TopicWeight: 0.1, // max cap is 50, max mesh penalty is -10, single invalid message is -100

		// 1 tick per second, maxes at 1 after 1 hour
		TimeInMeshWeight:  0.00027, // ~1/3600
		TimeInMeshQuantum: time.Second,
		TimeInMeshCap:     1,

		// deliveries decay after 1 hour, cap at 1000
		FirstMessageDeliveriesWeight: 5, // max value is 500
		FirstMessageDeliveriesDecay:  pubsub.ScoreParameterDecay(time.Hour),
		FirstMessageDeliveriesCap:    100,

		// TODO: consider mesh delivery failure when the network grows and traffic becomes significant

		// invalid messages decay after 1 hour
		InvalidMessageDeliveriesWeight: -1000,
		InvalidMessageDeliveriesDecay:  pubsub.ScoreParameterDecay(time.Hour),
	}
}

func castResult(err error) string {
	switch {
	case err == nil:
		return "accept"
	case errors.Is(err, ErrValidationReject):
		return "reject"
	default:
		return "ignore"
	}
}
