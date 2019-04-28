package activation

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/types"
	"io"
)

const CounterKey = 0xaaaa

type ActivationDb struct {
	//todo: think about whether we need one db or several
	atxs           database.DB
	meshDb         *mesh.MeshDB
	LayersPerEpoch types.LayerID
	ids            IdStore
}

func NewActivationDb(dbstore database.DB, idstore IdStore, meshDb *mesh.MeshDB, layersPerEpoch uint64) *ActivationDb {
	return &ActivationDb{atxs: dbstore, meshDb: meshDb, LayersPerEpoch: types.LayerID(layersPerEpoch), ids: idstore}
}

func (db *ActivationDb) ProcessBlockATXs(blk *types.Block) {
	for _, atx := range blk.ATXs {
		activeSet, err := db.CalcActiveSetFromView(atx)
		if err != nil {
			log.Error("could not calculate active set for %v", atx.Id())
		}
		//todo: maybe there is a potential bug in this case if count for the view can change between calls to this function
		atx.VerifiedActiveSet = activeSet
		err = db.StoreAtx(types.EpochId(atx.LayerIdx/db.LayersPerEpoch), atx)
		if err != nil {
			log.Error("cannot store atx: %v", atx)
			continue
		}
		err = db.ids.StoreNodeIdentity(atx.NodeId)
		if err != nil {
			log.Error("cannot store id: %v err=%v", atx.NodeId.String(), err)
			continue
		}
	}
}

func (db *ActivationDb) CalcActiveSetFromView(a *types.ActivationTx) (uint32, error) {
	bytes, err := types.ViewAsBytes(a.View)
	if err != nil {
		return 0, err
	}

	count, found := activesetCache.Get(common.BytesToHash(bytes))
	if found {
		return count, nil
	}

	var counter uint32 = 0
	set := make(map[types.AtxId]struct{})
	firstLayerOfLastEpoch := a.LayerIdx - db.LayersPerEpoch - (a.LayerIdx % db.LayersPerEpoch)
	lastLayerOfLastEpoch := firstLayerOfLastEpoch + db.LayersPerEpoch

	traversalFunc := func(blkh *types.BlockHeader) error {
		blk, err := db.meshDb.GetBlock(blkh.Id)
		if err != nil {
			log.Error("cannot validate atx, block %v not found", blkh.Id)
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

	mp := map[types.BlockID]struct{}{}
	for _, blk := range a.View {
		mp[blk] = struct{}{}
	}

	db.meshDb.ForBlockInView(mp, firstLayerOfLastEpoch, traversalFunc)
	activesetCache.Add(common.BytesToHash(bytes), counter)

	return counter, nil

}

//todo: move to config
const NIPST_LAYERTIME = 1000

func (db *ActivationDb) ValidateAtx(atx *types.ActivationTx) error {
	// validation rules: no other atx with same id and sequence number
	// if s != 0 the prevAtx is valid and it's seq num is s -1
	// positioning atx is valid
	// validate nipst duration?
	// fields 1-7 of the atx are the challenge of the poet
	// layer index i^ satisfies i -i^ < (layers_passed during nipst creation) ANTON: maybe should be ==?
	// the atx view contains d active Ids

	eatx, _ := db.GetAtx(atx.Id())
	if eatx != nil {
		return fmt.Errorf("found atx with same id")
	}
	prevAtxIds, err := db.GetNodeAtxIds(atx.NodeId)
	if err == nil && len(prevAtxIds) > 0 {
		prevAtx, err := db.GetAtx(prevAtxIds[len(prevAtxIds)-1])
		if err == nil {
			if prevAtx.Sequence >= atx.Sequence {
				return fmt.Errorf("found atx with same seq or greater")
			}
		}
	} else {
		if prevAtxIds == nil && atx.PrevATXId != types.EmptyAtx {
			return fmt.Errorf("found atx with invalid prev atx %v", atx.PrevATXId)
		}
	}
	if atx.PositioningAtx != types.EmptyAtx {
		posAtx, err := db.GetAtx(atx.PositioningAtx)
		if err != nil {
			return fmt.Errorf("positioning atx not found")
		}
		if !posAtx.Valid {
			return fmt.Errorf("positioning atx is not valid")
		}
		if atx.LayerIdx-posAtx.LayerIdx > NIPST_LAYERTIME {
			return fmt.Errorf("distance between pos atx invalid %v ", atx.LayerIdx-posAtx.LayerIdx)
		}
	} else {
		if atx.LayerIdx/db.LayersPerEpoch != 0 {
			return fmt.Errorf("no positioning atx found")
		}
	}

	//todo: verify NIPST challange
	if atx.VerifiedActiveSet <= uint32(db.ActiveIds(types.EpochId(uint64(atx.LayerIdx)/uint64(db.LayersPerEpoch)))) {
		return fmt.Errorf("positioning atx conatins view with more active ids than seen %v %v", atx.VerifiedActiveSet, db.ActiveIds(types.EpochId(uint64(atx.LayerIdx)/uint64(db.LayersPerEpoch))))
	}
	return nil
}

func (db *ActivationDb) StoreAtx(ech types.EpochId, atx *types.ActivationTx) error {
	log.Debug("storing atx %v, in epoch %v", atx, ech)

	//todo: maybe cleanup DB if failed by using defer
	if b, err := db.atxs.Get(atx.Id().Bytes()); err == nil && len(b) > 0 {
		// exists - how should we handle this?
		return nil
	}
	b, err := types.AtxAsBytes(atx)
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

func epochCounterKey(ech types.EpochId) []byte {
	return append(ech.ToBytes(), common.Uint64ToBytes(uint64(CounterKey))...)
}

func (db *ActivationDb) incValidAtxCounter(ech types.EpochId) error {
	key := epochCounterKey(ech)
	val, err := db.atxs.Get(key)
	if err == nil {
		return db.atxs.Put(key, common.Uint64ToBytes(common.BytesToUint64(val)+1))
	}
	return db.atxs.Put(key, common.Uint64ToBytes(1))
}

func (db *ActivationDb) ActiveIds(ech types.EpochId) uint64 {
	key := epochCounterKey(ech)
	val, err := db.atxs.Get(key)
	if err != nil {
		return 0
	}
	return common.BytesToUint64(val)
}

func (db *ActivationDb) addAtxToEpoch(epochId types.EpochId, atx types.AtxId) error {
	ids, err := db.atxs.Get(epochId.ToBytes())
	var atxs []types.AtxId
	if err != nil {
		//epoch doesnt exist, need to insert new layer
		ids = []byte{}
		atxs = make([]types.AtxId, 0, 0)
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

func (db *ActivationDb) addAtxToNodeId(nodeId types.NodeId, atx types.AtxId) error {
	ids, err := db.atxs.Get(nodeId.ToBytes())
	var atxs []types.AtxId
	if err != nil {
		//layer doesnt exist, need to insert new layer
		ids = []byte{}
		atxs = make([]types.AtxId, 0, 0)
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

func (db *ActivationDb) GetNodeAtxIds(node types.NodeId) ([]types.AtxId, error) {
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

func (db *ActivationDb) GetEpochAtxIds(epochId types.EpochId) ([]types.AtxId, error) {
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

func (db *ActivationDb) GetAtx(id types.AtxId) (*types.ActivationTx, error) {
	b, err := db.atxs.Get(id.Bytes())
	if err != nil {
		return nil, err
	}
	atx, err := types.BytesAsAtx(b)
	if err != nil {
		return nil, err
	}
	return atx, nil
}

func (db *ActivationDb) IsIdentityActive(edId string, layer types.LayerID) (bool, error) {
	epoch := layer.GetEpoch(uint16(db.LayersPerEpoch))
	nodeId, err := db.ids.GetIdentity(edId)
	if err != nil { // means there is no such identity
		log.Error("IsIdentityActive erred while getting identity err=%v", err)
		return false, nil
	}
	ids, err := db.GetNodeAtxIds(nodeId)
	if err != nil {
		return false, err
	}
	if len(ids) == 0 { // GetIdentity succeeded but no ATXs, this is a fatal error
		return false, fmt.Errorf("no active IDs for known node")
	}
	atx, err := db.GetAtx(ids[len(ids)-1])
	if err != nil {
		return false, nil
	}
	return atx.Valid && atx.LayerIdx.GetEpoch(uint16(db.LayersPerEpoch)) == epoch, err
}

func decodeAtxIds(idsBytes []byte) ([]types.AtxId, error) {
	var ids []types.AtxId
	if _, err := xdr.Unmarshal(bytes.NewReader(idsBytes), &ids); err != nil {
		return nil, errors.New("error marshaling layer ")
	}
	return ids, nil
}

func encodeAtxIds(ids []types.AtxId) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, errors.New("error marshalling atx ids ")
	}
	return w.Bytes(), nil
}
