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
	"sort"
	"sync"
)

const CounterKey = 0xaaaa

type ActivationDb struct {
	sync.RWMutex
	//todo: think about whether we need one db or several
	atxs           database.DB
	meshDb         *mesh.MeshDB
	LayersPerEpoch types.LayerID
	nipstValidator NipstValidator
	ids            IdStore
	log            log.Log
}

func NewActivationDb(dbstore database.DB, idstore IdStore, meshDb *mesh.MeshDB, layersPerEpoch uint64, nipstValidator NipstValidator, log log.Log) *ActivationDb {
	return &ActivationDb{atxs: dbstore, meshDb: meshDb, nipstValidator: nipstValidator, LayersPerEpoch: types.LayerID(layersPerEpoch), ids: idstore, log: log}
}

func (db *ActivationDb) ProcessBlockATXs(blk *types.Block) {
	for _, atx := range blk.ATXs {
		db.ProcessAtx(atx)
	}
}

// ProcessAtx validates the active set size declared in the atx, and validates the atx according to atx validation rules
// it then stores the atx with flag set to validity of the atx
func (db *ActivationDb) ProcessAtx(atx *types.ActivationTx) {
	eatx, _ := db.GetAtx(atx.Id())
	if eatx != nil {
		return
	}
	epoch := atx.PubLayerIdx.GetEpoch(uint16(db.LayersPerEpoch))
	db.log.Info("processing atx id %v, pub-epoch %v node: %v layer %v", atx.Id().String()[2:7], epoch, atx.NodeId.Key[:5], atx.PubLayerIdx)
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

// CalcActiveSetFromView traverses the view found in a - the activation tx and counts number of active ids published
// in the epoch prior to the epoch that a was published at, this number is the number of active ids in the next epoch
// the function returns error if the view is not found
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
	lastLayerOfLastEpoch := firstLayerOfLastEpoch + db.LayersPerEpoch - 1

	traversalFunc := func(blkh *types.BlockHeader) error {
		blk, err := db.meshDb.GetMiniBlock(blkh.Id)
		if err != nil {
			db.log.Error("cannot validate atx, block %v not found", blkh.Id)
			return err
		}
		//skip blocks not from atx epoch
		if blk.LayerIndex > lastLayerOfLastEpoch {
			return nil
		}
		for _, id := range blk.ATxIds {
			if _, found := set[id]; found {
				continue
			}
			set[id] = struct{}{}
			atx, err := db.GetAtx(id)
			if err == nil && atx.Valid {
				counter++
				db.log.Info("atx found traversing %v in block in layer %v", atx.ShortId(), blk.LayerIndex)
				// return eof to signal that enough active ids were found
				if counter >= a.ActiveSetSize {
					return io.EOF
				}
			} else {
				if err == nil {
					db.log.Error("atx found %v, but not valid", atx.Id().String()[:5])
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
	if atx.PrevATXId != *types.EmptyAtxId {
		prevATX, err := db.GetAtx(atx.PrevATXId)
		if err != nil {
			return fmt.Errorf("validation failed: prevATX not found: %v", err)
		}
		if !prevATX.Valid {
			return fmt.Errorf("validation failed: prevATX not valid")
		}
		if prevATX.Sequence+1 != atx.Sequence {
			return fmt.Errorf("sequence number is not one more than prev sequence number")
		}

		prevAtxIds, err := db.GetNodeAtxIds(atx.NodeId)
		if len(prevAtxIds) > 0 {
			lastAtx := prevAtxIds[len(prevAtxIds)-1]
			// last atx is not the one referenced
			if lastAtx != atx.PrevATXId {
				return fmt.Errorf("last atx is not the one referenced")
			}
		}

	} else {
		if atx.Sequence != 0 {
			return fmt.Errorf("no prevATX reported, but sequence number not zero")
		}
		prevAtxIds, err := db.GetNodeAtxIds(atx.NodeId)
		if err == nil && len(prevAtxIds) > 0 {
			var ids []string
			for _, x := range prevAtxIds {
				ids = append(ids, x.String()[2:7])
			}
			return fmt.Errorf("no prevATX reported, but other ATXs with same nodeID (%v) found, atxs: %v", atx.NodeId.Key[:5], ids)
		}
	}

	if atx.PositioningAtx != *types.EmptyAtxId {
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
		publicationEpoch := atx.PubLayerIdx.GetEpoch(uint16(db.LayersPerEpoch))
		if !publicationEpoch.IsGenesis() {
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
	db.log.Info("NIPST challenge: %v, OK nipst %v", hash.ShortString(), atx.NIPSTChallenge.String())

	if err = db.nipstValidator.Validate(atx.Nipst, *hash); err != nil {
		return fmt.Errorf("NIPST not valid: %v", err)
	}

	return nil
}

// StoreAtx stores an atx for epoh ech, it stores atx for the current epoch and adds the atx for the nodeid
// that created it in a sorted manner by the sequence id. this function does not validate the atx and assumes all data is correct
// and that all associated atx exist in the db. will return error if writing to db failed
func (db *ActivationDb) StoreAtx(ech types.EpochId, atx *types.ActivationTx) error {
	db.Lock()
	defer db.Unlock()

	//todo: maybe cleanup DB if failed by using defer
	if b, err := db.atxs.Get(atx.Id().Bytes()); err == nil && len(b) > 0 {
		// exists - how should we handle this?
		return nil
	}

	err := db.storeAtxUnlocked(atx)
	if err != nil {
		return err
	}

	err = db.addAtxToEpoch(ech, atx.Id())
	if err != nil {
		return err
	}

	if atx.Valid {
		err := db.incValidAtxCounter(ech)
		if err != nil {
			return err
		}
		db.log.Debug("after incrementing epoch counter ech (%v)", ech)
	}
	err = db.addAtxToNodeIdSorted(atx.NodeId, atx)
	if err != nil {
		return err
	}
	db.log.Debug("finished storing atx %v, in epoch %v", atx.ShortId(), ech)

	return nil
}

func (db *ActivationDb) storeAtxUnlocked(atx *types.ActivationTx) error {
	b, err := types.AtxAsBytes(atx)
	if err != nil {
		return err
	}
	err = db.atxs.Put(atx.Id().Bytes(), b)
	if err != nil {
		return err
	}
	return nil
}

func epochCounterKey(ech types.EpochId) []byte {
	return append(ech.ToBytes(), common.Uint64ToBytes(uint64(CounterKey))...)
}

// incValidAtxCounter increases the number of active ids seen for epoch ech
func (db *ActivationDb) incValidAtxCounter(ech types.EpochId) error {
	key := epochCounterKey(ech)
	val, err := db.atxs.Get(key)
	if err != nil {
		return db.atxs.Put(key, common.Uint32ToBytes(1))
	}
	return db.atxs.Put(key, common.Uint32ToBytes(common.BytesToUint32(val)+1))
}

// ActiveSetSize returns the active set size stored in db for epoch ech
func (db *ActivationDb) ActiveSetSize(ech types.EpochId) uint32 {
	key := epochCounterKey(ech)
	db.RLock()
	val, err := db.atxs.Get(key)
	db.RUnlock()
	if err != nil {
		//0 is not a valid active set size
		return 0
	}
	return common.BytesToUint32(val)
}

// addAtxToEpoch adds atx to epoch epochId
// this function is not thread safe and needs to be called under a global lock
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

// addAtxToNodeIdSorted inserts activation atx id by node it is not thread safe and needs to be called under a global lock
func (db *ActivationDb) addAtxToNodeIdSorted(nodeId types.NodeId, atx *types.ActivationTx) error {
	key := getNodeIdKey(nodeId)
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

	// append needs to be sorted
	atxs = append(atxs, atx.Id())
	l := len(atxs)
	if l > 1 {
		lastAtx, err := db.getAtxUnlocked(atxs[l-2])
		if err != nil {
			return errors.New("could not get atx from database ")
		}
		if atx.Sequence < lastAtx.Sequence {
			sort.Slice(atxs, func(i, j int) bool {
				atx1, err := db.getAtxUnlocked(atxs[i])
				if err != nil {
					panic("inconsistent db")
				}
				atx2, err := db.getAtxUnlocked(atxs[j])
				if err != nil {
					panic("inconsistent db")
				}

				return atx1.Sequence < atx2.Sequence
			})
		}
	}

	w, err := encodeAtxIds(atxs)
	if err != nil {
		return errors.New("could not encode layer atx ids")
	}
	return db.atxs.Put(key, w)
}

// GetNodeAtxIds returns the atx ids that were received for node nodeId
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

// GetEpochAtxIds returns all atx id's created in this epoch as seen by this node
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

// getAtxUnlocked gets the atx from db, this function is not thread safe and should be called under db lock
// this function returns a pointer to an atx and an error if failed to retrieve it
func (db *ActivationDb) getAtxUnlocked(id types.AtxId) (*types.ActivationTx, error) {
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

// GetAtx returns the atx by the given id. this function is thread safe and will return error if the id is not found in the
// atx db
func (db *ActivationDb) GetAtx(id types.AtxId) (*types.ActivationTx, error) {
	if id == *types.EmptyAtxId {
		return nil, nil
	}

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

// IsIdentityActive returns whether edId is active for the epoch of layer layer.
// it returns error if no associated atx is found in db
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
	return atx.Valid && atx.PubLayerIdx.GetEpoch(uint16(db.LayersPerEpoch)) == epoch, err
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
