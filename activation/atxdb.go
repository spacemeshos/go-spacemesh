package activation

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/signing"
	"sync"
	"time"
)

const topAtxKey = "topAtxKey"

func getNodeAtxKey(nodeID types.NodeId, targetEpoch types.EpochId) []byte {
	return append(getNodeAtxPrefix(nodeID), util.Uint64ToBytesBigEndian(uint64(targetEpoch))...)
}

func getNodeAtxPrefix(nodeID types.NodeId) []byte {
	return []byte(fmt.Sprintf("n_%v_", nodeID.Key))
}

func getAtxHeaderKey(atxID types.AtxId) []byte {
	return []byte(fmt.Sprintf("h_%v", atxID.Bytes()))
}

func getAtxBodyKey(atxID types.AtxId) []byte {
	return []byte(fmt.Sprintf("b_%v", atxID.Bytes()))
}

var errInvalidSig = fmt.Errorf("identity not found when validating signature, invalid atx")

type atxChan struct {
	ch        chan struct{}
	listeners int
}

// ActivationDb hold the atxs received from all nodes and their validity status
// it also stores identifications for all nodes e.g the coupling between ed id and bls id
type ActivationDb struct {
	sync.RWMutex
	//todo: think about whether we need one db or several(#1922)
	idStore
	atxs              database.Database
	atxHeaderCache    AtxCache
	meshDb            *mesh.MeshDB
	LayersPerEpoch    uint16
	nipstValidator    nipstValidator
	pendingActiveSet  map[types.Hash12]*sync.Mutex
	log               log.Log
	calcActiveSetFunc func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error)
	processAtxMutex   sync.Mutex
	assLock           sync.Mutex
	atxChannels       map[types.AtxId]*atxChan
}

// NewActivationDb creates a new struct of type ActivationDb, this struct will hold the atxs received from all nodes and
// their validity
func NewActivationDb(dbstore database.Database, idstore idStore, meshDb *mesh.MeshDB, layersPerEpoch uint16, nipstValidator nipstValidator, log log.Log) *ActivationDb {
	db := &ActivationDb{
		idStore:          idstore,
		atxs:             dbstore,
		atxHeaderCache:   NewAtxCache(600),
		meshDb:           meshDb,
		LayersPerEpoch:   layersPerEpoch,
		nipstValidator:   nipstValidator,
		pendingActiveSet: make(map[types.Hash12]*sync.Mutex),
		log:              log,
		atxChannels:      make(map[types.AtxId]*atxChan),
	}
	db.calcActiveSetFunc = db.CalcActiveSetSize
	return db
}

var closedChan = make(chan struct{})

func init() {
	close(closedChan)
}

// AwaitAtx returns a channel that will receive notification when the specified atx with id id is received via gossip
func (db *ActivationDb) AwaitAtx(id types.AtxId) chan struct{} {
	db.Lock()
	defer db.Unlock()

	if _, err := db.atxs.Get(getAtxHeaderKey(id)); err == nil {
		return closedChan
	}

	ch, found := db.atxChannels[id]
	if !found {
		ch = &atxChan{
			ch:        make(chan struct{}),
			listeners: 0,
		}
		db.atxChannels[id] = ch
	}
	ch.listeners++
	return ch.ch
}

// UnsubscribeAtx un subscribes the waiting for a specific atx with atx id id to arrive via gossip.
func (db *ActivationDb) UnsubscribeAtx(id types.AtxId) {
	db.Lock()
	defer db.Unlock()

	ch, found := db.atxChannels[id]
	if !found {
		return
	}
	ch.listeners--
	if ch.listeners < 1 {
		delete(db.atxChannels, id)
	}
}

// ProcessAtxs processes the list of given atxs using ProcessAtx method
func (db *ActivationDb) ProcessAtxs(atxs []*types.ActivationTx) error {
	seenMinerIds := map[string]struct{}{}
	for _, atx := range atxs {
		minerID := atx.NodeId.Key
		if _, found := seenMinerIds[minerID]; found {
			// TODO: Blacklist this miner
			// TODO: Ensure that these are two different, syntactically valid ATXs for the same epoch, otherwise the
			//  miner did nothing wrong
			db.log.With().Error("found miner with multiple ATXs published in same block",
				log.String("atx_node_id", atx.NodeId.ShortString()), log.AtxID(atx.ShortString()))
		}
		err := db.ProcessAtx(atx)
		if err != nil {
			return err
		}
	}
	return nil
}

// ProcessAtx validates the active set size declared in the atx, and contextually validates the atx according to atx
// validation rules it then stores the atx with flag set to validity of the atx.
//
// ATXs received as input must be already syntactically valid. Only contextual validation is performed.
func (db *ActivationDb) ProcessAtx(atx *types.ActivationTx) error {
	db.processAtxMutex.Lock()
	defer db.processAtxMutex.Unlock()

	eatx, _ := db.GetAtxHeader(atx.Id())
	if eatx != nil { // Already processed
		return nil
	}
	epoch := atx.PubLayerIdx.GetEpoch(db.LayersPerEpoch)
	db.log.With().Info("processing atx", log.AtxID(atx.ShortString()), log.EpochID(uint64(epoch)),
		log.String("atx_node_id", atx.NodeId.Key[:5]), log.LayerID(uint64(atx.PubLayerIdx)))
	err := db.ContextuallyValidateAtx(atx.ActivationTxHeader)
	if err != nil {
		db.log.With().Error("ATX failed contextual validation", log.AtxID(atx.ShortString()), log.Err(err))
		// TODO: Blacklist this miner
	} else {
		db.log.With().Info("ATX is valid", log.AtxID(atx.ShortString()))
	}
	err = db.StoreAtx(epoch, atx)
	if err != nil {
		return fmt.Errorf("cannot store atx %s: %v", atx.ShortString(), err)
	}

	err = db.StoreNodeIdentity(atx.NodeId)
	if err != nil {
		db.log.With().Error("cannot store node identity", log.String("atx_node_id", atx.NodeId.ShortString()), log.AtxID(atx.ShortString()), log.Err(err))
	}
	return nil
}

func (db *ActivationDb) createTraversalActiveSetCounterFunc(countedAtxs map[string]types.AtxId, penalties map[string]struct{}, layersPerEpoch uint16, epoch types.EpochId) func(b *types.Block) (bool, error) {

	traversalFunc := func(b *types.Block) (stop bool, err error) {

		// don't count ATXs in blocks that are not destined to the prev epoch
		if b.LayerIndex.GetEpoch(db.LayersPerEpoch) != epoch-1 {
			return false, nil
		}

		// count unique ATXs
		for _, id := range b.AtxIds {
			atx, err := db.GetAtxHeader(id)
			if err != nil {
				log.Panic("error fetching atx %v from database -- inconsistent state", id.ShortString()) // TODO: handle inconsistent state
				return false, fmt.Errorf("error fetching atx %v from database -- inconsistent state", id.ShortString())
			}

			// make sure the target epoch is our epoch
			if atx.TargetEpoch(layersPerEpoch) != epoch {
				db.log.With().Debug("atx found, but targeting epoch doesn't match publication epoch",
					log.String("atx_id", atx.ShortString()),
					log.Uint64("atx_target_epoch", uint64(atx.TargetEpoch(layersPerEpoch))),
					log.Uint64("actual_epoch", uint64(epoch)))
				continue
			}

			// ignore atx from nodes in penalty
			if _, exist := penalties[atx.NodeId.Key]; exist {
				db.log.With().Debug("ignoring atx from node in penalty",
					log.String("node_id", atx.NodeId.Key), log.String("atx_id", atx.ShortString()))
				continue
			}

			if prevID, exist := countedAtxs[atx.NodeId.Key]; exist { // same miner

				if prevID != id { // different atx for same epoch
					db.log.With().Error("Encountered second atx for the same miner on the same epoch",
						log.String("first_atx", prevID.ShortString()), log.String("second_atx", id.ShortString()))

					penalties[atx.NodeId.Key] = struct{}{} // mark node in penalty
					delete(countedAtxs, atx.NodeId.Key)    // remove the penalized node from counted
				}
				continue
			}

			countedAtxs[atx.NodeId.Key] = id
		}

		return false, nil
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

	startTime := time.Now()
	err := db.meshDb.ForBlockInView(blocks, firstLayerOfPrevEpoch, traversalFunc)
	if err != nil {
		return nil, err
	}
	db.log.With().Info("done calculating active set size", log.String("duration", time.Now().Sub(startTime).String()))

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
	if pubEpoch < 1 {
		return 0, fmt.Errorf("publication epoch cannot be less than 1, found %v", pubEpoch)
	}
	viewHash, err := types.CalcBlocksHash12(view)
	if err != nil {
		return 0, fmt.Errorf("failed to calc sorted view hash: %v", err)
	}
	count, found := activesetCache.Get(viewHash)
	if found {
		return count, nil
	}
	//check if we have a running calculation for this hash
	db.assLock.Lock()
	mu, alreadyRunning := db.pendingActiveSet[viewHash]
	if alreadyRunning {
		db.assLock.Unlock()
		// if there is a running calculation, wait for it to end and get the result
		mu.Lock()
		count, found := activesetCache.Get(viewHash)
		if found {
			mu.Unlock()
			return count, nil
		}
		// if not found, keep running mutex and calculate active set size
	} else {
		// if no running calc, insert new one
		mu = &sync.Mutex{}
		db.pendingActiveSet[viewHash] = mu
		db.pendingActiveSet[viewHash].Lock()
		db.assLock.Unlock()
	}

	mp := map[types.BlockID]struct{}{}
	for _, blk := range view {
		mp[blk] = struct{}{}
	}

	countedAtxs, err := db.calcActiveSetFunc(pubEpoch, mp)
	if err != nil {
		mu.Unlock()
		db.deleteLock(viewHash)
		return 0, err
	}
	activesetCache.Add(viewHash, uint32(len(countedAtxs)))
	mu.Unlock()
	db.deleteLock(viewHash)

	return uint32(len(countedAtxs)), nil

}

func (db *ActivationDb) deleteLock(viewHash types.Hash12) {
	db.assLock.Lock()
	if _, exist := db.pendingActiveSet[viewHash]; exist {
		delete(db.pendingActiveSet, viewHash)
	}
	db.assLock.Unlock()
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
	events.Publish(events.NewAtx{ID: atx.ShortString(), LayerID: uint64(atx.PubLayerIdx.GetEpoch(db.LayersPerEpoch))})
	pub, err := ExtractPublicKey(atx)
	if err != nil {
		return fmt.Errorf("cannot validate atx sig atx id %v err %v", atx.ShortString(), err)
	}
	if atx.NodeId.Key != pub.String() {
		return fmt.Errorf("node ids don't match")
	}
	if atx.PrevATXId != *types.EmptyAtxId {
		err = db.ValidateSignedAtx(*pub, atx)
		if err != nil { // means there is no such identity
			return fmt.Errorf("no id found %v err %v", atx.ShortString(), err)
		}
		prevATX, err := db.GetAtxHeader(atx.PrevATXId)
		if err != nil {
			return fmt.Errorf("validation failed: prevATX not found: %v", err)
		}

		if prevATX.NodeId.Key != atx.NodeId.Key {
			return fmt.Errorf("previous ATX belongs to different miner. atx.ID: %v, atx.NodeID: %v, prevAtx.NodeID: %v",
				atx.ShortString(), atx.NodeId.Key, prevATX.NodeId.Key)
		}

		prevEp := prevATX.PubLayerIdx.GetEpoch(db.LayersPerEpoch)
		curEp := atx.PubLayerIdx.GetEpoch(db.LayersPerEpoch)
		if prevEp >= curEp {
			return fmt.Errorf(
				"prevAtx epoch (%v, layer %v) isn't older than current atx epoch (%v, layer %v)",
				prevEp, prevATX.PubLayerIdx, curEp, atx.PubLayerIdx)
		}

		if prevATX.Sequence+1 != atx.Sequence {
			return fmt.Errorf("sequence number is not one more than prev sequence number")
		}

		if atx.Commitment != nil {
			return fmt.Errorf("prevATX declared, but commitment proof is included")
		}

		if atx.CommitmentMerkleRoot != nil {
			return fmt.Errorf("prevATX declared, but commitment merkle root is included in challenge")
		}
	} else {
		if atx.Sequence != 0 {
			return fmt.Errorf("no prevATX declared, but sequence number not zero")
		}
		if atx.Commitment == nil {
			return fmt.Errorf("no prevATX declared, but commitment proof is not included")
		}
		if atx.CommitmentMerkleRoot == nil {
			return fmt.Errorf("no prevATX declared, but commitment merkle root is not included in challenge")
		}
		if !bytes.Equal(atx.Commitment.MerkleRoot, atx.CommitmentMerkleRoot) {
			return errors.New("commitment merkle root included in challenge is not equal to the merkle root included in the proof")
		}
		if err := db.nipstValidator.VerifyPost(*pub, atx.Commitment, atx.Nipst.Space); err != nil {
			return fmt.Errorf("invalid commitment proof: %v", err)
		}
	}

	if atx.PositioningAtx != *types.EmptyAtxId {
		posAtx, err := db.GetAtxHeader(atx.PositioningAtx)
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
		return fmt.Errorf("could not calculate active set for ATX %v %s", atx.ShortString(), err)
	}

	if atx.ActiveSetSize != activeSet {
		return fmt.Errorf("atx contains view with unequal active ids (%v) than seen (%v)", atx.ActiveSetSize, activeSet)
	}

	hash, err := atx.NIPSTChallenge.Hash()
	if err != nil {
		return fmt.Errorf("cannot get NIPST Challenge hash: %v", err)
	}
	db.log.With().Info("Validated NIPST", log.String("challenge_hash", hash.String()), log.AtxID(atx.ShortString()))

	pubKey := signing.NewPublicKey(util.Hex2Bytes(atx.NodeId.Key))
	if err = db.nipstValidator.Validate(*pubKey, atx.Nipst, *hash); err != nil {
		return fmt.Errorf("NIPST not valid: %v", err)
	}

	return nil
}

// ContextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (db *ActivationDb) ContextuallyValidateAtx(atx *types.ActivationTxHeader) error {
	if atx.PrevATXId != *types.EmptyAtxId {
		lastAtx, err := db.GetNodeLastAtxID(atx.NodeId)
		if err != nil {
			db.log.With().Error("could not fetch node last ATX",
				log.AtxID(atx.ShortString()), log.String("atx_node_id", atx.NodeId.ShortString()), log.Err(err))
			return fmt.Errorf("could not fetch node last ATX: %v", err)
		}
		// last atx is not the one referenced
		if lastAtx != atx.PrevATXId {
			return fmt.Errorf("last atx is not the one referenced")
		}
	} else {
		lastAtx, err := db.GetNodeLastAtxID(atx.NodeId)
		if _, ok := err.(ErrAtxNotFound); err != nil && !ok {
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

	//todo: maybe cleanup DB if failed by using defer (#1921)
	if _, err := db.atxs.Get(getAtxHeaderKey(atx.Id())); err == nil {
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

	err = db.addAtxToNodeID(atx.NodeId, atx)
	if err != nil {
		return err
	}
	db.log.Debug("finished storing atx %v, in epoch %v", atx.ShortString(), ech)

	return nil
}

func (db *ActivationDb) storeAtxUnlocked(atx *types.ActivationTx) error {
	atxHeaderBytes, err := types.InterfaceToBytes(atx.ActivationTxHeader)
	if err != nil {
		return err
	}
	err = db.atxs.Put(getAtxHeaderKey(atx.Id()), atxHeaderBytes)
	if err != nil {
		return err
	}

	atxBodyBytes, err := types.InterfaceToBytes(getAtxBody(atx))
	if err != nil {
		return err
	}
	err = db.atxs.Put(getAtxBodyKey(atx.Id()), atxBodyBytes)
	if err != nil {
		return err
	}

	// notify subscribers
	if ch, found := db.atxChannels[atx.Id()]; found {
		close(ch.ch)
		delete(db.atxChannels, atx.Id())
	}

	return nil
}

func getAtxBody(atx *types.ActivationTx) *types.ActivationTx {
	return &types.ActivationTx{
		InnerActivationTx: &types.InnerActivationTx{
			ActivationTxHeader: nil,
			Nipst:              atx.Nipst,
			View:               atx.View,
			Commitment:         atx.Commitment,
		},
		Sig: atx.Sig,
	}
}

type atxIDAndLayer struct {
	AtxID   types.AtxId
	LayerID types.LayerID
}

// updateTopAtxIfNeeded replaces the top ATX (positioning ATX candidate) if the latest ATX has a higher layer ID.
// This function is not thread safe and needs to be called under a global lock.
func (db *ActivationDb) updateTopAtxIfNeeded(atx *types.ActivationTx) error {
	currentTopAtx, err := db.getTopAtx()
	if err != nil && err != database.ErrNotFound {
		return fmt.Errorf("failed to get current ATX: %v", err)
	}
	if err == nil && currentTopAtx.LayerID >= atx.PubLayerIdx {
		return nil
	}

	newTopAtx := atxIDAndLayer{
		AtxID:   atx.Id(),
		LayerID: atx.PubLayerIdx,
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

func (db ActivationDb) getTopAtx() (atxIDAndLayer, error) {
	topAtxBytes, err := db.atxs.Get([]byte(topAtxKey))
	if err != nil {
		return atxIDAndLayer{}, err
	}
	var topAtx atxIDAndLayer
	err = types.BytesToInterface(topAtxBytes, &topAtx)
	if err != nil {
		return atxIDAndLayer{}, fmt.Errorf("failed to unmarshal top ATX: %v", err)
	}
	return topAtx, nil
}

// addAtxToNodeID inserts activation atx id by node
func (db *ActivationDb) addAtxToNodeID(nodeID types.NodeId, atx *types.ActivationTx) error {
	err := db.atxs.Put(getNodeAtxKey(nodeID, atx.TargetEpoch(db.LayersPerEpoch)), atx.Id().Bytes())
	if err != nil {
		return fmt.Errorf("failed to store ATX ID for node: %v", err)
	}
	return nil
}

// ErrAtxNotFound is a specific error returned when no atx was found in DB
type ErrAtxNotFound error

// GetNodeLastAtxID returns the last atx id that was received for node nodeID
func (db *ActivationDb) GetNodeLastAtxID(nodeID types.NodeId) (types.AtxId, error) {
	nodeAtxsIterator := db.atxs.Find(getNodeAtxPrefix(nodeID))
	// ATX syntactic validation ensures that each ATX is at least one epoch after a referenced previous ATX.
	// Contextual validation ensures that the previous ATX referenced matches what this method returns, so the next ATX
	// added will always be the next ATX returned by this method.
	// leveldb_iterator.Last() returns the last entry in lexicographical order of the keys:
	//   https://github.com/google/leveldb/blob/master/doc/index.md#comparators
	// For the lexicographical order to match the epoch order we must encode the epoch id using big endian encoding when
	// composing the key.
	if exists := nodeAtxsIterator.Last(); !exists {
		return *types.EmptyAtxId, ErrAtxNotFound(fmt.Errorf("atx for node %v does not exist", nodeID.ShortString()))
	}
	return types.AtxId(types.BytesToHash(nodeAtxsIterator.Value())), nil
}

// GetNodeAtxIDForEpoch returns an atx published by the provided nodeID for the specified targetEpoch. meaning the atx
// that the requested nodeID has published. it returns an error if no atx was found for provided nodeID
func (db *ActivationDb) GetNodeAtxIDForEpoch(nodeID types.NodeId, targetEpoch types.EpochId) (types.AtxId, error) {
	id, err := db.atxs.Get(getNodeAtxKey(nodeID, targetEpoch))
	if err != nil {
		return *types.EmptyAtxId, fmt.Errorf("atx for node %v targeting epoch %v: %v",
			nodeID.ShortString(), targetEpoch, err)
	}
	return types.AtxId(types.BytesToHash(id)), nil
}

// GetPosAtxID returns the best (highest layer id), currently known to this node, pos atx id
func (db *ActivationDb) GetPosAtxID() (types.AtxId, error) {
	idAndLayer, err := db.getTopAtx()
	if err != nil {
		return *types.EmptyAtxId, err
	}
	return idAndLayer.AtxID, nil
}

// GetAtxHeader returns the ATX header by the given ID. This function is thread safe and will return an error if the ID
// is not found in the ATX DB.
func (db *ActivationDb) GetAtxHeader(id types.AtxId) (*types.ActivationTxHeader, error) {
	if id == *types.EmptyAtxId {
		return nil, errors.New("trying to fetch empty atx id")
	}

	if atxHeader, gotIt := db.atxHeaderCache.Get(id); gotIt {
		return atxHeader, nil
	}
	db.RLock()
	atxHeaderBytes, err := db.atxs.Get(getAtxHeaderKey(id))
	db.RUnlock()
	if err != nil {
		return nil, err
	}
	var atxHeader types.ActivationTxHeader
	err = types.BytesToInterface(atxHeaderBytes, &atxHeader)
	if err != nil {
		return nil, err
	}
	atxHeader.SetId(&id)
	db.atxHeaderCache.Add(id, &atxHeader)
	return &atxHeader, nil
}

// GetFullAtx returns the full atx struct of the given atxId id, it returns an error if the full atx cannot be found
// in all databases
func (db *ActivationDb) GetFullAtx(id types.AtxId) (*types.ActivationTx, error) {
	if id == *types.EmptyAtxId {
		return nil, errors.New("trying to fetch empty atx id")
	}

	db.RLock()
	atxBytes, err := db.atxs.Get(getAtxBodyKey(id))
	db.RUnlock()
	if err != nil {
		return nil, err
	}
	atx, err := types.BytesAsAtx(atxBytes)
	if err != nil {
		return nil, err
	}
	header, err := db.GetAtxHeader(id)
	if err != nil {
		return nil, err
	}
	atx.ActivationTxHeader = header
	return atx, nil
}

// ValidateSignedAtx extracts public key from message and verifies public key exists in idStore, this is how we validate
// ATX signature. If this is the first ATX it is considered valid anyways and ATX syntactic validation will determine ATX validity
func (db *ActivationDb) ValidateSignedAtx(pubKey signing.PublicKey, signedAtx *types.ActivationTx) error {
	// this is the first occurrence of this identity, we cannot validate simply by extracting public key
	// pass it down to Atx handling so that atx can be syntactically verified and identity could be registered.
	if signedAtx.PrevATXId == *types.EmptyAtxId {
		return nil
	}

	pubString := pubKey.String()
	_, err := db.GetIdentity(pubString)
	if err != nil { // means there is no such identity
		return errInvalidSig
	}
	return nil
}
