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
	"sort"
	"sync"
)

const CounterKey = 0xaaaa

type ActivationDb struct {
	sync.RWMutex
	//todo: think about whether we need one db or several
	atxs            database.DB
	nipsts          database.DB
	nipstLock       sync.RWMutex
	atxCache        AtxCache
	meshDb          *mesh.MeshDB
	LayersPerEpoch  uint16
	nipstValidator  NipstValidator
	ids             IdStore
	log             log.Log
	processAtxMutex sync.Mutex
}

func NewActivationDb(dbstore database.DB, nipstStore database.DB, idstore IdStore, meshDb *mesh.MeshDB, layersPerEpoch uint16, nipstValidator NipstValidator, log log.Log) *ActivationDb {
	return &ActivationDb{atxs: dbstore, nipsts: nipstStore, atxCache: NewAtxCache(20), meshDb: meshDb, nipstValidator: nipstValidator, LayersPerEpoch: layersPerEpoch, ids: idstore, log: log}
}

// ProcessAtx validates the active set size declared in the atx, and contextually validates the atx according to atx
// validation rules it then stores the atx with flag set to validity of the atx.
//
// ATXs received as input must be already syntactically valid. Only contextual validation is performed.
func (db *ActivationDb) ProcessAtx(atx *types.ActivationTx) {
	db.log.Info("waiting for mutex, atx %v", atx.ShortId())
	db.processAtxMutex.Lock()
	defer db.processAtxMutex.Unlock()

	db.log.Info("aquired mutex, atx %v", atx.ShortId())
	eatx, _ := db.GetAtx(atx.Id())
	if eatx != nil {
		db.log.Info("at ProcessAtx, atx %v already in DB", atx.ShortId())
		atx.Nipst = nil
		return
	}
	epoch := atx.PubLayerIdx.GetEpoch(db.LayersPerEpoch)
	db.log.Info("processing atx id %v, pub-epoch %v node: %v layer %v", atx.ShortId(), epoch, atx.NodeId.Key[:5], atx.PubLayerIdx)
	err := db.ContextuallyValidateAtx(atx)
	if err != nil {
		db.log.Error("ATX %v failed contextual validation: %v", atx.ShortId(), err)
	} else {
		db.log.Info("ATX %v is valid", atx.ShortId())
	}
	err = db.StoreAtx(epoch, atx)
	if err != nil {
		db.log.Error("cannot store atx: %v", atx)
	}

	err = db.ids.StoreNodeIdentity(atx.NodeId)
	if err != nil {
		db.log.Error("cannot store node identity: %v err=%v", atx.NodeId, err)
	}
}

// CalcActiveSetFromView traverses the view found in a - the activation tx and counts number of active ids published
// in the epoch prior to the epoch that a was published at, this number is the number of active ids in the next epoch
// the function returns error if the view is not found
func (db *ActivationDb) CalcActiveSetFromView(a *types.ActivationTx) (uint32, error) {
	viewBytes, err := types.ViewAsBytes(a.View)
	if err != nil {
		return 0, err
	}

	count, found := activesetCache.Get(common.BytesToHash(viewBytes))
	if found {
		return count, nil
	}

	var counter uint32 = 0
	set := make(map[types.AtxId]struct{})
	pubEpoch := a.PubLayerIdx.GetEpoch(db.LayersPerEpoch)
	if pubEpoch < 1 {
		return 0, fmt.Errorf("publication epoch cannot be less than 1, found %v", pubEpoch)
	}
	countingEpoch := pubEpoch - 1
	firstLayerOfLastEpoch := types.LayerID(countingEpoch) * types.LayerID(db.LayersPerEpoch)

	traversalFunc := func(blkh *types.BlockHeader) error {
		blk, err := db.meshDb.GetBlock(blkh.Id)
		if err != nil {
			db.log.Error("cannot validate atx, block %v not found", blkh.Id)
			return err
		}
		//skip blocks not from atx epoch
		if blk.LayerIndex.GetEpoch(db.LayersPerEpoch) != countingEpoch {
			return nil
		}
		for _, id := range blk.AtxIds {
			if _, found := set[id]; found {
				continue
			}
			set[id] = struct{}{}
			atx, err := db.GetAtx(id)
			if err != nil {
				log.Panic("error fetching atx %v from database -- inconsistent state", id.ShortId()) // TODO: handle inconsistent state
				return fmt.Errorf("error fetching atx %v from database -- inconsistent state", id.ShortId())
			}
			if atx.TargetEpoch(db.LayersPerEpoch) != pubEpoch {
				db.log.Debug("atx %v found, but targeting epoch %v instead of publication epoch %v",
					atx.ShortId(), atx.TargetEpoch(db.LayersPerEpoch), pubEpoch)
				continue
			}
			counter++
			db.log.Debug("atx %v (epoch %d) found traversing in block %x (epoch %d)",
				atx.ShortId(), atx.TargetEpoch(db.LayersPerEpoch), blk.Id, blk.LayerIndex.GetEpoch(db.LayersPerEpoch))
		}
		return nil
	}

	mp := map[types.BlockID]struct{}{}
	for _, blk := range a.View {
		mp[blk] = struct{}{}
	}

	err = db.meshDb.ForBlockInView(mp, firstLayerOfLastEpoch, traversalFunc)
	if err != nil {
		return 0, err
	}
	activesetCache.Add(common.BytesToHash(viewBytes), counter)

	return counter, nil

}

// SyntacticallyValidateAtx ensures the following conditions apply, otherwise it returns an error.
//
// - If the sequence number is non-zero: PrevATX points to a syntactically valid ATX whose sequence number is one less
//   than the current ATX's sequence number.
// - If the sequence number is zero: PrevATX is empty.
// - Positioning ATX points to a syntactically valid ATX.
// - NIPST challenge is a hash of the serialization of the following fields:
//   NodeID, SequenceNumber, PrevATXID, LayerID, StartTick, PositioningATX.
// - The NIPST is valid.
// - ATX LayerID is NipstLayerTime or less after the PositioningATX LayerID.
// - The ATX view of the previous epoch contains ActiveSetSize activations.
func (db *ActivationDb) SyntacticallyValidateAtx(atx *types.ActivationTx) error {
	if atx.PrevATXId != *types.EmptyAtxId {
		prevATX, err := db.GetAtx(atx.PrevATXId)
		if err != nil {
			return fmt.Errorf("validation failed: prevATX not found: %v", err)
		}
		if prevATX.NodeId.Key != atx.NodeId.Key {
			return fmt.Errorf("previous ATX belongs to different miner. atx.Id: %v, atx.NodeId: %v, prevAtx.NodeId: %v",
				atx.ShortId(), atx.NodeId.Key, prevATX.NodeId.Key)
		}
		if prevATX.Sequence+1 != atx.Sequence {
			return fmt.Errorf("sequence number is not one more than prev sequence number")
		}
	} else {
		if atx.Sequence != 0 {
			return fmt.Errorf("no prevATX reported, but sequence number not zero")
		}
	}

	if atx.PositioningAtx != *types.EmptyAtxId {
		posAtx, err := db.GetAtx(atx.PositioningAtx)
		if err != nil {
			return fmt.Errorf("positioning atx not found")
		}
		if atx.PubLayerIdx <= posAtx.PubLayerIdx {
			return fmt.Errorf("atx layer (%v) must be after positioning atx layer (%v)",
				atx.PubLayerIdx, posAtx.PubLayerIdx)
		}
		if uint64(atx.PubLayerIdx-posAtx.PubLayerIdx) > uint64(db.LayersPerEpoch) {
			return fmt.Errorf("expected distance of one epoch (%v layers) from pos ATX but found %v",
				db.LayersPerEpoch, atx.PubLayerIdx-posAtx.PubLayerIdx)
		}
	} else {
		publicationEpoch := atx.PubLayerIdx.GetEpoch(db.LayersPerEpoch)
		if !publicationEpoch.IsGenesis() {
			return fmt.Errorf("no positioning atx found")
		}
	}

	activeSet, err := db.CalcActiveSetFromView(atx)
	if err != nil && !atx.PubLayerIdx.GetEpoch(db.LayersPerEpoch).IsGenesis() {
		return fmt.Errorf("could not calculate active set for ATX %v", atx.ShortId())
	}

	if atx.ActiveSetSize != activeSet {
		return fmt.Errorf("atx contains view with unequal active ids (%v) than seen (%v)", atx.ActiveSetSize, activeSet)
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

// ContextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (db *ActivationDb) ContextuallyValidateAtx(atx *types.ActivationTx) error {
	if atx.PrevATXId != *types.EmptyAtxId {
		prevAtxIds, err := db.GetNodeAtxIds(atx.NodeId)
		if err != nil {
			db.log.WithFields(
				log.String("atx_id", atx.ShortId()), log.String("node_id", atx.NodeId.Key)).
				Error("could not fetch node ATXs: %v", err)
			return fmt.Errorf("could not fetch node ATXs: %v", err)
		}
		if len(prevAtxIds) == 0 {
			return fmt.Errorf("ATX references a previous ATX, but no ATXs were found for this miner ID")
		}
		lastAtx := prevAtxIds[len(prevAtxIds)-1]
		// last atx is not the one referenced
		if lastAtx != atx.PrevATXId {
			return fmt.Errorf("last atx is not the one referenced")
		}
	} else {
		prevAtxIds, err := db.GetNodeAtxIds(atx.NodeId)
		if err != nil && err != database.ErrNotFound {
			db.log.Error("fetching ATX ids failed: %v", err)
			return err
		}
		if len(prevAtxIds) > 0 {
			var ids []string
			for _, x := range prevAtxIds {
				ids = append(ids, x.ShortString())
			}
			return fmt.Errorf("no prevATX reported, but other ATXs with same nodeID (%v) found, atxs: %v", atx.NodeId.Key[:5], ids)
		}
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

	err = db.incValidAtxCounter(ech)
	if err != nil {
		return err
	}
	err = db.addAtxToNodeIdSorted(atx.NodeId, atx)
	if err != nil {
		return err
	}
	db.log.Debug("finished storing atx %v, in epoch %v", atx.ShortId(), ech)

	return nil
}

func (db *ActivationDb) storeAtxUnlocked(atx *types.ActivationTx) error {
	b, err := types.InterfaceToBytes(atx.Nipst)
	if err != nil {
		return err
	}
	db.nipstLock.Lock()
	err = db.nipsts.Put(atx.Id().Bytes(), b)
	db.nipstLock.Unlock()
	if err != nil {
		return err
	}
	//todo: think of how to break down the object better
	atx.Nipst = nil

	b, err = types.AtxAsBytes(atx)
	if err != nil {
		return err
	}

	err = db.atxs.Put(atx.Id().Bytes(), b)
	if err != nil {
		return err
	}
	return nil
}

func (db *ActivationDb) GetNipst(atxId types.AtxId) (*types.NIPST, error) {
	db.nipstLock.RLock()
	bts, err := db.nipsts.Get(atxId.Bytes())
	db.nipstLock.RUnlock()
	if err != nil {
		return nil, err
	}
	npst := types.NIPST{}
	err = types.BytesToInterface(bts, &npst)
	if err != nil {
		return nil, err
	}
	return &npst, nil
}

func epochCounterKey(ech types.EpochId) []byte {
	return append(ech.ToBytes(), common.Uint64ToBytes(uint64(CounterKey))...)
}

// incValidAtxCounter increases the number of active ids seen for epoch ech. Use only under db.lock.
func (db *ActivationDb) incValidAtxCounter(ech types.EpochId) error {
	key := epochCounterKey(ech)
	val, err := db.atxs.Get(key)
	if err != nil {
		db.log.Debug("incrementing epoch %v ATX counter to 1", ech)
		return db.atxs.Put(key, common.Uint32ToBytes(1))
	}
	db.log.Debug("incrementing epoch %v ATX counter to %v", ech, common.BytesToUint32(val)+1)
	return db.atxs.Put(key, common.Uint32ToBytes(common.BytesToUint32(val)+1))
}

// ActiveSetSize returns the active set size stored in db for epoch ech
func (db *ActivationDb) ActiveSetSize(epochId types.EpochId) (uint32, error) {
	key := epochCounterKey(epochId)
	db.RLock()
	val, err := db.atxs.Get(key)
	db.RUnlock()
	if err != nil {
		return 0, fmt.Errorf("could not fetch active set size from cache: %v", err)
	}
	return common.BytesToUint32(val), nil
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
	db.log.Debug("fetching atxIDs for node %v", nodeId.Key[:5])

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
		return nil, errors.New("trying to fetch empty atx id")
	}

	if atx, gotIt := db.atxCache.Get(id); gotIt {
		return atx, nil
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
	db.atxCache.Add(id, atx)
	return atx, nil
}

// IsIdentityActive returns whether edId is active for the epoch of layer layer.
// it returns error if no associated atx is found in db
func (db *ActivationDb) IsIdentityActive(edId string, layer types.LayerID) (bool, types.AtxId, error) {
	// TODO: genesis flow should decide what we want to do here
	if layer.GetEpoch(db.LayersPerEpoch) == 0 {
		return true, *types.EmptyAtxId, nil
	}

	epoch := layer.GetEpoch(db.LayersPerEpoch)
	nodeId, err := db.ids.GetIdentity(edId)
	if err != nil { // means there is no such identity
		db.log.Error("IsIdentityActive erred while getting identity err=%v", err)
		return false, *types.EmptyAtxId, nil
	}
	ids, err := db.GetNodeAtxIds(nodeId)
	if err != nil {
		db.log.Error("IsIdentityActive erred while getting node atx ids err=%v", err)
		return false, *types.EmptyAtxId, err
	}
	if len(ids) == 0 { // GetIdentity succeeded but no ATXs, this is a fatal error
		return false, *types.EmptyAtxId, fmt.Errorf("no active IDs for known node")
	}

	atx, err := db.GetAtx(ids[len(ids)-1])
	if err != nil {
		db.log.Error("IsIdentityActive erred while getting atx err=%v", err)
		return false, *types.EmptyAtxId, nil
	}

	lastAtxTargetEpoch := atx.TargetEpoch(db.LayersPerEpoch)
	if lastAtxTargetEpoch < epoch {
		db.log.Info("IsIdentityActive latest atx is too old expected=%v actual=%v atxid=%v",
			epoch, lastAtxTargetEpoch, atx.ShortId())
		return false, *types.EmptyAtxId, nil
	}

	if lastAtxTargetEpoch > epoch {
		// This could happen if we already published the ATX for the next epoch, so we check the previous one as well
		if len(ids) < 2 {
			db.log.Info("IsIdentityActive latest atx is too new but no previous atx len(ids)=%v", len(ids))
			return false, *types.EmptyAtxId, nil
		}
		atx, err = db.GetAtx(ids[len(ids)-2])
		if err != nil {
			db.log.Error("IsIdentityActive erred while getting atx for second newest err=%v", err)
			return false, *types.EmptyAtxId, nil
		}
	}

	// lastAtxTargetEpoch = epoch
	return atx.TargetEpoch(db.LayersPerEpoch) == epoch, atx.Id(), nil
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
