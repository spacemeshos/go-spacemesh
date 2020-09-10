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
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"sync"
	"time"
)

const topAtxKey = "topAtxKey"

func getNodeAtxKey(nodeID types.NodeID, targetEpoch types.EpochID) []byte {
	return append(getNodeAtxPrefix(nodeID), util.Uint64ToBytesBigEndian(uint64(targetEpoch))...)
}

func getNodeAtxPrefix(nodeID types.NodeID) []byte {
	return []byte(fmt.Sprintf("n_%v_", nodeID.Key))
}

func getNodeAtxEpochKey(epoch types.EpochID, nodeID types.NodeID) []byte {
	return append(getEpochPrefix(epoch), nodeID.ToBytes()...)
}

func getEpochPrefix(epoch types.EpochID) []byte {
	return []byte(fmt.Sprintf("e_%v_", epoch.ToBytes()))
}

func getAtxHeaderKey(atxID types.ATXID) []byte {
	return []byte(fmt.Sprintf("h_%v", atxID.Bytes()))
}

func getAtxBodyKey(atxID types.ATXID) []byte {
	return []byte(fmt.Sprintf("b_%v", atxID.Bytes()))
}

var errInvalidSig = fmt.Errorf("identity not found when validating signature, invalid atx")

type atxChan struct {
	ch        chan struct{}
	listeners int
}

// DB hold the atxs received from all nodes and their validity status
// it also stores identifications for all nodes e.g the coupling between ed id and bls id
type DB struct {
	sync.RWMutex
	// todo: think about whether we need one db or several(#1922)
	idStore
	atxs              database.Database
	atxHeaderCache    AtxCache
	meshDb            *mesh.DB
	LayersPerEpoch    uint16
	nipstValidator    nipstValidator
	pendingActiveSet  map[types.Hash12]*sync.Mutex
	log               log.Log
	calcActiveSetFunc func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error)
	processAtxMutex   sync.Mutex
	assLock           sync.Mutex
	atxChannels       map[types.ATXID]*atxChan
}

// NewDB creates a new struct of type DB, this struct will hold the atxs received from all nodes and
// their validity
func NewDB(dbStore database.Database, idStore idStore, meshDb *mesh.DB, layersPerEpoch uint16, nipstValidator nipstValidator, log log.Log) *DB {
	db := &DB{
		idStore:          idStore,
		atxs:             dbStore,
		atxHeaderCache:   NewAtxCache(600),
		meshDb:           meshDb,
		LayersPerEpoch:   layersPerEpoch,
		nipstValidator:   nipstValidator,
		pendingActiveSet: make(map[types.Hash12]*sync.Mutex),
		log:              log,
		atxChannels:      make(map[types.ATXID]*atxChan),
	}
	db.calcActiveSetFunc = db.CalcActiveSetSize
	return db
}

var closedChan = make(chan struct{})

func init() {
	close(closedChan)
}

// AwaitAtx returns a channel that will receive notification when the specified atx with id id is received via gossip
func (db *DB) AwaitAtx(id types.ATXID) chan struct{} {
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
func (db *DB) UnsubscribeAtx(id types.ATXID) {
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
func (db *DB) ProcessAtxs(atxs []*types.ActivationTx) error {
	// seenMinerIds := map[string]struct{}{}
	for _, atx := range atxs {
		/* minerID := atx.NodeID.Key
		if _, found := seenMinerIds[minerID]; found {
			// TODO: Blacklist this miner
			// TODO: Ensure that these are two different, syntactically valid ATXs for the same epoch, otherwise the
			//  miner did nothing wrong
			db.log.With().Error("found miner with multiple ATXs published in same block",
				log.FieldNamed("atx_node_id", atx.NodeID), atx.ID())
		}*/
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
func (db *DB) ProcessAtx(atx *types.ActivationTx) error {
	db.processAtxMutex.Lock()
	defer db.processAtxMutex.Unlock()

	existingATX, _ := db.GetAtxHeader(atx.ID())
	if existingATX != nil { // Already processed
		return nil
	}
	epoch := atx.PubLayerID.GetEpoch()
	db.log.With().Info("processing atx", atx.ID(), epoch, log.FieldNamed("atx_node_id", atx.NodeID),
		atx.PubLayerID)
	err := db.ContextuallyValidateAtx(atx.ActivationTxHeader)
	if err != nil {
		db.log.With().Error("ATX failed contextual validation", atx.ID(), log.Err(err))
		// TODO: Blacklist this miner
	} else {
		db.log.With().Info("ATX is valid", atx.ID())
	}
	err = db.StoreAtx(epoch, atx)
	if err != nil {
		return fmt.Errorf("cannot store atx %s: %v", atx.ShortString(), err)
	}

	err = db.StoreNodeIdentity(atx.NodeID)
	if err != nil {
		db.log.With().Error("cannot store node identity", log.FieldNamed("atx_node_id", atx.NodeID),
			atx.ID(), log.Err(err))
	}
	return nil
}

func (db *DB) createTraversalActiveSetCounterFunc(countedAtxs map[string]types.ATXID, penalties map[string]struct{}, layersPerEpoch uint16, epoch types.EpochID) func(b *types.Block) (bool, error) {

	traversalFunc := func(b *types.Block) (stop bool, err error) {

		// count unique ATXs
		if b.ActiveSet == nil {
			return false, nil
		}
		for _, id := range *b.ActiveSet {
			atx, err := db.GetAtxHeader(id)
			if err != nil {
				log.Panic("error fetching atx %v from database -- inconsistent state", id.ShortString()) // TODO: handle inconsistent state
				return false, fmt.Errorf("error fetching atx %v from database -- inconsistent state", id.ShortString())
			}

			// todo: should we accept only eopch -1 atxs?

			// make sure the target epoch is our epoch
			if atx.TargetEpoch() != epoch {
				db.log.With().Debug("atx found, but targeting epoch doesn't match publication epoch", atx.ID(),
					log.FieldNamed("atx_target_epoch", atx.TargetEpoch()),
					log.FieldNamed("actual_epoch", epoch))
				continue
			}

			// ignore atx from nodes in penalty
			if _, exist := penalties[atx.NodeID.Key]; exist {
				db.log.With().Debug("ignoring atx from node in penalty", atx.NodeID, atx.ID())
				continue
			}

			if prevID, exist := countedAtxs[atx.NodeID.Key]; exist { // same miner

				if prevID != id { // different atx for same epoch
					db.log.With().Error("Encountered second atx for the same miner on the same epoch",
						log.FieldNamed("first_atx", prevID), log.FieldNamed("second_atx", id))

					penalties[atx.NodeID.Key] = struct{}{} // mark node in penalty
					delete(countedAtxs, atx.NodeID.Key)    // remove the penalized node from counted
				}
				continue
			}

			countedAtxs[atx.NodeID.Key] = id
		}

		return false, nil
	}

	return traversalFunc
}

// CalcActiveSetSize - returns the active set size that matches the view of the contextually valid blocks in the provided layer
func (db *DB) CalcActiveSetSize(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {

	if epoch == 0 {
		return nil, errors.New("tried to retrieve active set for epoch 0")
	}

	firstLayerOfPrevEpoch := (epoch - 1).FirstLayer()

	countedAtxs := make(map[string]types.ATXID)
	penalties := make(map[string]struct{})

	traversalFunc := db.createTraversalActiveSetCounterFunc(countedAtxs, penalties, db.LayersPerEpoch, epoch)

	startTime := time.Now()
	err := db.meshDb.ForBlockInView(blocks, firstLayerOfPrevEpoch, traversalFunc)
	if err != nil {
		return nil, err
	}
	db.log.With().Info("done calculating active set size",
		log.Int("size", len(countedAtxs)),
		log.String("duration", time.Now().Sub(startTime).String()))

	result := make(map[string]struct{}, len(countedAtxs))
	for k := range countedAtxs {
		result[k] = struct{}{}
	}

	return result, nil
}

// CalcActiveSetFromView traverses the view found in a - the activation tx and counts number of active ids published
// in the epoch prior to the epoch that a was published at, this number is the number of active ids in the next epoch
// the function returns error if the view is not found
func (db *DB) CalcActiveSetFromView(view []types.BlockID, pubEpoch types.EpochID) (uint32, error) {
	if pubEpoch < 1 {
		return 0, fmt.Errorf("publication epoch cannot be less than 1, found %v", pubEpoch)
	}
	viewHash := types.CalcBlocksHash12(view)
	count, found := activesetCache.Get(viewHash)
	if found {
		return count, nil
	}
	// check if we have a running calculation for this hash
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

func (db *DB) deleteLock(viewHash types.Hash12) {
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
func (db *DB) SyntacticallyValidateAtx(atx *types.ActivationTx) error {
	events.ReportNewActivation(atx)
	pub, err := ExtractPublicKey(atx)
	if err != nil {
		return fmt.Errorf("cannot validate atx sig atx id %v err %v", atx.ShortString(), err)
	}
	if atx.NodeID.Key != pub.String() {
		return fmt.Errorf("node ids don't match")
	}
	if atx.PrevATXID != *types.EmptyATXID {
		err = db.ValidateSignedAtx(*pub, atx)
		if err != nil { // means there is no such identity
			return fmt.Errorf("no id found %v err %v", atx.ShortString(), err)
		}
		prevATX, err := db.GetAtxHeader(atx.PrevATXID)
		if err != nil {
			return fmt.Errorf("validation failed: prevATX not found: %v", err)
		}

		if prevATX.NodeID.Key != atx.NodeID.Key {
			return fmt.Errorf("previous ATX belongs to different miner. atx.ID: %v, atx.NodeID: %v, prevAtx.NodeID: %v",
				atx.ShortString(), atx.NodeID.Key, prevATX.NodeID.Key)
		}

		prevEp := prevATX.PubLayerID.GetEpoch()
		curEp := atx.PubLayerID.GetEpoch()
		if prevEp >= curEp {
			return fmt.Errorf(
				"prevAtx epoch (%v, layer %v) isn't older than current atx epoch (%v, layer %v)",
				prevEp, prevATX.PubLayerID, curEp, atx.PubLayerID)
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

	if atx.PositioningATX != *types.EmptyATXID {
		posAtx, err := db.GetAtxHeader(atx.PositioningATX)
		if err != nil {
			return fmt.Errorf("positioning atx not found")
		}
		if atx.PubLayerID <= posAtx.PubLayerID {
			return fmt.Errorf("atx layer (%v) must be after positioning atx layer (%v)",
				atx.PubLayerID, posAtx.PubLayerID)
		}
		if uint64(atx.PubLayerID-posAtx.PubLayerID) > uint64(db.LayersPerEpoch) {
			return fmt.Errorf("expected distance of one epoch (%v layers) from pos ATX but found %v",
				db.LayersPerEpoch, atx.PubLayerID-posAtx.PubLayerID)
		}
	} else {
		publicationEpoch := atx.PubLayerID.GetEpoch()
		if !publicationEpoch.IsGenesis() {
			return fmt.Errorf("no positioning atx found")
		}
	}

	hash, err := atx.NIPSTChallenge.Hash()
	if err != nil {
		return fmt.Errorf("cannot get NIPST Challenge hash: %v", err)
	}
	db.log.With().Info("Validated NIPST", log.String("challenge_hash", hash.String()), atx.ID())

	pubKey := signing.NewPublicKey(util.Hex2Bytes(atx.NodeID.Key))
	if err = db.nipstValidator.Validate(*pubKey, atx.Nipst, *hash); err != nil {
		return fmt.Errorf("NIPST not valid: %v", err)
	}

	return nil
}

// ContextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (db *DB) ContextuallyValidateAtx(atx *types.ActivationTxHeader) error {
	if atx.PrevATXID != *types.EmptyATXID {
		lastAtx, err := db.GetNodeLastAtxID(atx.NodeID)
		if err != nil {
			db.log.With().Error("could not fetch node last ATX", atx.ID(),
				log.FieldNamed("atx_node_id", atx.NodeID), log.Err(err))
			return fmt.Errorf("could not fetch node last ATX: %v", err)
		}
		// last atx is not the one referenced
		if lastAtx != atx.PrevATXID {
			return fmt.Errorf("last atx is not the one referenced")
		}
	} else {
		lastAtx, err := db.GetNodeLastAtxID(atx.NodeID)
		if _, ok := err.(ErrAtxNotFound); err != nil && !ok {
			db.log.Error("fetching ATX ids failed: %v", err)
			return err
		}
		if err == nil { // we found an ATX for this node ID, although it reported no prevATX -- this is invalid
			return fmt.Errorf("no prevATX reported, but other ATX with same nodeID (%v) found: %v",
				atx.NodeID.ShortString(), lastAtx.ShortString())
		}
	}

	return nil
}

// StoreAtx stores an atx for epoch ech, it stores atx for the current epoch and adds the atx for the nodeID that
// created it in a sorted manner by the sequence id. This function does not validate the atx and assumes all data is
// correct and that all associated atx exist in the db. Will return error if writing to db failed.
func (db *DB) StoreAtx(ech types.EpochID, atx *types.ActivationTx) error {
	db.Lock()
	defer db.Unlock()

	// todo: maybe cleanup DB if failed by using defer (#1921)
	if _, err := db.atxs.Get(getAtxHeaderKey(atx.ID())); err == nil {
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

	err = db.addAtxToNodeID(atx.NodeID, atx)
	if err != nil {
		return err
	}

	err = db.addNodeAtxToEpoch(atx.PubLayerID.GetEpoch(), atx.NodeID, atx)
	if err != nil {
		return err
	}

	db.log.Info("finished storing atx %v, in epoch %v", atx.ShortString(), ech)

	return nil
}

func (db *DB) storeAtxUnlocked(atx *types.ActivationTx) error {
	atxHeaderBytes, err := types.InterfaceToBytes(atx.ActivationTxHeader)
	if err != nil {
		return err
	}
	err = db.atxs.Put(getAtxHeaderKey(atx.ID()), atxHeaderBytes)
	if err != nil {
		return err
	}

	atxBodyBytes, err := types.InterfaceToBytes(getAtxBody(atx))
	if err != nil {
		return err
	}
	err = db.atxs.Put(getAtxBodyKey(atx.ID()), atxBodyBytes)
	if err != nil {
		return err
	}

	// notify subscribers
	if ch, found := db.atxChannels[atx.ID()]; found {
		close(ch.ch)
		delete(db.atxChannels, atx.ID())
	}

	return nil
}

func getAtxBody(atx *types.ActivationTx) *types.ActivationTx {
	return &types.ActivationTx{
		InnerActivationTx: &types.InnerActivationTx{
			ActivationTxHeader: nil,
			Nipst:              atx.Nipst,
			Commitment:         atx.Commitment,
		},
		Sig: atx.Sig,
	}
}

type atxIDAndLayer struct {
	AtxID   types.ATXID
	LayerID types.LayerID
}

// updateTopAtxIfNeeded replaces the top ATX (positioning ATX candidate) if the latest ATX has a higher layer ID.
// This function is not thread safe and needs to be called under a global lock.
func (db *DB) updateTopAtxIfNeeded(atx *types.ActivationTx) error {
	currentTopAtx, err := db.getTopAtx()
	if err != nil && err != database.ErrNotFound {
		return fmt.Errorf("failed to get current ATX: %v", err)
	}
	if err == nil && currentTopAtx.LayerID >= atx.PubLayerID {
		return nil
	}

	newTopAtx := atxIDAndLayer{
		AtxID:   atx.ID(),
		LayerID: atx.PubLayerID,
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

func (db *DB) getTopAtx() (atxIDAndLayer, error) {
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
func (db *DB) addAtxToNodeID(nodeID types.NodeID, atx *types.ActivationTx) error {
	err := db.atxs.Put(getNodeAtxKey(nodeID, atx.PubLayerID.GetEpoch()), atx.ID().Bytes())
	if err != nil {
		return fmt.Errorf("failed to store ATX ID for node: %v", err)
	}
	return nil
}

func (db *DB) addNodeAtxToEpoch(epoch types.EpochID, nodeID types.NodeID, atx *types.ActivationTx) error {
	db.log.Info("added atx %v to epoch %v", atx.ID().ShortString(), epoch)
	err := db.atxs.Put(getNodeAtxEpochKey(atx.PubLayerID.GetEpoch(), nodeID), atx.ID().Bytes())
	if err != nil {
		return fmt.Errorf("failed to store ATX ID for node: %v", err)
	}
	return nil
}

// ErrAtxNotFound is a specific error returned when no atx was found in DB
type ErrAtxNotFound error

// GetNodeLastAtxID returns the last atx id that was received for node nodeID
func (db *DB) GetNodeLastAtxID(nodeID types.NodeID) (types.ATXID, error) {
	nodeAtxsIterator := db.atxs.Find(getNodeAtxPrefix(nodeID))
	// ATX syntactic validation ensures that each ATX is at least one epoch after a referenced previous ATX.
	// Contextual validation ensures that the previous ATX referenced matches what this method returns, so the next ATX
	// added will always be the next ATX returned by this method.
	// leveldb_iterator.Last() returns the last entry in lexicographical order of the keys:
	//   https://github.com/google/leveldb/blob/master/doc/index.md#comparators
	// For the lexicographical order to match the epoch order we must encode the epoch id using big endian encoding when
	// composing the key.
	if exists := nodeAtxsIterator.Last(); !exists {
		return *types.EmptyATXID, ErrAtxNotFound(fmt.Errorf("atx for node %v does not exist", nodeID.ShortString()))
	}
	return types.ATXID(types.BytesToHash(nodeAtxsIterator.Value())), nil
}

// GetEpochAtxs returns all valid ATXs received in the epoch epochID
func (db *DB) GetEpochAtxs(epochID types.EpochID) (atxs []types.ATXID) {
	atxIterator := db.atxs.Find(getEpochPrefix(epochID))
	for atxIterator.Next() {
		if atxIterator.Key() == nil {
			break
		}
		var a types.ATXID
		err := types.BytesToInterface(atxIterator.Value(), &a)
		if err != nil {
			db.log.Panic("cannot parse atx from DB")
			break
		}
		atxs = append(atxs, a)
	}
	db.log.Info("returned epoch %v atxs %v %v", epochID, len(atxs), atxs)
	return atxs
}

// GetNodeAtxIDForEpoch returns an atx published by the provided nodeID for the specified targetEpoch. meaning the atx
// that the requested nodeID has published. it returns an error if no atx was found for provided nodeID
func (db *DB) GetNodeAtxIDForEpoch(nodeID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error) {
	id, err := db.atxs.Get(getNodeAtxKey(nodeID, targetEpoch))
	if err != nil {
		return *types.EmptyATXID, fmt.Errorf("atx for node %v targeting epoch %v: %v",
			nodeID.ShortString(), targetEpoch, err)
	}
	return types.ATXID(types.BytesToHash(id)), nil
}

// GetPosAtxID returns the best (highest layer id), currently known to this node, pos atx id
func (db *DB) GetPosAtxID() (types.ATXID, error) {
	idAndLayer, err := db.getTopAtx()
	if err != nil {
		return *types.EmptyATXID, err
	}
	return idAndLayer.AtxID, nil
}

// GetAtxHeader returns the ATX header by the given ID. This function is thread safe and will return an error if the ID
// is not found in the ATX DB.
func (db *DB) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if id == *types.EmptyATXID {
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
	atxHeader.SetID(&id)
	db.atxHeaderCache.Add(id, &atxHeader)
	return &atxHeader, nil
}

// GetFullAtx returns the full atx struct of the given atxId id, it returns an error if the full atx cannot be found
// in all databases
func (db *DB) GetFullAtx(id types.ATXID) (*types.ActivationTx, error) {
	if id == *types.EmptyATXID {
		return nil, errors.New("trying to fetch empty atx id")
	}

	db.RLock()
	atxBytes, err := db.atxs.Get(getAtxBodyKey(id))
	db.RUnlock()
	if err != nil {
		return nil, err
	}
	atx, err := types.BytesToAtx(atxBytes)
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
func (db *DB) ValidateSignedAtx(pubKey signing.PublicKey, signedAtx *types.ActivationTx) error {
	// this is the first occurrence of this identity, we cannot validate simply by extracting public key
	// pass it down to Atx handling so that atx can be syntactically verified and identity could be registered.
	if signedAtx.PrevATXID == *types.EmptyATXID {
		return nil
	}

	pubString := pubKey.String()
	_, err := db.GetIdentity(pubString)
	if err != nil { // means there is no such identity
		return errInvalidSig
	}
	return nil
}

// HandleGossipAtx handles the atx gossip data channel
func (db *DB) HandleGossipAtx(data service.GossipMessage, syncer service.Syncer) {
	if data == nil {
		return
	}
	atx, err := types.BytesToAtx(data.Bytes())
	if err != nil {
		db.log.Error("cannot parse incoming ATX")
		return
	}
	atx.CalcAndSetID()

	db.log.With().Info("got new ATX", atx.Fields(len(data.Bytes()))...)

	//todo fetch from neighbour (#1925)
	if atx.Nipst == nil {
		db.log.Panic("nil nipst in gossip")
		return
	}

	if err := syncer.FetchPoetProof(atx.GetPoetProofRef()); err != nil {
		db.log.Warning("received ATX (%v) with syntactically invalid or missing PoET proof (%x): %v",
			atx.ShortString(), atx.GetShortPoetProofRef(), err)
		return
	}

	if err := syncer.FetchAtxReferences(atx); err != nil {
		db.log.With().Warning("received ATX with missing references of prev or pos id",
			atx.ID(), atx.PrevATXID, atx.PositioningATX, log.Err(err))
		return
	}

	err = db.SyntacticallyValidateAtx(atx)
	events.ReportValidActivation(atx, err == nil)
	if err != nil {
		db.log.Warning("received syntactically invalid ATX %v: %v", atx.ShortString(), err)
		// TODO: blacklist peer
		return
	}

	//db.pool.Put(atx)
	err = db.ProcessAtx(atx)
	if err != nil {
		db.log.Warning("cannot process ATX %v: %v", atx.ShortString(), err)
		// TODO: blacklist peer
		return
	}
	data.ReportValidation(AtxProtocol)
	db.log.With().Info("stored and propagated new syntactically valid ATX", atx.ID())
}
