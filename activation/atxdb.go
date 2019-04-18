package activation

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/types"
	"io"
	"sync"
)

const CounterKey = 0xaaaa

type ActivationDb struct {
	sync.RWMutex
	//todo: think about whether we need one db or several
	atxs           database.DB
	meshDb         *mesh.MeshDB
	LayersPerEpoch types.LayerID
	log            log.Log
}

func NewActivationDb(dbstore database.DB, meshDb *mesh.MeshDB, layersPerEpoch uint64, log log.Log) *ActivationDb {
	return &ActivationDb{atxs: dbstore, meshDb: meshDb, LayersPerEpoch: types.LayerID(layersPerEpoch), log: log}
}

func (db *ActivationDb) ProcessBlockATXs(blk *types.Block) {
	for _, atx := range blk.ATXs {
		db.ProcessAtx(atx, true)
	}
}

func (db *ActivationDb) ProcessAtx(atx *types.ActivationTx, fromBlock bool) {
	epoch := atx.PubLayerIdx.GetEpoch(uint16(db.LayersPerEpoch))
	db.log.Info("processing atx id %v, epoch %v node: %v", atx.Id().String()[2:7], epoch, atx.NodeId.Key[:5])
	activeSet, err := db.CalcActiveSetFromView(atx)
	if err != nil {
		db.log.Error("could not calculate active set for %v", atx.Id())
	}
	//todo: maybe there is a potential bug in this case if count for the view can change between calls to this function
	atx.VerifiedActiveSet = activeSet
	err = db.ValidateAtx(atx)
	//todo: should we store invalid atxs
	if err != nil {
		db.log.Warning("ATX %v failed validation: %v", atx.Id().String()[2:7], err)
	} else {
		db.log.Info("ATX %v is valid", atx.Id().String()[2:7])
	}
	atx.Valid = err == nil
	err = db.StoreAtx(epoch, atx)
	if err != nil {
		db.log.Error("cannot store atx: %v", atx)
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
	firstLayerOfLastEpoch := a.PubLayerIdx - db.LayersPerEpoch - (a.PubLayerIdx % db.LayersPerEpoch)
	lastLayerOfLastEpoch := firstLayerOfLastEpoch + db.LayersPerEpoch

	traversalFunc := func(blkh *types.BlockHeader) error {
		blk, err := db.meshDb.GetBlock(blkh.Id)
		if err != nil {
			db.log.Error("cannot validate atx, block %v not found", blkh.Id)
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
			atx, err := db.GetAtx(atx.Id())
			if err == nil && atx.Valid {
				counter++
				if counter >= a.ActiveSetSize {
					return io.EOF
				}
			}
		}
		return nil
	}

	errHandler := func(er error) {}

	mp := map[types.BlockID]struct{}{}
	for _, blk := range a.View {
		mp[blk] = struct{}{}
	}

	db.meshDb.ForBlockInView(mp, firstLayerOfLastEpoch, traversalFunc, errHandler)
	activesetCache.Add(common.BytesToHash(bytes), counter)

	return counter, nil

}


// ValidateAtx ensures the following conditions apply, otherwise it returns an error.
//
// - No other ATX exists with the same MinerID and sequence number.
// - If the sequence number is non-zero: PrevATX points to a valid ATX whose sequence number is one less than the
//   current ATX's sequence number.
// - If the sequence number is zero: PrevATX is empty.
// - Positioning ATX points to a valid ATX.
// - NIPST challenge is a hash of the serialization of the following fields:
//   NodeID, SequenceNumber, PrevATXID, LayerID, StartTick, PositioningATX.
// - ATX LayerID is NipstLayerTime or more after the PositioningATX LayerID.
// - The ATX view of the previous epoch contains ActiveSetSize activations
func (db *ActivationDb) ValidateAtx(atx *types.ActivationTx) error {
	eatx, _ := db.GetAtx(atx.Id())
	if eatx != nil {
		return fmt.Errorf("found atx with same id") // TODO: should this validation be here?
	}

	if atx.PrevATXId != types.EmptyAtxId {
		prevATX, err := db.GetAtx(atx.PrevATXId)
		if err != nil {
			return fmt.Errorf("prevATX not found")
		}
		if !prevATX.Valid {
			return fmt.Errorf("prevATX not valid")
		}
		if prevATX.Sequence+1 != atx.Sequence {
			return fmt.Errorf("sequence number is not one more than prev sequence number")
		}
	} else {
		if atx.Sequence != 0 {
			return fmt.Errorf("no prevATX reported, but sequence number not zero")
		}
		prevAtxIds, err := db.GetNodeAtxIds(atx.NodeId)
		if err == nil && len(prevAtxIds) > 0 {
			var ids []string
			for _, x := range prevAtxIds {
				ids = append(ids,x.String()[2:7])
			}
			return fmt.Errorf("no prevATX reported, but other ATXs with same nodeID (%v) found, atxs: %v", atx.NodeId.Key[:5], ids)
		}
	}

	if atx.PositioningAtx != types.EmptyAtxId {
		posAtx, err := db.GetAtx(atx.PositioningAtx)
		if err != nil {
			return fmt.Errorf("positioning atx not found")
		}
		if !posAtx.Valid {
			return fmt.Errorf("positioning atx is not valid")
		}
		if atx.PubLayerIdx-posAtx.PubLayerIdx > db.LayersPerEpoch {
			return fmt.Errorf("distance between pos atx invalid %v ", atx.PubLayerIdx-posAtx.PubLayerIdx)
		}
	} else {
		epoch := atx.PubLayerIdx.GetEpoch(uint16(db.LayersPerEpoch))
		if epoch == 0 {
			return fmt.Errorf("atx epoch cannot be 0")
		}
		if !(epoch - 1).IsGenesis() {
			return fmt.Errorf("no positioning atx found")
		}
	}

	if atx.ActiveSetSize != atx.VerifiedActiveSet {
		return fmt.Errorf("atx conatins view with unequal active ids (%v) than seen (%v)", atx.ActiveSetSize, atx.VerifiedActiveSet)
	}

	hash, err := atx.NIPSTChallenge.Hash()
	if err != nil {
		return fmt.Errorf("cannot get NIPST Challenge hash: %v", err)
	}
	if !atx.Nipst.ValidateNipstChallenge(hash) {
		return fmt.Errorf("nipst challenge hash mismatch")
	}
	return nil
}

func (db *ActivationDb) StoreAtx(ech types.EpochId, atx *types.ActivationTx) error {
	db.Lock()
	defer db.Unlock()
	db.log.Debug("storing atx %v, in epoch %v", atx, ech)

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
	if atx.Valid {
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
	key := epochCounterKey(ech - 1)
	val, err := db.atxs.Get(key)
	if err == nil {
		return db.atxs.Put(key, common.Uint32ToBytes(common.BytesToUint32(val)+1))
	}
	return db.atxs.Put(key, common.Uint32ToBytes(1))
}

func (db *ActivationDb) ActiveSetIds(ech types.EpochId) uint32 {
	key := epochCounterKey(ech)
	db.RLock()
	val, err := db.atxs.Get(key)
	db.RUnlock()
	if err != nil {
		return 0
	}
	return common.BytesToUint32(val)
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

func getNodeIdKey(id types.NodeId) []byte {
	return []byte(id.Key)
}

func (db *ActivationDb) addAtxToNodeId(nodeId types.NodeId, atx types.AtxId) error {
	key := getNodeIdKey(nodeId)
	db.log.Info("adding atx %v to nodeId %v", atx.String()[2:7], nodeId.Key[:5])
	ids, err := db.atxs.Get(key)
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
	return db.atxs.Put(key, w)
}

func (db *ActivationDb) GetNodeAtxIds(nodeId types.NodeId) ([]types.AtxId, error) {
	key := getNodeIdKey(nodeId)
	db.log.Info("fetching atxIDs for node %v", nodeId.Key[:5])

	db.RLock()
	ids, err := db.atxs.Get(key)
	db.RUnlock()
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
	db.RLock()
	ids, err := db.atxs.Get(epochId.ToBytes())
	db.RUnlock()
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
	db.RLock()
	b, err := db.atxs.Get(id.Bytes())
	db.RUnlock()
	if err != nil {
		return nil, err
	}
	atx, err := types.BytesAsAtx(b)
	if err != nil {
		return nil, err
	}
	return atx, nil
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
