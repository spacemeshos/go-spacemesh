package tortoise

import (
	"context"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type state struct {
	logger log.Log

	// cache ref ballot by epoch
	refBallotBeacons map[types.EpochID]map[types.BallotID][]byte
	// cache ballots with bad beacons. this cache is mainly for self-healing where we only have BallotID
	// in the opinions map to work with.
	badBeaconBallots map[types.BallotID]struct{}

	// epochWeight average weight of atx's that target keyed epoch
	epochWeight map[types.EpochID]uint64

	// last layer processed: note that tortoise does not have a concept of "current" layer (and it's not aware of the
	// current time or latest tick). As far as Tortoise is concerned, Last is the current layer. This is a subjective
	// view of time, but Tortoise receives layers as soon as Hare finishes processing them or when they are received via
	// gossip, and there's nothing for Tortoise to verify without new data anyway.
	Last        types.LayerID
	Processed   types.LayerID
	LastEvicted types.LayerID
	Verified    types.LayerID
	// if key exists in the map the ballot is good. if value is true it was written to the disk.
	GoodBallotsIndex map[types.BallotID]bool
	// use 2D array to be able to iterate from the latest elements easily
	BallotOpinionsByLayer map[types.LayerID]map[types.BallotID]Opinion
	// BallotWeight is a weight of a single ballot.
	BallotWeight map[types.BallotID]*big.Float
	// BallotLayer stores reverse mapping from BallotOpinionsByLayer
	BallotLayer map[types.BallotID]types.LayerID
	// BlockLayer stores mapping from BlockID to LayerID
	BlockLayer map[types.BlockID]types.LayerID
}

func (s *state) Evict(ctx context.Context, windowStart types.LayerID) error {
	var (
		logger = s.logger.WithContext(ctx)

		epochToEvict = make(map[types.EpochID]struct{})
		oldestEpoch  = windowStart.GetEpoch()
	)
	for layerToEvict := s.LastEvicted.Add(1); layerToEvict.Before(windowStart); layerToEvict = layerToEvict.Add(1) {
		logger.With().Debug("evicting layer",
			layerToEvict,
			log.Int("ballots", len(s.BallotOpinionsByLayer[layerToEvict])))
		for ballot := range s.BallotOpinionsByLayer[layerToEvict] {
			delete(s.GoodBallotsIndex, ballot)
			delete(s.BallotLayer, ballot)
			delete(s.BallotWeight, ballot)
			delete(s.BlockLayer, types.BlockID(ballot))
			delete(s.badBeaconBallots, ballot)
		}
		delete(s.BallotOpinionsByLayer, layerToEvict)
		if layerToEvict.GetEpoch() < oldestEpoch {
			epochToEvict[layerToEvict.GetEpoch()] = struct{}{}
		}
	}
	for epoch := range epochToEvict {
		delete(s.refBallotBeacons, epoch)
		delete(s.epochWeight, epoch)
	}
	s.LastEvicted = windowStart.Sub(1)
	return nil
}
