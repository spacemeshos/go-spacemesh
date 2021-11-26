package tortoise

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	namespaceLast     = "l"
	namespaceEvict    = "e"
	namespaceVerified = "v"
	namespaceGood     = "g"
	namespaceOpinions = "o"
)

type state struct {
	log log.Log
	db  database.Database

	// if true only non-flushed items will be persisted
	diffMode bool

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

func (s *state) Persist() error {
	batch := s.db.NewBatch()

	var b bytes.Buffer
	for id, flushed := range s.GoodBallotsIndex {
		if flushed && s.diffMode {
			continue
		}
		s.GoodBallotsIndex[id] = true
		encodeGoodBallotKey(&b, id)
		if err := batch.Put(b.Bytes(), nil); err != nil {
			return fmt.Errorf("put 'good' namespace into batch: %w", err)
		}
		b.Reset()
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}
	batch.Reset()

	for layer, ballots := range s.BallotOpinionsByLayer {
		for ballot, opinions := range ballots {
			for block, opinion := range opinions {
				if opinion.Flushed && s.diffMode {
					continue
				}
				opinion.Flushed = true
				opinions[block] = opinion
				encodeOpinionKey(&b, layer, ballot, block)

				buf, err := codec.Encode(opinion)
				if err != nil {
					s.log.With().Panic("can't encode vec", log.Err(err))
				}
				if err := batch.Put(b.Bytes(), buf); err != nil {
					return fmt.Errorf("put 'opinions' namespace into batch: %w", err)
				}
				b.Reset()
			}
		}
		if err := batch.Write(); err != nil {
			return fmt.Errorf("write batch: %w", err)
		}
		batch.Reset()
	}

	if err := batch.Put([]byte(namespaceLast), s.Last.Bytes()); err != nil {
		return fmt.Errorf("put 'last' namespace into batch: %w", err)
	}
	if err := batch.Put([]byte(namespaceEvict), s.LastEvicted.Bytes()); err != nil {
		return fmt.Errorf("put 'evict' namespace into batch: %w", err)
	}
	if err := batch.Put([]byte(namespaceVerified), s.Verified.Bytes()); err != nil {
		return fmt.Errorf("put 'verified' namespace into batch: %w", err)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}
	return nil
}

func (s *state) Recover() error {
	s.GoodBallotsIndex = map[types.BallotID]bool{}
	s.BallotOpinionsByLayer = map[types.LayerID]map[types.BallotID]Opinion{}
	s.BallotLayer = map[types.BallotID]types.LayerID{}
	s.BlockLayer = map[types.BlockID]types.LayerID{}

	buf, err := s.db.Get([]byte(namespaceLast))
	if err != nil {
		return fmt.Errorf("get 'last' namespace from DB: %w", err)
	}
	s.Last = types.BytesToLayerID(buf)

	buf, err = s.db.Get([]byte(namespaceVerified))
	if err != nil {
		return fmt.Errorf("get 'verified' namespace from DB: %w", err)
	}
	s.Verified = types.BytesToLayerID(buf)

	buf, err = s.db.Get([]byte(namespaceEvict))
	if err != nil {
		return fmt.Errorf("get 'last evicted' namespace from DB: %w", err)
	}
	s.LastEvicted = types.BytesToLayerID(buf)

	it := s.db.Find([]byte(namespaceGood))
	defer it.Release()
	for it.Next() {
		s.GoodBallotsIndex[decodeBallot(it.Key()[1:])] = true
	}
	if it.Error() != nil {
		return fmt.Errorf("good ballot iterator: %w", it.Error())
	}

	it = s.db.Find([]byte(namespaceOpinions))
	defer it.Release()
	for it.Next() {
		layer := decodeLayerKey(it.Key())
		offset := 1 + types.LayerIDSize
		ballot := decodeBallot(it.Key()[offset : offset+types.BallotIDSize])

		s.BallotLayer[ballot] = layer
		// TODO: find a way to persist block->layer mapping independently with unified content block
		s.BlockLayer[types.BlockID(ballot)] = layer

		if _, exist := s.BallotOpinionsByLayer[layer]; !exist {
			s.BallotOpinionsByLayer[layer] = map[types.BallotID]Opinion{}
		}
		if _, exist := s.BallotOpinionsByLayer[layer][ballot]; !exist {
			s.BallotOpinionsByLayer[layer][ballot] = Opinion{}
		}
		block := decodeBlock(it.Key()[offset+types.BlockIDSize:])

		var opinion vec
		if err := codec.Decode(it.Value(), &opinion); err != nil {
			return fmt.Errorf("decode opinion with codec: %w", err)
		}
		s.BallotOpinionsByLayer[layer][ballot][block] = opinion
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("opinion iterator: %w", it.Error())
	}

	return nil
}

func (s *state) Evict(ctx context.Context, windowStart types.LayerID) error {
	var (
		logger       = s.log.WithContext(ctx)
		batch        = s.db.NewBatch()
		b            bytes.Buffer
		epochToEvict = make(map[types.EpochID]struct{})
		oldestEpoch  = windowStart.GetEpoch()
	)
	for layerToEvict := s.LastEvicted.Add(1); layerToEvict.Before(windowStart); layerToEvict = layerToEvict.Add(1) {
		logger.With().Debug("evicting layer",
			layerToEvict,
			log.Int("ballots", len(s.BallotOpinionsByLayer[layerToEvict])))
		for ballot := range s.BallotOpinionsByLayer[layerToEvict] {
			if s.GoodBallotsIndex[ballot] {
				encodeGoodBallotKey(&b, ballot)
				if err := batch.Delete(b.Bytes()); err != nil {
					return fmt.Errorf("delete good ballot in batch %x: %w", b.Bytes(), err)
				}
				b.Reset()
			}
			delete(s.GoodBallotsIndex, ballot)
			delete(s.BallotLayer, ballot)
			delete(s.BallotWeight, ballot)
			delete(s.BlockLayer, types.BlockID(ballot))
			delete(s.badBeaconBallots, ballot)
			for block, opinion := range s.BallotOpinionsByLayer[layerToEvict][ballot] {
				if !opinion.Flushed {
					continue
				}
				encodeOpinionKey(&b, layerToEvict, ballot, block)
				if err := batch.Delete(b.Bytes()); err != nil {
					return fmt.Errorf("delete opinion in batch %x: %w", b.Bytes(), err)
				}
				b.Reset()
			}
		}
		delete(s.BallotOpinionsByLayer, layerToEvict)
		if layerToEvict.GetEpoch() < oldestEpoch {
			epochToEvict[layerToEvict.GetEpoch()] = struct{}{}
		}
		if err := batch.Write(); err != nil {
			return fmt.Errorf("write batch for layer %s to db: %w", layerToEvict, err)
		}
		batch.Reset()
	}
	for epoch := range epochToEvict {
		delete(s.refBallotBeacons, epoch)
		delete(s.epochWeight, epoch)
	}
	s.LastEvicted = windowStart.Sub(1)
	return nil
}

func decodeBallot(key []byte) types.BallotID {
	var ballotID types.BallotID
	copy(ballotID[:], key)
	return ballotID
}

func decodeBlock(key []byte) types.BlockID {
	var bid types.BlockID
	copy(bid[:], key)
	return bid
}

func encodeLayerKey(layer types.LayerID) []byte {
	return util.Uint32ToBytesBE(layer.Uint32())
}

func decodeLayerKey(key []byte) types.LayerID {
	return types.NewLayerID(util.BytesToUint32BE(key[1 : 1+types.LayerIDSize]))
}

func encodeGoodBallotKey(b *bytes.Buffer, ballot types.BallotID) {
	b.WriteString(namespaceGood)
	b.Write(ballot.Bytes())
}

func encodeOpinionKey(b *bytes.Buffer, layer types.LayerID, ballot types.BallotID, block types.BlockID) {
	b.WriteString(namespaceOpinions)
	b.Write(encodeLayerKey(layer))
	b.Write(ballot.Bytes())
	b.Write(block.Bytes())
}
