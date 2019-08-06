package activation

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/sha256-simd"
	"sort"
	"sync"
)

const topAtxKey = "topAtxKey"

type ActivationDb struct {
	sync.RWMutex
	//todo: think about whether we need one db or several
	IdStore
	atxs            database.DB
	atxCache        AtxCache
	meshDb          *mesh.MeshDB
	LayersPerEpoch  uint16
	nipstValidator  NipstValidator
	log             log.Log
	processAtxMutex sync.Mutex
}

func NewActivationDb(dbstore database.DB, idstore IdStore, meshDb *mesh.MeshDB, layersPerEpoch uint16, nipstValidator NipstValidator, log log.Log) *ActivationDb {
	return &ActivationDb{atxs: dbstore, atxCache: NewAtxCache(20), meshDb: meshDb, nipstValidator: nipstValidator, LayersPerEpoch: layersPerEpoch, IdStore: idstore, log: log}
}

// ProcessAtx validates the active set size declared in the atx, and contextually validates the atx according to atx
// validation rules it then stores the atx with flag set to validity of the atx.
//
// ATXs received as input must be already syntactically valid. Only contextual validation is performed.
func (db *ActivationDb) ProcessAtx(atx *types.ActivationTx) {
	db.processAtxMutex.Lock()
	defer db.processAtxMutex.Unlock()

	eatx, _ := db.GetAtx(atx.Id())
	if eatx != nil {
		return
	}
	epoch := atx.PubLayerIdx.GetEpoch(db.LayersPerEpoch)
	db.log.With().Info("processing atx", log.AtxId(atx.ShortId()), log.EpochId(uint64(epoch)),
		log.NodeId(atx.NodeId.Key[:5]), log.LayerId(uint64(atx.PubLayerIdx)))
	err := db.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	if err != nil {
		db.log.With().Error("ATX failed contextual validation", log.AtxId(atx.ShortId()), log.Err(err))
	} else {
		db.log.With().Info("ATX is valid", log.AtxId(atx.ShortId()))
	}
	err = db.StoreAtx(epoch, atx)
	if err != nil {
		db.log.With().Error("cannot store atx", log.AtxId(atx.ShortId()), log.Err(err))
	}

	err = db.StoreNodeIdentity(atx.NodeId)
	if err != nil {
		db.log.With().Error("cannot store node identity", log.NodeId(atx.NodeId.ShortString()), log.AtxId(atx.ShortId()), log.Err(err))
	}
}

func (db *ActivationDb) createTraversalActiveSetCounterFunc(countedAtxs map[string]types.AtxId, penalties map[string]struct{}, layersPerEpoch uint16, epoch types.EpochId) func(b *types.Block) error {

	traversalFunc := func(b *types.Block) error {

		// don't count ATXs in blocks that are not destined to the prev epoch
		if b.LayerIndex.GetEpoch(db.LayersPerEpoch) != epoch-1 {
			return nil
		}

		// count unique ATXs
		for _, id := range b.AtxIds {
			atx, err := db.GetAtx(id)
			if err != nil {
				log.Panic("error fetching atx %v from database -- inconsistent state", id.ShortId()) // TODO: handle inconsistent state
				return fmt.Errorf("error fetching atx %v from database -- inconsistent state", id.ShortId())
			}

			// make sure the target epoch is our epoch
			if atx.TargetEpoch(layersPerEpoch) != epoch {
				db.log.With().Debug("atx found, but targeting epoch doesn't match publication epoch",
					log.String("atx_id", atx.ShortId()),
					log.Uint64("atx_target_epoch", uint64(atx.TargetEpoch(layersPerEpoch))),
					log.Uint64("actual_epoch", uint64(epoch)))
				continue
			}

			// ignore atx from nodes in penalty
			if _, exist := penalties[atx.NodeId.Key]; exist {
				db.log.With().Debug("ignoring atx from node in penalty",
					log.String("node_id", atx.NodeId.Key), log.String("atx_id", atx.ShortId()))
				continue
			}

			if prevId, exist := countedAtxs[atx.NodeId.Key]; exist { // same miner

				if prevId != id { // different atx for same epoch
					db.log.With().Error("Encountered second atx for the same miner on the same epoch",
						log.String("first_atx", prevId.ShortId()), log.String("second_atx", id.ShortId()))

					penalties[atx.NodeId.Key] = struct{}{} // mark node in penalty
					delete(countedAtxs, atx.NodeId.Key)    // remove the penalized node from counted
				}
				continue
			}

			countedAtxs[atx.NodeId.Key] = id
		}

		return nil
	}

	return traversalFunc
}

// CalcActiveSetSize - returns the active set size that matches the view of the contextually valid blocks in the provided layer
func (db *ActivationDb) CalcActiveSetSize(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {

	if epoch == 0 {
		return nil, errors.New("tried to retrieve active set for epoch 0")
	}

	firstLayerOfPrevEpoch := (epoch - 1).FirstLayer(db.LayersPerEpoch)

	countedAtxs := make(map[string]types.AtxId)
	penalties := make(map[string]struct{})

	traversalFunc := db.createTraversalActiveSetCounterFunc(countedAtxs, penalties, db.LayersPerEpoch, epoch)

	err := db.meshDb.ForBlockInView(blocks, firstLayerOfPrevEpoch, traversalFunc)
	if err != nil {
		return nil, err
	}

	result := make(map[string]struct{}, len(countedAtxs))
	for k := range countedAtxs {
		result[k] = struct{}{}
	}

	return result, nil
}

// CalcActiveSetFromView traverses the view found in a - the activation tx and counts number of active ids published
// in the epoch prior to the epoch that a was published at, this number is the number of active ids in the next epoch
// the function returns error if the view is not found
func (db *ActivationDb) CalcActiveSetFromView(view []types.BlockID, pubEpoch types.EpochId) (uint32, error) {
	viewHash, err := calcSortedViewHash(view)
	if err != nil {
		return 0, fmt.Errorf("failed to calc sorted view hash: %v", err)
	}
	count, found := activesetCache.Get(viewHash)
	if found {
		return count, nil
	}

	if pubEpoch < 1 {
		return 0, fmt.Errorf("publication epoch cannot be less than 1, found %v", pubEpoch)
	}

	mp := map[types.BlockID]struct{}{}
	for _, blk := range view {
		mp[blk] = struct{}{}
	}

	countedAtxs, err := db.CalcActiveSetSize(pubEpoch, mp)
	if err != nil {
		return 0, err
	}
	activesetCache.Add(viewHash, uint32(len(countedAtxs)))

	return uint32(len(countedAtxs)), nil

}

func calcSortedViewHash(view []types.BlockID) (common.Hash, error) {
	sortedView := make([]types.BlockID, len(view))
	copy(sortedView, view)
	sort.Slice(sortedView, func(i, j int) bool {
		return sortedView[i] < sortedView[j]
	})
	viewBytes, err := types.ViewAsBytes(sortedView)
	if err != nil {
		return common.Hash{}, err
	}
	viewHash := sha256.Sum256(viewBytes)
	return viewHash, err
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

	activeSet, err := db.CalcActiveSetFromView(atx.View, atx.PubLayerIdx.GetEpoch(db.LayersPerEpoch))
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
	db.log.With().Info("Validated NIPST", log.String("challenge_hash", hash.ShortString()))

	if err = db.nipstValidator.Validate(atx.Nipst, *hash); err != nil {
		return fmt.Errorf("NIPST not valid: %v", err)
	}

	return nil
}

// ContextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (db *ActivationDb) ContextuallyValidateAtx(atx *types.ActivationTxHeader) error {
	if atx.PrevATXId != *types.EmptyAtxId {
		lastAtx, err := db.GetNodeLastAtxId(atx.NodeId)
		if err != nil {
			db.log.With().Error("could not fetch node last ATX",
				log.AtxId(atx.ShortId()), log.NodeId(atx.NodeId.ShortString()), log.Err(err))
			return fmt.Errorf("could not fetch node last ATX: %v", err)
		}
		// last atx is not the one referenced
		if lastAtx != atx.PrevATXId {
			return fmt.Errorf("last atx is not the one referenced")
		}
	} else {
		lastAtx, err := db.GetNodeLastAtxId(atx.NodeId)
		if err != nil && err != database.ErrNotFound {
			db.log.Error("fetching ATX ids failed: %v", err)
			return err
		}
		if err == nil { // we found an ATX for this node ID, although it reported no prevATX -- this is invalid
			return fmt.Errorf("no prevATX reported, but other ATX with same nodeID (%v) found: %v",
				atx.NodeId.ShortString(), lastAtx.ShortString())
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

	err = db.updateTopAtxIfNeeded(atx)
	if err != nil {
		return err
	}

	err = db.addAtxToNodeId(atx.NodeId, atx)
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

type atxIdAndLayer struct {
	AtxId   types.AtxId
	LayerId types.LayerID
}

// updateTopAtxIfNeeded replaces the top ATX (positioning ATX candidate) if the latest ATX has a higher layer ID.
// This function is not thread safe and needs to be called under a global lock.
func (db *ActivationDb) updateTopAtxIfNeeded(atx *types.ActivationTx) error {
	currentTopAtx, err := db.getTopAtx()
	if err != nil && err != database.ErrNotFound {
		return fmt.Errorf("failed to get current ATX: %v", err)
	}
	if err == nil && currentTopAtx.LayerId >= atx.PubLayerIdx {
		return nil
	}

	newTopAtx := atxIdAndLayer{
		AtxId:   atx.Id(),
		LayerId: atx.PubLayerIdx,
	}
	topAtxBytes, err := types.InterfaceToBytes(&newTopAtx)
	if err != nil {
		return fmt.Errorf("failed to marshal top ATX: %v", err)
	}

	err = db.atxs.Put([]byte(topAtxKey), topAtxBytes)
	if err != nil {
		return fmt.Errorf("failed to store top ATX: %v", err)
	}
	return nil
}

func (db ActivationDb) getTopAtx() (atxIdAndLayer, error) {
	topAtxBytes, err := db.atxs.Get([]byte(topAtxKey))
	if err != nil {
		return atxIdAndLayer{}, err
	}
	var topAtx atxIdAndLayer
	err = types.BytesToInterface(topAtxBytes, &topAtx)
	if err != nil {
		return atxIdAndLayer{}, fmt.Errorf("failed to unmarshal top ATX: %v", err)
	}
	return topAtx, nil
}

func getNodeIdKey(id types.NodeId) []byte {
	return []byte(id.Key)
}

// addAtxToNodeId inserts activation atx id by node
func (db *ActivationDb) addAtxToNodeId(nodeId types.NodeId, atx *types.ActivationTx) error {
	key := getNodeIdKey(nodeId)
	err := db.atxs.Put(key, atx.Id().Bytes())
	if err != nil {
		return fmt.Errorf("failed to store ATX ID for node: %v", err)
	}
	return nil
}

// GetNodeLastAtxId returns the last atx id that was received for node nodeId
func (db *ActivationDb) GetNodeLastAtxId(nodeId types.NodeId) (types.AtxId, error) {
	key := getNodeIdKey(nodeId)
	db.log.Debug("fetching atxIDs for node %v", nodeId.ShortString())

	id, err := db.atxs.Get(key)
	if err != nil {
		return *types.EmptyAtxId, err
	}
	return types.AtxId{Hash: common.BytesToHash(id)}, nil
}

// GetPosAtxId returns the best (highest layer id), currently known to this node, pos atx id
func (db *ActivationDb) GetPosAtxId(epochId types.EpochId) (*types.AtxId, error) {
	idAndLayer, err := db.getTopAtx()
	if err != nil {
		return types.EmptyAtxId, err
	}
	if idAndLayer.LayerId.GetEpoch(db.LayersPerEpoch) != epochId {
		return types.EmptyAtxId, fmt.Errorf("current posAtx (epoch %v) does not belong to the requested epoch (%v)",
			idAndLayer.LayerId.GetEpoch(db.LayersPerEpoch), epochId)
	}
	return &idAndLayer.AtxId, nil
}

// getAtxUnlocked gets the atx from db, this function is not thread safe and should be called under db lock
// this function returns a pointer to an atx and an error if failed to retrieve it
func (db *ActivationDb) getAtxUnlocked(id types.AtxId) (*types.ActivationTx, error) {
	if atx, gotIt := db.atxCache.Get(id); gotIt {
		return atx, nil
	}
	b, err := db.atxs.Get(id.Bytes())
	if err != nil {
		return nil, err
	}
	atx, err := types.BytesAsAtx(b, &id)
	if err != nil {
		return nil, err
	}
	db.atxCache.Add(id, atx)
	return atx, nil
}

// GetAtx returns the atx by the given id. this function is thread safe and will return error if the id is not found in the
// atx db
func (db *ActivationDb) GetAtx(id types.AtxId) (*types.ActivationTxHeader, error) {
	atx, err := db.GetFullAtx(id)
	if err != nil {
		return nil, err
	}
	return &atx.ActivationTxHeader, nil
}

func (db *ActivationDb) GetFullAtx(id types.AtxId) (*types.ActivationTx, error) {
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
	atx, err := types.BytesAsAtx(b, &id)
	if err != nil {
		return nil, err
	}
	db.atxCache.Add(id, atx)
	return atx, nil
}

// IsIdentityActive returns whether edId is active for the epoch of layer layer.
// it returns error if no associated atx is found in db
func (db *ActivationDb) IsIdentityActive(edId string, layer types.LayerID) (*types.NodeId, bool, types.AtxId, error) {
	// TODO: genesis flow should decide what we want to do here
	if layer.GetEpoch(db.LayersPerEpoch) == 0 {
		return nil, true, *types.EmptyAtxId, nil
	}

	epoch := layer.GetEpoch(db.LayersPerEpoch)
	nodeId, err := db.GetIdentity(edId)
	if err != nil { // means there is no such identity
		db.log.Error("IsIdentityActive erred while getting identity err=%v", err)
		return nil, false, *types.EmptyAtxId, nil
	}
	atxId, err := db.GetNodeLastAtxId(nodeId)
	if err != nil {
		db.log.Error("IsIdentityActive erred while getting last node atx id err=%v", err)
		return nil, false, *types.EmptyAtxId, err
	}
	atx, err := db.GetAtx(atxId)
	if err != nil {
		db.log.With().Error("IsIdentityActive erred while getting atx", log.AtxId(atxId.ShortId()), log.Err(err))
		return nil, false, *types.EmptyAtxId, nil
	}

	lastAtxTargetEpoch := atx.TargetEpoch(db.LayersPerEpoch)
	if lastAtxTargetEpoch < epoch {
		db.log.With().Info("IsIdentityActive latest atx is too old", log.Uint64("expected", uint64(epoch)),
			log.Uint64("actual", uint64(lastAtxTargetEpoch)), log.AtxId(atx.ShortId()))
		return nil, false, *types.EmptyAtxId, nil
	}

	if lastAtxTargetEpoch > epoch {
		// This could happen if we already published the ATX for the next epoch, so we check the previous one as well
		if atx.PrevATXId == *types.EmptyAtxId {
			db.log.With().Info("IsIdentityActive latest atx is too new but no previous atx", log.AtxId(atxId.ShortId()))
			return nil, false, *types.EmptyAtxId, nil
		}
		prevAtxId := atx.PrevATXId
		atx, err = db.GetAtx(prevAtxId)
		if err != nil {
			db.log.With().Error("IsIdentityActive erred while getting atx for second newest", log.AtxId(prevAtxId.ShortId()), log.Err(err))
			return nil, false, *types.EmptyAtxId, nil
		}
	}

	// lastAtxTargetEpoch = epoch
	return &nodeId, atx.TargetEpoch(db.LayersPerEpoch) == epoch, atx.Id(), nil
}
