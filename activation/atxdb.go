package activation

import (
	"bytes"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"io"
)

const CounterKey = 0xaaaa

type ActivationDb struct {
	//todo: think about whether we need one db or several
	atxs           database.DB
	meshDb         *mesh.MeshDB
	LayersPerEpoch block.LayerID
}

func NewActivationDb(dbstore database.DB, meshDb *mesh.MeshDB, layersPerEpoch uint64) *ActivationDb {
	return &ActivationDb{atxs: dbstore, meshDb: meshDb, LayersPerEpoch: block.LayerID(layersPerEpoch)}
}

func (m *ActivationDb) ProcessBlockATXs(blk *block.Block) {
	for _, atx := range blk.ATXs {
		activeSet, err := m.CalcActiveSetFromView(atx)
		if err != nil {
			log.Error("could not calculate active set for %v", atx.Id())
		}
		atx.VerifiedActiveSet = activeSet
		err = m.StoreAtx(block.EpochId(atx.LayerIndex/m.LayersPerEpoch), atx)
		if err != nil {
			log.Error("cannot store atx: %v", atx)
		}
	}
}

func (m *ActivationDb) CalcActiveSetFromView(a *block.ActivationTx) (uint32, error) {
	bytes, err := block.ViewAsBytes(a.View)
	if err != nil {
		return 0, err
	}

	count, found := activesetCache.Get(common.BytesToHash(bytes))
	if found {
		return count, nil
	}

	var counter uint32 = 0
	set := make(map[block.AtxId]struct{})
	firstLayerOfLastEpoch := a.LayerIndex - m.LayersPerEpoch - (a.LayerIndex % m.LayersPerEpoch)
	lastLayerOfLastEpoch := firstLayerOfLastEpoch + m.LayersPerEpoch

	traversalFunc := func(blkh *block.BlockHeader) error {
		blk, err := m.meshDb.GetBlock(blkh.Id)
		if err != nil {
			log.Error("cannot validate atx, block %v not found", blk.Id)
			return err
		}
		//skip blocks not from atx epoch
		if blk.LayerIndex > lastLayerOfLastEpoch {
			return nil
		}
		for _, atx := range blk.ATXs {
			if _, found := set[atx.Id()]; found {
				continue
			}
			set[atx.Id()] = struct{}{}
			if atx.Validate() == nil {
				counter++
				if counter >= a.ActiveSetSize {
					return io.EOF
				}
			}
		}
		return nil
	}

	errHandler := func(er error) {}

	mp := map[block.BlockID]struct{}{}
	for _, blk := range a.View {
		mp[blk] = struct{}{}
	}

	m.meshDb.ForBlockInView(mp, firstLayerOfLastEpoch, traversalFunc, errHandler)
	activesetCache.Add(common.BytesToHash(bytes), counter)

	return counter, nil

}

func (db *ActivationDb) StoreAtx(ech block.EpochId, atx *block.ActivationTx) error {
	log.Debug("storing atx %v, in epoch %v", atx, ech)

	//todo: maybe cleanup DB if failed by using defer
	if b, err := db.atxs.Get(atx.Id().Bytes()); err == nil && len(b) > 0 {
		// exists - how should we handle this?
		return nil
	}
	b, err := block.AtxAsBytes(atx)
	if err != nil {
		return err
	}
	err = db.atxs.Put(atx.Id().Bytes(), b)
	if err != nil {
		return err
	}

	err = db.addAtxToEpoch(ech, atx.Id())
	if err != nil {
		return err
	}
	if atx.Validate() == nil {
		db.incValidAtxCounter(ech)
	}
	err = db.addAtxToNodeId(atx.NodeId, atx.Id())
	if err != nil {
		return err
	}
	return nil
}

func epochCounterKey(ech block.EpochId) []byte {
	return append(ech.ToBytes(), common.Uint64ToBytes(uint64(CounterKey))...)
}

func (db *ActivationDb) incValidAtxCounter(ech block.EpochId) error {
	key := epochCounterKey(ech)
	val, err := db.atxs.Get(key)
	if err == nil {
		return db.atxs.Put(key, common.Uint64ToBytes(common.BytesToUint64(val)+1))
	}
	return db.atxs.Put(key, common.Uint64ToBytes(1))
}

func (db *ActivationDb) ActiveIds(ech block.EpochId) uint64 {
	key := epochCounterKey(ech)
	val, err := db.atxs.Get(key)
	if err != nil {
		return 0
	}
	return common.BytesToUint64(val)
}

func (db *ActivationDb) addAtxToEpoch(epochId block.EpochId, atx block.AtxId) error {
	ids, err := db.atxs.Get(epochId.ToBytes())
	var atxs []block.AtxId
	if err != nil {
		//epoch doesnt exist, need to insert new layer
		ids = []byte{}
		atxs = make([]block.AtxId, 0, 0)
	} else {
		atxs, err = decodeAtxIds(ids)
		if err != nil {
			return errors.New("could not get all atxs from database ")
		}
	}
	atxs = append(atxs, atx)
	w, err := encodeAtxIds(atxs)
	if err != nil {
		return errors.New("could not encode layer atx ids")
	}
	return db.atxs.Put(epochId.ToBytes(), w)
}

func (db *ActivationDb) addAtxToNodeId(nodeId block.NodeId, atx block.AtxId) error {
	ids, err := db.atxs.Get(nodeId.ToBytes())
	var atxs []block.AtxId
	if err != nil {
		//layer doesnt exist, need to insert new layer
		ids = []byte{}
		atxs = make([]block.AtxId, 0, 0)
	} else {
		atxs, err = decodeAtxIds(ids)
		if err != nil {
			return errors.New("could not get all atxs from database ")
		}
	}
	atxs = append(atxs, atx)
	w, err := encodeAtxIds(atxs)
	if err != nil {
		return errors.New("could not encode layer atx ids")
	}
	return db.atxs.Put(nodeId.ToBytes(), w)
}

func (db *ActivationDb) GetNodeAtxIds(node block.NodeId) ([]block.AtxId, error) {
	ids, err := db.atxs.Get(node.ToBytes())
	if err != nil {
		return nil, err
	}
	atxs, err := decodeAtxIds(ids)
	if err != nil {
		return nil, err
	}
	return atxs, nil
}

func (db *ActivationDb) GetEpochAtxIds(epochId block.EpochId) ([]block.AtxId, error) {
	ids, err := db.atxs.Get(epochId.ToBytes())
	if err != nil {
		return nil, err
	}
	atxs, err := decodeAtxIds(ids)
	if err != nil {
		return nil, err
	}
	return atxs, nil
}

func (db *ActivationDb) GetAtx(id block.AtxId) (*block.ActivationTx, error) {
	b, err := db.atxs.Get(id.Bytes())
	if err != nil {
		return nil, err
	}
	atx, err := block.BytesAsAtx(b)
	if err != nil {
		return nil, err
	}
	return atx, nil
}

func decodeAtxIds(idsBytes []byte) ([]block.AtxId, error) {
	var ids []block.AtxId
	if _, err := xdr.Unmarshal(bytes.NewReader(idsBytes), &ids); err != nil {
		return nil, errors.New("error marshaling layer ")
	}
	return ids, nil
}

func encodeAtxIds(ids []block.AtxId) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, errors.New("error marshalling atx ids ")
	}
	return w.Bytes(), nil
}
