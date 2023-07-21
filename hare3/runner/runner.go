package runner

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare"
	heligibility "github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/broker"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3/leader"
	"github.com/spacemeshos/go-spacemesh/hare3/weakcoin"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/syncer"
)

// WeakCoinResult is used to communicate the result of the weak coin back to
// the application, via a channel.
type WeakCoinResult struct {
	Layer types.LayerID
	// A nil coin value means that no coins were received.
	Coin *bool
}

// Protocol runner runs an instance of the hare3 protocol.
type ProtocolRunner struct {
	clock        RoundClock
	protocol     hare3.Protocol
	coinChooser  *weakcoin.Chooser
	maxRound     hare3.AbsRound
	gossiper     NetworkGossiper
	layer        types.LayerID
	ac           *eligibility.ActiveCheck
	mb           *MessageBuilder
	weakcoinChan chan<- WeakCoinResult
	l            log.Log
}

func NewProtocolRunner(
	clock RoundClock,
	protocol *hare3.Protocol,
	coinChooser *weakcoin.Chooser,
	iterationLimit int8,
	gossiper NetworkGossiper,
	layer types.LayerID,
	ac *eligibility.ActiveCheck,
	mb *MessageBuilder,
	weakcoinChan chan<- WeakCoinResult,
	l log.Log,
) *ProtocolRunner {
	return &ProtocolRunner{
		clock:        clock,
		protocol:     *protocol,
		coinChooser:  coinChooser,
		maxRound:     hare3.NewAbsRound(iterationLimit, 0),
		gossiper:     gossiper,
		layer:        layer,
		ac:           ac,
		mb:           mb,
		weakcoinChan: weakcoinChan,
		l:            l,
	}
}

// Run runs the protocol until it terminates and returns the result. If the
// protocol exceeded the iteration limit or the context expires an error will be
// returned and the result will be nil.
func (r *ProtocolRunner) Run(ctx context.Context) ([]types.Hash20, error) {
	// ok want to actually use a lock here
	for {
		// Note the round starts at -2 since the hare protocol increments the round as the first step of NextRound
		previousRound := r.protocol.Round()
		if previousRound == r.maxRound {
			return nil, fmt.Errorf("hare protocol runner exceeded iteration limit of %d", r.maxRound.Iteration())
		}
		// The weak coin is calculated from pre-round messages, we select the
		// coin after one round, we are not concerned about late messages
		// affecting the outcome since the coin is weak (see package doc for
		// hare3/weakcoin for an explanation of weak)
		coin := r.coinChooser.Choose()
		if previousRound == hare3.Preround.Round()+1 && coin != nil {
			r.weakcoinChan <- WeakCoinResult{
				Layer: r.layer,
				Coin:  coin,
			}
		}

		r.l.Info("awaiting beginning of round %d", previousRound+1)
		select {
		// We await the beginning of the next round, which is achieved by calling AwaitEndOfRound with the previous round.
		case <-r.clock.AwaitEndOfRound(uint32(previousRound)):
			// TODO fix this eligibility should not return an error
			proof, count, err := r.ac.Eligibility(ctx, previousRound)
			if err != nil {
				return nil, err
			}
			r.l.Info("running round %d, of layer %d, eligibility count %d", previousRound+1, r.layer, count)
			toSend, output := r.protocol.NextRound(true)
			if toSend != nil {
				r.l.Info("sending hare message round: %v, proposals: %v", toSend.Round, toSend.Values)
				r.gossiper.Gossip(ctx, r.mb.BuildEncodedMessage(toSend, proof, count))
			}
			if output != nil {
				return output, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// RoundClock is a timer interface.
type RoundClock interface {
	AwaitWakeup() <-chan struct{}
	// RoundEnd returns the time at which round ends, passing round-1 will
	// return the time at which round starts.
	RoundEnd(round uint32) time.Time
	AwaitEndOfRound(round uint32) <-chan struct{}
}

type LayerClock interface {
	LayerToTime(types.LayerID) time.Time
	AwaitLayer(types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
}

type HareRunner struct {
	clock           LayerClock
	gossiper        NetworkGossiper
	signer          *signing.EdSigner
	oracle          *heligibility.Oracle
	syncer          *syncer.Syncer
	beaconRetriever *beacon.ProtocolDriver
	b               *broker.Broker
	l               log.Log
	db              *datastore.CachedDB
	output          chan hare.LayerOutput
	weakcoinChan    chan<- WeakCoinResult
	eg              errgroup.Group

	effectiveGenesis types.LayerID
	maxIterations    int8
	wakeupDelta      time.Duration
	roundDuration    time.Duration
	committeeSize    int
	expectedLeaders  int
	nodeID           types.NodeID
}

func NewHareRunner(clock LayerClock,
	gossiper NetworkGossiper,
	signer *signing.EdSigner,
	oracle *heligibility.Oracle,
	syncer *syncer.Syncer,
	beaconRetriever *beacon.ProtocolDriver,
	b *broker.Broker,
	l log.Log,
	db *datastore.CachedDB,
	output chan hare.LayerOutput,
	weakcoinChan chan<- WeakCoinResult,
	effectiveGenesis types.LayerID,
	maxIterations int8,
	wakeupDelta time.Duration,
	roundDuration time.Duration,
	committeeSize int,
	expectedLeaders int,
	nodeID types.NodeID,
) *HareRunner {
	return &HareRunner{
		clock:            clock,
		gossiper:         gossiper,
		signer:           signer,
		oracle:           oracle,
		syncer:           syncer,
		beaconRetriever:  beaconRetriever,
		b:                b,
		l:                l,
		db:               db,
		output:           output,
		weakcoinChan:     weakcoinChan,
		effectiveGenesis: effectiveGenesis,
		maxIterations:    maxIterations,
		wakeupDelta:      wakeupDelta,
		roundDuration:    roundDuration,
		committeeSize:    committeeSize,
		expectedLeaders:  expectedLeaders,
		nodeID:           nodeID,
	}
}

func (r *HareRunner) Run(ctx context.Context) {
	// Ensure that we wait for any child routines to complete before returning.
	defer r.eg.Wait()
	currentLayer := r.clock.CurrentLayer()
	var startLayer types.LayerID
	if currentLayer < r.effectiveGenesis {
		startLayer = r.effectiveGenesis + 1
		toWait := time.Until(r.clock.LayerToTime(startLayer))
		r.l.WithContext(ctx).With().Info(
			"hare runner awaiting start layer",
			log.Stringer("time_to_wait", toWait),
			log.Stringer("current_layer", currentLayer),
			log.Stringer("effective_genesis", r.effectiveGenesis),
			log.Stringer("start_layer", r.effectiveGenesis))
	} else {
		// We wait for the subsequent layer so that we can be sure of being on time.
		startLayer = currentLayer + 1
		toWait := time.Until(r.clock.LayerToTime(startLayer))
		r.l.WithContext(ctx).With().Info(
			"hare runner awaiting start layer",
			log.Stringer("time_to_wait", toWait),
			log.Stringer("current_layer", currentLayer),
			log.Stringer("start_layer", startLayer))
	}

	for l := startLayer; ; l += 1 {
		r.l.WithContext(ctx).With().Debug("hare runner starting layer", log.Stringer("current_layer", l))

		// copy the loop variable, it's used in a go-routine below.
		layer := l
		select {
		case <-r.clock.AwaitLayer(layer):
			if !r.syncer.IsSynced(ctx) {
				// if not currently synced don't start consensus process.
				r.l.With().Info("not starting hare: node not synced",
					log.Context(ctx),
					layer)
				continue
			}

			// We construct a proof to see if we are eligible, for each round.
			// we will need the eligibility oracle at least.

			// It seems like it could be possible for the beacon to be synced
			// slowly as it can be reconstructed piecemeal from smeshers
			// ballots. Well in fact ReportBeaconFromBallot takes ballots from
			// any layer in the epoch and then adds up the weight for that
			// ballot value, and when the threshold is crossed we consider the
			// beacon decided. But the actual value of the beacon comes from
			// the first ballot (the reference ballot) and so we can end up
			// with multiple ballots from the same smesher pushing their
			// beacon value over the threshold. It seems what we really want is
			// to get all the ref ballots and take the majority there (by
			// storing them), oh we can't get all the ref ballots because they
			// don't actually exist at the beginning of the epoch, you only get
			// 50 per layer, but we could store all the final messages from
			// beacon.
			// TODO ask in chat.
			//
			// Also I thought that the beacon protocol did not take into
			// account weight, so it seems strange to base the syncing around
			// weight. Actually I think this is wrong, votes in beacon are
			// weighted.
			//
			// So we will just try every time, this could be improved at least
			// in logging to change the message if this is really taking too
			// long, with the default settings it shouldn't take longer than 16
			// Layers to sync. Really we want to be notified of the beacon
			// arriving.
			beacon, err := r.beaconRetriever.GetBeacon(layer.GetEpoch())
			if err != nil {
				r.l.With().Info("no beacon for epoch",
					log.Context(ctx),
					layer,
				)
				continue
			}
			props := goodProposals(ctx, r.l, r.db, r.nodeID, layer, beacon)

			actives, err := r.oracle.ActiveMap(ctx, layer)
			if err != nil {
				r.l.With().Info("no active set for epoch",
					log.Context(ctx),
					layer,
				)
				continue
			}

			lc := leader.NewDefaultLeaderChecker(r.oracle, r.expectedLeaders, actives, layer)

			// Execute layer
			r.eg.Go(func() error {
				result, err := r.runLayer(log.WithNewSessionID(ctx), layer, props, lc)
				// We log the error, which is either ctx timeout or iteration exceeded
				if err != nil {
					r.l.With().Info("hare terminated without agreement", layer, log.Err(err))
				} else {
					select {
					// Send the result
					case r.output <- hare.LayerOutput{
						Ctx:       ctx,
						Layer:     layer,
						Proposals: result,
					}:
					case <-ctx.Done():
						return nil
					}
				}
				return nil
			})
		case <-ctx.Done():
			return
		}
	}
}

// runLayer constructs a ProtocolRunner and returns the output of its Run method.
func (r *HareRunner) runLayer(ctx context.Context, layer types.LayerID, props []types.ProposalID, lc hare3.LeaderChecker) ([]types.ProposalID, error) {
	// The broker may have already created a handler and coinChooser in
	// response to early messages, that's why we get them from the broker.
	handler, coinChooser := r.b.Register(ctx, layer)
	roundClock := hare.NewSimpleRoundClock(r.clock.LayerToTime(layer), r.wakeupDelta, r.roundDuration)

	initialSet := make([]types.Hash20, len(props))
	for i := 0; i < len(props); i++ {
		initialSet[i] = types.Hash20(props[i])
	}
	ac := eligibility.NewActiveCheck(layer, r.nodeID, r.committeeSize, r.oracle, r.l)
	protocolRunner := NewProtocolRunner(roundClock, handler.Protocol(lc, initialSet), coinChooser, r.maxIterations, r.gossiper, layer, ac, NewMessageBuilder(layer, r.nodeID, r.signer), r.weakcoinChan, r.l)
	v, err := protocolRunner.Run(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]types.ProposalID, len(v))
	for i := 0; i < len(v); i++ {
		result[i] = types.ProposalID(v[i])
	}
	return result, nil
}

type NetworkGossiper interface {
	Gossip(ctx context.Context, msg []byte)
}

func NewDefaultGossiper(p pubsub.Publisher, l log.Log) *DefaultGossiper {
	return &DefaultGossiper{
		p: p,
		l: l,
	}
}

type DefaultGossiper struct {
	p pubsub.Publisher
	l log.Log
}

func (g *DefaultGossiper) Gossip(ctx context.Context, msg []byte) {
	err := g.p.Publish(ctx, pubsub.HareProtocol, msg)
	g.l.With().Error("error while publishing hare message", log.Err(err))
}

type MessageBuilder struct {
	layer  types.LayerID
	nodeID types.NodeID
	signer *signing.EdSigner
}

func NewMessageBuilder(layer types.LayerID, nodeID types.NodeID, signer *signing.EdSigner) *MessageBuilder {
	return &MessageBuilder{layer: layer, nodeID: nodeID, signer: signer}
}

func (b *MessageBuilder) BuildEncodedMessage(m *hare3.OutputMessage, proof types.VrfSignature, eligibilityCount uint16) []byte {
	values := make([]types.ProposalID, len(m.Values))
	for i := range m.Values {
		values[i] = types.ProposalID(m.Values[i])
	}
	msg := hare.Message{
		InnerMessage: &hare.InnerMessage{
			Layer:  b.layer,
			Round:  uint32(m.Round),
			Values: values,
		},
		SmesherID: b.nodeID,
		Eligibility: types.HareEligibility{
			Proof: proof,
			Count: eligibilityCount,
		},
	}

	msg.Signature = b.signer.Sign(signing.HARE, msg.SignedBytes())

	bytes, err := codec.Encode(&msg)
	if err != nil {
		panic(err.Error())
	}

	return bytes
}
