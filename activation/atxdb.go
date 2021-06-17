package activation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
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
	return atxID.Bytes()
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
	atxs                database.Database
	atxHeaderCache      AtxCache
	meshDb              *mesh.DB
	LayersPerEpoch      uint16
	goldenATXID         types.ATXID
	nipstValidator      nipstValidator
	pendingTotalWeight  map[types.Hash12]*sync.Mutex
	pTotalWeightLock    sync.Mutex
	log                 log.Log
	calcTotalWeightFunc func(targetEpoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error)
	processAtxMutex     sync.Mutex
	atxChannels         map[types.ATXID]*atxChan
}

// NewDB creates a new struct of type DB, this struct will hold the atxs received from all nodes and
// their validity
func NewDB(dbStore database.Database, idStore idStore, meshDb *mesh.DB, layersPerEpoch uint16, goldenATXID types.ATXID, nipstValidator nipstValidator, log log.Log) *DB {
	db := &DB{
		idStore:            idStore,
		atxs:               dbStore,
		atxHeaderCache:     NewAtxCache(600),
		meshDb:             meshDb,
		LayersPerEpoch:     layersPerEpoch,
		goldenATXID:        goldenATXID,
		nipstValidator:     nipstValidator,
		pendingTotalWeight: make(map[types.Hash12]*sync.Mutex),
		log:                log,
		atxChannels:        make(map[types.ATXID]*atxChan),
	}
	db.calcTotalWeightFunc = db.GetMinerWeightsInEpochFromView
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
	db.log.With().Info("processing atx",
		atx.ID(),
		epoch,
		log.FieldNamed("atx_node_id", atx.NodeID),
		atx.PubLayerID)
	err := db.ContextuallyValidateAtx(atx.ActivationTxHeader)
	if err != nil {
		db.log.With().Error("atx failed contextual validation", atx.ID(), log.Err(err))
		// TODO: Blacklist this miner
	} else {
		db.log.With().Info("atx is valid", atx.ID())
	}
	err = db.StoreAtx(epoch, atx)
	if err != nil {
		return fmt.Errorf("cannot store atx %s: %v", atx.ShortString(), err)
	}

	err = db.StoreNodeIdentity(atx.NodeID)
	if err != nil {
		db.log.With().Error("cannot store node identity",
			log.FieldNamed("atx_node_id", atx.NodeID),
			atx.ID(),
			log.Err(err))
	}
	return nil
}

func (db *DB) createTraversalFuncForMinerWeights(minerWeight map[string]uint64, targetEpoch types.EpochID) func(b *types.Block) (bool, error) {
	return func(b *types.Block) (stop bool, err error) {

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
			if atx.TargetEpoch() != targetEpoch {
				db.log.With().Debug("atx found in relevant layer, but target epoch doesn't match requested epoch",
					atx.ID(),
					log.FieldNamed("atx_target_epoch", atx.TargetEpoch()),
					log.FieldNamed("requested_epoch", targetEpoch))
				continue
			}

			minerWeight[atx.NodeID.Key] = atx.GetWeight()
		}

		return false, nil
	}
}

// GetMinerWeightsInEpochFromView returns a map of miner IDs and each one's weight targeting targetEpoch, by traversing
// the provided view.
func (db *DB) GetMinerWeightsInEpochFromView(targetEpoch types.EpochID, view map[types.BlockID]struct{}) (map[string]uint64, error) {
	if targetEpoch == 0 {
		return nil, errors.New("tried to retrieve miner weights for targetEpoch 0")
	}

	firstLayerOfPrevEpoch := (targetEpoch - 1).FirstLayer()

	minerWeight := make(map[string]uint64)

	traversalFunc := db.createTraversalFuncForMinerWeights(minerWeight, targetEpoch)

	startTime := time.Now()
	err := db.meshDb.ForBlockInView(view, firstLayerOfPrevEpoch, traversalFunc)
	if err != nil {
		return nil, err
	}
	db.log.With().Info("done calculating miner weights",
		log.Int("numMiners", len(minerWeight)),
		log.String("duration", time.Now().Sub(startTime).String()))

	return minerWeight, nil
}

// CalcTotalWeightFromView traverses the provided view and returns the total weight of the ATXs found targeting
// targetEpoch. The function returns error if the view is not found.
func (db *DB) CalcTotalWeightFromView(view []types.BlockID, targetEpoch types.EpochID) (uint64, error) {
	if targetEpoch < 1 {
		return 0, fmt.Errorf("targetEpoch cannot be less than 1, got %v", targetEpoch)
	}
	viewHash := types.CalcBlocksHash12(view)
	totalWeight, found := totalWeightCache.Get(viewHash)
	if found {
		return totalWeight, nil
	}
	// check if we have a running calculation for this hash
	db.pTotalWeightLock.Lock()
	mu, alreadyRunning := db.pendingTotalWeight[viewHash]
	if alreadyRunning {
		db.pTotalWeightLock.Unlock()
		// if there is a running calculation, wait for it to end and get the result
		mu.Lock()
		totalWeight, found = totalWeightCache.Get(viewHash)
		if found {
			mu.Unlock()
			return totalWeight, nil
		}
		// if not found, keep running mutex, ensure it's still in the pending map and calculate total weight
		db.pTotalWeightLock.Lock()
		db.pendingTotalWeight[viewHash] = mu
		db.pTotalWeightLock.Unlock()
	} else {
		// if no running calc, insert new one
		mu = &sync.Mutex{}
		mu.Lock()
		db.pendingTotalWeight[viewHash] = mu
		db.pTotalWeightLock.Unlock()
	}

	mp := map[types.BlockID]struct{}{}
	for _, blk := range view {
		mp[blk] = struct{}{}
	}

	atxWeights, err := db.calcTotalWeightFunc(targetEpoch, mp)
	if err != nil {
		db.deleteLock(viewHash)
		mu.Unlock()
		return 0, err
	}
	totalWeight = 0
	for _, w := range atxWeights {
		totalWeight += w
	}
	totalWeightCache.Add(viewHash, totalWeight)
	db.deleteLock(viewHash)
	mu.Unlock()

	return totalWeight, nil
}

func (db *DB) deleteLock(viewHash types.Hash12) {
	db.pTotalWeightLock.Lock()
	if _, exist := db.pendingTotalWeight[viewHash]; exist {
		delete(db.pendingTotalWeight, viewHash)
	}
	db.pTotalWeightLock.Unlock()
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

	if atx.PositioningATX == *types.EmptyATXID {
		return fmt.Errorf("empty positioning atx")
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
			return fmt.Errorf("previous atx belongs to different miner. atx.ID: %v, atx.NodeID: %v, prevAtx.NodeID: %v",
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
		if err := db.nipstValidator.VerifyPost(*pub, atx.Commitment, atx.Space); err != nil {
			return fmt.Errorf("invalid commitment proof: %v", err)
		}
	}

	if atx.PositioningATX != db.goldenATXID {
		posAtx, err := db.GetAtxHeader(atx.PositioningATX)
		if err != nil {
			return fmt.Errorf("positioning atx not found")
		}
		if atx.PubLayerID <= posAtx.PubLayerID {
			return fmt.Errorf("atx layer (%v) must be after positioning atx layer (%v)",
				atx.PubLayerID, posAtx.PubLayerID)
		}
		if uint64(atx.PubLayerID-posAtx.PubLayerID) > uint64(db.LayersPerEpoch) {
			return fmt.Errorf("expected distance of one epoch (%v layers) from pos atx but found %v",
				db.LayersPerEpoch, atx.PubLayerID-posAtx.PubLayerID)
		}
	} else {
		publicationEpoch := atx.PubLayerID.GetEpoch()
		if !publicationEpoch.NeedsGoldenPositioningATX() {
			return fmt.Errorf("golden atx used for atx in epoch %d, but is only valid in epoch 1", publicationEpoch)
		}
	}

	hash, err := atx.NIPSTChallenge.Hash()
	if err != nil {
		return fmt.Errorf("cannot get nipst challenge hash: %v", err)
	}
	db.log.With().Info("validated nipst", log.String("challenge_hash", hash.String()), atx.ID())

	pubKey := signing.NewPublicKey(util.Hex2Bytes(atx.NodeID.Key))
	if err = db.nipstValidator.Validate(*pubKey, atx.Nipst, atx.Space, *hash); err != nil {
		return fmt.Errorf("nipst not valid: %v", err)
	}

	return nil
}

// ContextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (db *DB) ContextuallyValidateAtx(atx *types.ActivationTxHeader) error {
	if atx.PrevATXID != *types.EmptyATXID {
		lastAtx, err := db.GetNodeLastAtxID(atx.NodeID)
		if err != nil {
			db.log.With().Error("could not fetch node last atx", atx.ID(),
				log.FieldNamed("atx_node_id", atx.NodeID),
				log.Err(err))
			return fmt.Errorf("could not fetch node last atx: %v", err)
		}
		// last atx is not the one referenced
		if lastAtx != atx.PrevATXID {
			return fmt.Errorf("last atx is not the one referenced")
		}
	} else {
		lastAtx, err := db.GetNodeLastAtxID(atx.NodeID)
		if _, ok := err.(ErrAtxNotFound); err != nil && !ok {
			db.log.With().Error("fetching atx ids failed", log.Err(err))
			return err
		}
		if err == nil { // we found an ATX for this node ID, although it reported no prevATX -- this is invalid
			return fmt.Errorf("no prevATX reported, but other atx with same nodeID (%v) found: %v",
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

	db.log.With().Info("finished storing atx in epoch", atx.ID(), ech)
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

	// todo: this changed so that a full atx will be written - inherently there will be double data with atx header
	atxBytes, err := types.InterfaceToBytes(atx)
	if err != nil {
		return err
	}
	err = db.atxs.Put(getAtxBodyKey(atx.ID()), atxBytes)
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
		return fmt.Errorf("failed to get current atx: %v", err)
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
		return fmt.Errorf("failed to marshal top atx: %v", err)
	}

	err = db.atxs.Put([]byte(topAtxKey), topAtxBytes)
	if err != nil {
		return fmt.Errorf("failed to store top atx: %v", err)
	}
	return nil
}

func (db *DB) getTopAtx() (atxIDAndLayer, error) {
	topAtxBytes, err := db.atxs.Get([]byte(topAtxKey))
	if err != nil {
		if err == database.ErrNotFound {
			return atxIDAndLayer{
				AtxID:   db.goldenATXID,
				LayerID: 0,
			}, nil
		}
		return atxIDAndLayer{}, fmt.Errorf("failed to get top atx: %v", err)
	}
	var topAtx atxIDAndLayer
	err = types.BytesToInterface(topAtxBytes, &topAtx)
	if err != nil {
		return atxIDAndLayer{}, fmt.Errorf("failed to unmarshal top atx: %v", err)
	}
	return topAtx, nil
}

// addAtxToNodeID inserts activation atx id by node
func (db *DB) addAtxToNodeID(nodeID types.NodeID, atx *types.ActivationTx) error {
	err := db.atxs.Put(getNodeAtxKey(nodeID, atx.PubLayerID.GetEpoch()), atx.ID().Bytes())
	if err != nil {
		return fmt.Errorf("failed to store atx ID for node: %v", err)
	}
	return nil
}

func (db *DB) addNodeAtxToEpoch(epoch types.EpochID, nodeID types.NodeID, atx *types.ActivationTx) error {
	db.log.Info("added atx %v to epoch %v", atx.ID().ShortString(), epoch)
	err := db.atxs.Put(getNodeAtxEpochKey(atx.PubLayerID.GetEpoch(), nodeID), atx.ID().Bytes())
	if err != nil {
		return fmt.Errorf("failed to store atx ID for node: %v", err)
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
	db.log.With().Info("returned epoch atxs", epochID, log.Int("count", len(atxs)))
	db.log.With().Debug("returned epoch atxs", epochID,
		log.Int("count", len(atxs)),
		log.String("atxs", fmt.Sprint(atxs)))
	return
}

// GetNodeAtxIDForEpoch returns an atx published by the provided nodeID for the specified publication epoch. meaning the atx
// that the requested nodeID has published. it returns an error if no atx was found for provided nodeID
func (db *DB) GetNodeAtxIDForEpoch(nodeID types.NodeID, publicationEpoch types.EpochID) (types.ATXID, error) {
	id, err := db.atxs.Get(getNodeAtxKey(nodeID, publicationEpoch))
	if err != nil {
		return *types.EmptyATXID, fmt.Errorf("atx for node %v with publication epoch %v: %v",
			nodeID.ShortString(), publicationEpoch, err)
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

// GetEpochWeight returns the total weight of ATXs targeting the given epochID.
func (db *DB) GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error) {
	weight := uint64(0)
	activeSet := db.GetEpochAtxs(epochID - 1)
	for _, atxID := range activeSet {
		atxHeader, err := db.GetAtxHeader(atxID)
		if err != nil {
			return 0, nil, err
		}
		weight += atxHeader.GetWeight()
	}
	return weight, activeSet, nil
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
	atx.ActivationTxHeader.SetID(&id)
	db.atxHeaderCache.Add(id, atx.ActivationTxHeader)

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
func (db *DB) HandleGossipAtx(ctx context.Context, data service.GossipMessage, syncer service.Fetcher) {
	if data == nil {
		return
	}
	err := db.HandleAtxData(ctx, data.Bytes(), syncer)
	if err != nil {
		db.log.WithContext(ctx).With().Error("error handling atx data", log.Err(err))
		return
	}
	data.ReportValidation(AtxProtocol)
}

// HandleAtxData handles atxs received either by gossip or sync
func (db *DB) HandleAtxData(ctx context.Context, data []byte, syncer service.Fetcher) error {
	atx, err := types.BytesToAtx(data)
	if err != nil {
		return fmt.Errorf("cannot parse incoming atx")
	}
	atx.CalcAndSetID()
	logger := db.log.WithContext(ctx).WithFields(atx.ID())

	logger.With().Info("got new atx", atx.Fields(len(data))...)

	// todo fetch from neighbour (#1925)
	if atx.Nipst == nil {
		logger.Panic("nil nipst in gossip")
		return fmt.Errorf("nil nipst in gossip")
	}

	if err := syncer.GetPoetProof(ctx, atx.GetPoetProofRef()); err != nil {
		return fmt.Errorf("received atx (%v) with syntactically invalid or missing PoET proof (%x): %v",
			atx.ShortString(), atx.GetPoetProofRef().ShortString(), err)
	}

	if err := db.FetchAtxReferences(ctx, atx, syncer); err != nil {
		return fmt.Errorf("received ATX with missing references of prev or pos id %v, %v, %v, %v",
			atx.ID().ShortString(), atx.PrevATXID.ShortString(), atx.PositioningATX.ShortString(), log.Err(err))
	}

	err = db.SyntacticallyValidateAtx(atx)
	events.ReportValidActivation(atx, err == nil)
	if err != nil {
		return fmt.Errorf("received syntactically invalid atx %v: %v", atx.ShortString(), err)
	}

	err = db.ProcessAtx(atx)
	if err != nil {
		return fmt.Errorf("cannot process atx %v: %v", atx.ShortString(), err)
		// TODO: blacklist peer
	}

	logger.With().Info("stored and propagated new syntactically valid atx", atx.ID())
	return nil
}

// FetchAtxReferences fetches positioning and prev atxs from peers if they are not found in db
func (db *DB) FetchAtxReferences(ctx context.Context, atx *types.ActivationTx, f service.Fetcher) error {
	logger := db.log.WithContext(ctx)
	if atx.PositioningATX != *types.EmptyATXID && atx.PositioningATX != db.goldenATXID {
		logger.With().Info("going to fetch pos atx", atx.PositioningATX, atx.ID())
		if err := f.FetchAtx(ctx, atx.PositioningATX); err != nil {
			return err
		}
	}

	if atx.PrevATXID != *types.EmptyATXID {
		logger.With().Info("going to fetch prev atx", atx.PrevATXID, atx.ID())
		if err := f.FetchAtx(ctx, atx.PrevATXID); err != nil {
			return err
		}
	}
	logger.With().Info("done fetching references for atx", atx.ID())

	return nil
}
