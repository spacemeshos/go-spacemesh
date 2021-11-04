package tortoise

import (
	"bytes"
	"context"
	"fmt"

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

	// cache ref blocks by epoch
	refBlockBeacons map[types.EpochID]map[types.BlockID][]byte
	// cache blocks with bad beacons. this cache is mainly for self-healing where we only have BlockID
	// in the opinions map to work with.
	badBeaconBlocks map[types.BlockID]struct{}

	// last layer processed: note that tortoise does not have a concept of "current" layer (and it's not aware of the
	// current time or latest tick). As far as Tortoise is concerned, Last is the current layer. This is a subjective
	// view of time, but Tortoise receives layers as soon as Hare finishes processing them or when they are received via
	// gossip, and there's nothing for Tortoise to verify without new data anyway.
	Last        types.LayerID
	LastEvicted types.LayerID
	Verified    types.LayerID
	// if key exists in the map the block is good. if value is true it was written to the disk.
	GoodBlocksIndex map[types.BlockID]bool
	// use 2D array to be able to iterate from the latest elements easily
	BlockOpinionsByLayer map[types.LayerID]map[types.BlockID]Opinion
	// BlockLayer stores reverse mapping from BlockOpinionsByLayer
	BlockLayer map[types.BlockID]types.LayerID
}

func (s *state) Persist() error {
	batch := s.db.NewBatch()

	var b bytes.Buffer
	for id, flushed := range s.GoodBlocksIndex {
		if flushed && s.diffMode {
			continue
		}
		s.GoodBlocksIndex[id] = true
		encodeGoodBlockKey(&b, id)
		if err := batch.Put(b.Bytes(), nil); err != nil {
			return fmt.Errorf("put 'good' namespace into batch: %w", err)
		}
		b.Reset()
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}
	batch.Reset()

	for layer, blocks := range s.BlockOpinionsByLayer {
		for block1, opinions := range blocks {
			for block2, opinion := range opinions {
				if opinion.Flushed && s.diffMode {
					continue
				}
				opinion.Flushed = true
				opinions[block2] = opinion
				encodeOpinionKey(&b, layer, block1, block2)

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
	s.GoodBlocksIndex = map[types.BlockID]bool{}
	s.BlockOpinionsByLayer = map[types.LayerID]map[types.BlockID]Opinion{}
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
		s.GoodBlocksIndex[decodeBlock(it.Key()[1:])] = true
	}
	if it.Error() != nil {
		return fmt.Errorf("good blocks iterator: %w", it.Error())
	}

	it = s.db.Find([]byte(namespaceOpinions))
	defer it.Release()
	for it.Next() {
		layer := decodeLayerKey(it.Key())
		offset := 1 + types.LayerIDSize
		block1 := decodeBlock(it.Key()[offset : offset+types.BlockIDSize])

		s.BlockLayer[block1] = layer

		if _, exist := s.BlockOpinionsByLayer[layer]; !exist {
			s.BlockOpinionsByLayer[layer] = map[types.BlockID]Opinion{}
		}
		if _, exist := s.BlockOpinionsByLayer[layer][block1]; !exist {
			s.BlockOpinionsByLayer[layer][block1] = Opinion{}
		}
		block2 := decodeBlock(it.Key()[offset+types.BlockIDSize:])

		var opinion vec
		if err := codec.Decode(it.Value(), &opinion); err != nil {
			return fmt.Errorf("decode opinion with codec: %w", err)
		}
		s.BlockOpinionsByLayer[layer][block1][block2] = opinion
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("opinion iterator: %w", it.Error())
	}

	return nil
}

func (s *state) Evict(ctx context.Context, windowStart types.LayerID) error {
	var (
		logger = s.log.WithContext(ctx)
		batch  = s.db.NewBatch()
		b      bytes.Buffer
	)
	for layerToEvict := s.LastEvicted.Add(1); layerToEvict.Before(windowStart); layerToEvict = layerToEvict.Add(1) {
		logger.With().Debug("evicting layer",
			layerToEvict,
			log.Int("blocks", len(s.BlockOpinionsByLayer[layerToEvict])))
		for blk := range s.BlockOpinionsByLayer[layerToEvict] {
			if s.GoodBlocksIndex[blk] {
				encodeGoodBlockKey(&b, blk)
				if err := batch.Delete(b.Bytes()); err != nil {
					return fmt.Errorf("delete good block in batch %x: %w", b.Bytes(), err)
				}
				b.Reset()
			}
			delete(s.GoodBlocksIndex, blk)
			delete(s.BlockLayer, blk)
			delete(s.badBeaconBlocks, blk)
			for blk2, opinion := range s.BlockOpinionsByLayer[layerToEvict][blk] {
				if !opinion.Flushed {
					continue
				}
				encodeOpinionKey(&b, layerToEvict, blk, blk2)
				if err := batch.Delete(b.Bytes()); err != nil {
					return fmt.Errorf("delete opinion in batch %x: %w", b.Bytes(), err)
				}
				b.Reset()
			}
		}
		delete(s.BlockOpinionsByLayer, layerToEvict)
		if err := batch.Write(); err != nil {
			return fmt.Errorf("write batch for layer %s to db: %w", layerToEvict, err)
		}
		batch.Reset()
	}
	oldEpoch := s.LastEvicted.GetEpoch()
	s.LastEvicted = windowStart.Sub(1)
	if s.LastEvicted.GetEpoch() > oldEpoch {
		delete(s.refBlockBeacons, oldEpoch)
	}
	return nil
}

func decodeBlock(key []byte) types.BlockID {
	var block types.BlockID
	copy(block[:], key)
	return block
}

func encodeLayerKey(layer types.LayerID) []byte {
	return util.Uint32ToBytesBE(layer.Uint32())
}

func decodeLayerKey(key []byte) types.LayerID {
	return types.NewLayerID(util.BytesToUint32BE(key[1 : 1+types.LayerIDSize]))
}

func encodeGoodBlockKey(b *bytes.Buffer, bid types.BlockID) {
	b.WriteString(namespaceGood)
	b.Write(bid.Bytes())
}

func encodeOpinionKey(b *bytes.Buffer, layer types.LayerID, bid1, bid2 types.BlockID) {
	b.WriteString(namespaceOpinions)
	b.Write(encodeLayerKey(layer))
	b.Write(bid1.Bytes())
	b.Write(bid2.Bytes())
}
