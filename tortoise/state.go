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

	for layer1, blocks := range s.BlockOpinionsByLayer {
		for block1, blockOpinions := range blocks {
			for layer2, layerOpinions := range blockOpinions {
				for block2, opinion := range layerOpinions {
					if opinion.Flushed && s.diffMode {
						continue
					}
					opinion.Flushed = true
					layerOpinions[block2] = opinion
					encodeOpinionKey(&b, layer1, layer2, block1, block2)

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
		// first byte is namespace
		layer1Offset := len(namespaceOpinions)
		block1Offset := layer1Offset + types.LayerIDSize
		layer2Offset := block1Offset + types.BlockIDSize
		block2Offset := layer2Offset + types.LayerIDSize

		layer1 := decodeLayerKey(it.Key()[layer1Offset:])
		block1 := decodeBlock(it.Key()[block1Offset:layer2Offset])
		s.BlockLayer[block1] = layer1

		if _, exist := s.BlockOpinionsByLayer[layer1]; !exist {
			s.BlockOpinionsByLayer[layer1] = map[types.BlockID]Opinion{}
		}
		if _, exist := s.BlockOpinionsByLayer[layer1][block1]; !exist {
			s.BlockOpinionsByLayer[layer1][block1] = Opinion{}
		}

		layer2 := decodeLayerKey(it.Key()[layer2Offset:block2Offset])
		block2 := decodeBlock(it.Key()[block2Offset:])
		if _, exist := s.BlockOpinionsByLayer[layer1][block1][layer2]; !exist {
			s.BlockOpinionsByLayer[layer1][block1][layer2] = map[types.BlockID]vec{}
		}

		var opinion vec
		if err := codec.Decode(it.Value(), &opinion); err != nil {
			return fmt.Errorf("decode opinion with codec: %w", err)
		}
		s.BlockOpinionsByLayer[layer1][block1][layer2][block2] = opinion
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
			for lid := range s.BlockOpinionsByLayer[layerToEvict][blk] {
				for blk2, opinion := range s.BlockOpinionsByLayer[layerToEvict][blk][lid] {
					if !opinion.Flushed {
						continue
					}
					encodeOpinionKey(&b, layerToEvict, lid, blk, blk2)
					if err := batch.Delete(b.Bytes()); err != nil {
						return fmt.Errorf("delete opinion in batch %x: %w", b.Bytes(), err)
					}
					b.Reset()
				}
			}
		}
		delete(s.BlockOpinionsByLayer, layerToEvict)
		for lyr := range s.BlockOpinionsByLayer {
			for blk := range s.BlockOpinionsByLayer[lyr] {
				delete(s.BlockOpinionsByLayer[lyr][blk], layerToEvict)
			}
		}
		if err := batch.Write(); err != nil {
			return fmt.Errorf("write batch for layer %s to db: %w", layerToEvict, err)
		}
		batch.Reset()
	}
	s.LastEvicted = windowStart.Sub(1)
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
	return types.NewLayerID(util.BytesToUint32BE(key[:types.LayerIDSize]))
}

func encodeGoodBlockKey(b *bytes.Buffer, bid types.BlockID) {
	b.WriteString(namespaceGood)
	b.Write(bid.Bytes())
}

func encodeOpinionKey(b *bytes.Buffer, layer1, layer2 types.LayerID, bid1, bid2 types.BlockID) {
	b.WriteString(namespaceOpinions)
	b.Write(encodeLayerKey(layer1))
	b.Write(bid1.Bytes())
	b.Write(encodeLayerKey(layer2))
	b.Write(bid2.Bytes())
}
