package runner

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/broker"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"golang.org/x/sync/errgroup"
)

type ProtocolRunner struct {
	clock    RoundClock
	protocol hare3.Protocol
	maxRound hare3.AbsRound
	gossiper NetworkGossiper
}

func NewProtocolRunner(
	clock RoundClock,
	protocol *hare3.Protocol,
	iterationLimit int8,
	gossiper NetworkGossiper,
) *ProtocolRunner {
	return &ProtocolRunner{
		clock:    clock,
		protocol: *protocol,
		maxRound: hare3.NewAbsRound(iterationLimit, 0),
		gossiper: gossiper,
	}
}

// Run waits for successive rounds from the clock and drives the protocol round by round.
func (r *ProtocolRunner) Run(ctx context.Context) ([]types.Hash20, error) {
	// ok want to actually use a lock here
	for {
		if r.protocol.Round() == r.maxRound {
			return nil, fmt.Errorf("hare protocol runner exceeded iteration limit of %d", r.maxRound.Iteration())
		}
		// The weak coin is calculated from pre-round messages we need to really be checking for the highest from gradecast i think.
		if r.protocol.Round() == hare3.Preround.Round()+3 {
		}
		select {
		// We await the beginning of the round, which is achieved by calling AwaitEndOfRound with (round - 1).
		case <-r.clock.AwaitEndOfRound(uint32(r.protocol.Round() - 1)):
			// This will need to be set per round to determine if this parcicipant is active in this round.
			var active bool
			toSend, output := r.protocol.NextRound(active)
			if toSend != nil {
				msg, err := buildEncodedOutputMessgae(toSend)
				if err != nil {
					// This should never happen
					panic(err)
				}
				r.gossiper.Gossip(msg)
			}
			if output != nil {
				return output, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type NetworkGossiper interface {
	Gossip(msg []byte) error
}

func buildEncodedOutputMessgae(m *hare3.OutputMessage) ([]byte, error) {
	return nil, nil
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
	AwaitLayer(types.LayerID) chan struct{}
	CurrentLayer() types.LayerID
}

type HareRunner struct {
	clock           LayerClock
	protocol        hare3.Protocol
	gossiper        NetworkGossiper
	oracle          *eligibility.Oracle
	syncer          *syncer.Syncer
	beaconRetriever *beacon.ProtocolDriver
	lc              hare3.LeaderChecker
	b               *broker.Broker
	l               log.Log
	db              *datastore.CachedDB
	output          chan hare.LayerOutput
	eg              errgroup.Group

	effectiveGenesis types.LayerID
	maxIterations    int8
	wakeupDelta      time.Duration
	roundDuration    time.Duration
	nodeID           types.NodeID
}

func (r *HareRunner) Run(ctx context.Context) {
	// Ensure that we wait for any child routines to complete before returning.
	defer r.eg.Wait()
	currentLayer := r.clock.CurrentLayer()
	var startLayer types.LayerID
	if currentLayer < r.effectiveGenesis {
		startLayer = r.effectiveGenesis + 1
		r.l.WithContext(ctx).Info(
			"hare runner awaiting start layer",
			log.Stringer("current_layer", currentLayer),
			log.Stringer("effective_genesis", r.effectiveGenesis),
			log.Stringer("start_layer", r.effectiveGenesis))
	} else {
		// We wait for the subsequent layer so that we can be sure of being on time.
		startLayer = currentLayer + 1
		r.l.WithContext(ctx).Info(
			"hare runner awaiting start layer",
			log.Stringer("current_layer", currentLayer),
			log.Stringer("start_layer", startLayer))
	}

	for layer := startLayer; ; layer += 1 {
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
			// any layer in the epoch and then adds up the weignt for that
			// ballot value, and when the threshold is crossed we consider the
			// beacon decided. But the actual value of the beacon comes from
			// the first ballot (the reference ballot) and so we can end up
			// with multiple ballots from the same smesher pusshing their
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
			actives, err := r.oracle.ActiveMap(ctx, layer)
			if err != nil {
				r.l.With().Error("aborting hare for layer, failed to retrieve active set", layer.Field(), log.Err(err))
				continue
			}
			props := goodProposals(ctx, r.l, r.db, r.nodeID, layer, beacon)

			// Execute layer
			r.eg.Go(func() error {
				result, err := r.runLayer(log.WithNewSessionID(ctx), layer, actives, props)
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

func (r *HareRunner) runLayer(
	ctx context.Context,
	layer types.LayerID,
	actives map[types.NodeID]struct{},
	props []types.ProposalID,
) ([]types.ProposalID, error) {
	handler := r.b.Register(ctx, layer)
	roundClock := hare.NewSimpleRoundClock(r.clock.LayerToTime(layer), r.wakeupDelta, r.roundDuration)
	initialSet := make([]types.Hash20, len(props))
	for i := 0; i < len(props); i++ {
		initialSet[i] = types.Hash20(props[i])
	}
	protocolRunner := NewProtocolRunner(roundClock, handler.Protocol(r.lc, initialSet), r.maxIterations, r.gossiper)
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

func goodProposals(
	ctx context.Context,
	logger log.Log,
	db *datastore.CachedDB,
	nodeID types.NodeID,
	lid types.LayerID,
	epochBeacon types.Beacon,
) []types.ProposalID {
	props, err := proposals.GetByLayer(db, lid)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			logger.With().Warning("no proposals found for hare, using empty set", log.Context(ctx), lid, log.Err(err))
		} else {
			logger.With().Error("failed to get proposals for hare", log.Context(ctx), lid, log.Err(err))
		}
		return []types.ProposalID{}
	}

	var (
		beacon        types.Beacon
		result        []types.ProposalID
		ownHdr        *types.ActivationTxHeader
		ownTickHeight = uint64(math.MaxUint64)
	)
	// a non-smesher will not filter out any proposals, as it doesn't have voting power
	// and only observes the consensus process.
	ownHdr, err = db.GetEpochAtx(lid.GetEpoch(), nodeID)
	if err != nil {
		logger.With().Error("failed to get own atx", log.Context(ctx), lid, log.Err(err))
		return []types.ProposalID{}
	}
	if ownHdr != nil {
		ownTickHeight = ownHdr.TickHeight()
	}
	atxs := map[types.ATXID]int{}
	for _, p := range props {
		atxs[p.AtxID]++
	}
	for _, p := range props {
		if p.IsMalicious() {
			logger.With().Warning("not voting on proposal from malicious identity",
				log.Stringer("id", p.ID()),
			)
			continue
		}
		if n := atxs[p.AtxID]; n > 1 {
			logger.With().Warning("proposal with same atx added several times in the recorded set",
				log.Int("n", n),
				log.Stringer("id", p.ID()),
				log.Stringer("atxid", p.AtxID),
			)
			continue
		}
		if ownHdr != nil {
			hdr, err := db.GetAtxHeader(p.AtxID)
			if err != nil {
				logger.With().Error("failed to get atx", log.Context(ctx), lid, p.AtxID, log.Err(err))
				return []types.ProposalID{}
			}
			if hdr.BaseTickHeight >= ownTickHeight {
				// does not vote for future proposal
				logger.With().Warning("proposal base tick height too high. skipping",
					log.Context(ctx),
					lid,
					log.Uint64("proposal_height", hdr.BaseTickHeight),
					log.Uint64("own_height", ownTickHeight),
				)
				continue
			}
		}
		if p.EpochData != nil {
			beacon = p.EpochData.Beacon
		} else if p.RefBallot == types.EmptyBallotID {
			logger.With().Error("proposal missing ref ballot", p.ID())
			return []types.ProposalID{}
		} else if refBallot, err := ballots.Get(db, p.RefBallot); err != nil {
			logger.With().Error("failed to get ref ballot", p.ID(), p.RefBallot, log.Err(err))
			return []types.ProposalID{}
		} else if refBallot.EpochData == nil {
			logger.With().Error("ref ballot missing epoch data", log.Context(ctx), lid, refBallot.ID())
			return []types.ProposalID{}
		} else {
			beacon = refBallot.EpochData.Beacon
		}

		if beacon == epochBeacon {
			result = append(result, p.ID())
		} else {
			logger.With().Warning("proposal has different beacon value",
				log.Context(ctx),
				lid,
				p.ID(),
				log.String("proposal_beacon", beacon.ShortString()),
				log.String("epoch_beacon", epochBeacon.ShortString()))
		}
	}
	return result
}
