package tortoise

import (
	"bytes"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	namespaceLast     = "l"
	namespaceEvict    = "e"
	namespaceVerified = "v"
	namespaceGood     = "g"
	namespaceOpinons  = "o"
)

type state struct {
	log log.Log
	db  database.Database

	// if true only non-flushed items will be synced
	diffMode bool

	Last            types.LayerID
	Evict           types.LayerID
	Verified        types.LayerID
	GoodBlocksIndex map[types.BlockID]bool
	// use 2D array to be able to iterate from latest elements easily
	BlockOpinionsByLayer map[types.LayerID]map[types.BlockID]Opinion // records hdist, for each block, its votes about every
	// TODO: Tal says: We keep a vector containing our vote totals (positive and negative) for every previous block
	// that's not needed here, probably for self healing?
}

func (s *state) Persist() error {
	batch := s.db.NewBatch()

	if err := batch.Put([]byte(namespaceLast), s.Last.Bytes()); err != nil {
		return err
	}
	if err := batch.Put([]byte(namespaceEvict), s.Evict.Bytes()); err != nil {
		return err
	}
	if err := batch.Put([]byte(namespaceVerified), s.Verified.Bytes()); err != nil {
		return err
	}
	var b bytes.Buffer
	for id, flushed := range s.GoodBlocksIndex {
		if flushed && s.diffMode {
			continue
		}
		s.GoodBlocksIndex[id] = true
		b.Reset()
		b.WriteString(namespaceGood)
		b.Write(id.Bytes())
		if err := batch.Put(b.Bytes(), nil); err != nil {
			return err
		}
	}

	for layer, blocks := range s.BlockOpinionsByLayer {
		for block1, opinions := range blocks {
			for block2, opinion := range opinions {
				if opinion.Flushed && s.diffMode {
					continue
				}
				opinion.Flushed = true
				opinions[block2] = opinion

				b.Reset()
				b.WriteString(namespaceOpinons)
				b.Write(layer.Bytes())
				b.Write(block1.Bytes())
				b.Write(block2.Bytes())

				buf, err := codec.Encode(opinion)
				if err != nil {
					s.log.Panic("can't encode vec", log.Err(err))
				}
				if err := batch.Put(b.Bytes(), buf); err != nil {
					return err
				}
			}
		}
	}
	return batch.Write()
}

func (s *state) Recover() error {
	s.GoodBlocksIndex = map[types.BlockID]bool{}
	s.BlockOpinionsByLayer = map[types.LayerID]map[types.BlockID]Opinion{}

	buf, err := s.db.Get([]byte(namespaceLast))
	if err != nil {
		return err
	}
	s.Last = types.BytesToLayerID(buf)
	buf, err = s.db.Get([]byte(namespaceVerified))
	if err != nil {
		return err
	}
	s.Verified = types.BytesToLayerID(buf)
	buf, err = s.db.Get([]byte(namespaceEvict))
	if err != nil {
		return err
	}
	s.Evict = types.BytesToLayerID(buf)

	it := s.db.Find([]byte(namespaceGood))
	for it.Next() {
		var id types.BlockID
		copy(id[:], it.Key()[1:])
		s.GoodBlocksIndex[id] = true
	}

	it = s.db.Find([]byte(namespaceOpinons))
	for it.Next() {
		// FIXME(dshulyak) derive these indexes from actual values
		layer := types.BytesToLayerID(it.Key()[1:5])
		var block1, block2 types.BlockID
		copy(block1[:], it.Key()[5:37])
		copy(block2[:], it.Key()[37:])

		if _, exist := s.BlockOpinionsByLayer[layer]; !exist {
			s.BlockOpinionsByLayer[layer] = map[types.BlockID]Opinion{}
		}
		if _, exist := s.BlockOpinionsByLayer[layer][block1]; !exist {
			s.BlockOpinionsByLayer[layer][block1] = Opinion{}
		}
		var opinion vec
		if err := codec.Decode(it.Value(), &opinion); err != nil {
			return err
		}
		s.BlockOpinionsByLayer[layer][block1][block2] = opinion
	}
	return nil
}
