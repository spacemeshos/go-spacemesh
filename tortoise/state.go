package tortoise

import (
	"bytes"
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

	if err := batch.Put([]byte(namespaceLast), s.Last.Bytes()); err != nil {
		return fmt.Errorf("put 'last' namespace into batch: %w", err)
	}
	if err := batch.Put([]byte(namespaceEvict), s.LastEvicted.Bytes()); err != nil {
		return fmt.Errorf("put 'evict' namespace into batch: %w", err)
	}
	if err := batch.Put([]byte(namespaceVerified), s.Verified.Bytes()); err != nil {
		return fmt.Errorf("put 'verified' namespace into batch: %w", err)
	}
	var b bytes.Buffer
	for id, flushed := range s.GoodBlocksIndex {
		if flushed && s.diffMode {
			continue
		}
		s.GoodBlocksIndex[id] = true
		b.WriteString(namespaceGood)
		b.Write(id.Bytes())
		if err := batch.Put(b.Bytes(), nil); err != nil {
			return fmt.Errorf("put 'good' namespace into batch: %w", err)
		}
		b.Reset()
	}

	for layer1, blocks := range s.BlockOpinionsByLayer {
		for block1, blockOpinions := range blocks {
			for layer2, layerOpinions := range blockOpinions {
				for block2, opinion := range layerOpinions {
					if opinion.Flushed && s.diffMode {
						continue
					}
					opinion.Flushed = true
					layerOpinions[block2] = opinion
					b.WriteString(namespaceOpinions)
					b.Write(encodeLayerKey(layer1))
					b.Write(block1.Bytes())
					b.Write(encodeLayerKey(layer2))
					b.Write(block2.Bytes())

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
		layer1 := decodeLayerKey(it.Key()[1:])
		offset1 := 1 + types.LayerIDSize
		offset2 := offset1 + types.BlockIDSize + types.LayerIDSize
		block1 := decodeBlock(it.Key()[offset1 : offset1+types.BlockIDSize])

		s.BlockLayer[block1] = layer1

		if _, exist := s.BlockOpinionsByLayer[layer1]; !exist {
			s.BlockOpinionsByLayer[layer1] = map[types.BlockID]Opinion{}
		}
		if _, exist := s.BlockOpinionsByLayer[layer1][block1]; !exist {
			s.BlockOpinionsByLayer[layer1][block1] = Opinion{}
		}
		layer2 := decodeLayerKey(it.Key()[offset1+types.BlockIDSize : offset2])
		block2 := decodeBlock(it.Key()[offset2:])
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

func (s *state) Evict() error {
	var (
		batch = s.db.NewBatch()
		it    = s.db.Find([]byte(namespaceOpinions))
		b     bytes.Buffer
	)
	defer it.Release()
	for it.Next() {
		// first byte is namespace
		layer := decodeLayerKey(it.Key()[1:])
		if layer.After(s.LastEvicted) {
			break
		}
		b.WriteString(namespaceGood)
		offset := 1 + types.LayerIDSize
		b.Write(it.Key()[offset : offset+types.BlockIDSize])
		if err := batch.Delete(b.Bytes()); err != nil {
			return fmt.Errorf("delete from batch: %w", err)
		}
		b.Reset()
		if err := batch.Delete(it.Key()); err != nil {
			return fmt.Errorf("delete from batch: %w", err)
		}
	}
	if it.Error() != nil {
		return fmt.Errorf("iterator: %w", it.Error())
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("write batch: %w", err)
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
	return types.NewLayerID(util.BytesToUint32BE(key[:types.LayerIDSize]))
}
