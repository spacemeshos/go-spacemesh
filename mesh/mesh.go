// Package mesh defines the main store point for all the block-mesh objects
// such as ballots, blocks, transactions and global state
package mesh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

var (
	constTrue      = []byte{1}
	constFalse     = []byte{0}
	constLATEST    = []byte("latest")
	constPROCESSED = []byte("processed")
)

// VERIFIED refers to layers we pushed into the state.
var VERIFIED = []byte("verified")

// Validator interface to be used in tests to mock validation flow.
type Validator interface {
	ProcessLayer(context.Context, types.LayerID) error
}

type txMemPool interface {
	Invalidate(id types.TransactionID)
	Get(id types.TransactionID) (*types.Transaction, error)
	Put(id types.TransactionID, tx *types.Transaction)
}

// AtxDB holds logic for working with atxs.
type AtxDB interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
	GetFullAtx(id types.ATXID) (*types.ActivationTx, error)
	SyntacticallyValidateAtx(ctx context.Context, atx *types.ActivationTx) error
}

// Mesh is the logic layer above our mesh.DB database.
type Mesh struct {
	log.Log
	*DB
	AtxDB
	state
	Validator
	trtl   tortoise
	txPool txMemPool
	// latestLayer is the latest layer this node had seen from blocks
	latestLayer types.LayerID
	// latestLayerInState is the latest layer whose contents have been applied to the state
	latestLayerInState types.LayerID
	// processedLayer is the latest layer whose votes have been processed
	processedLayer types.LayerID
	// see doc for MissingLayer()
	missingLayer        types.LayerID
	nextProcessedLayers map[types.LayerID]struct{}
	maxProcessedLayer   types.LayerID
	mutex               sync.RWMutex
	done                chan struct{}
	txMutex             sync.Mutex
}

// NewMesh creates a new instant of a mesh.
func NewMesh(db *DB, atxDb AtxDB, trtl tortoise, txPool txMemPool, state state, logger log.Log) *Mesh {
	msh := &Mesh{
		Log:                 logger,
		trtl:                trtl,
		txPool:              txPool,
		state:               state,
		done:                make(chan struct{}),
		DB:                  db,
		AtxDB:               atxDb,
		nextProcessedLayers: make(map[types.LayerID]struct{}),
		latestLayer:         types.GetEffectiveGenesis(),
		latestLayerInState:  types.GetEffectiveGenesis(),
	}

	msh.Validator = &validator{Mesh: msh}
	gLyr := types.GetEffectiveGenesis()
	for i := types.NewLayerID(1); !i.After(gLyr); i = i.Add(1) {
		if i.Before(gLyr) {
			if err := msh.SetZeroBlockLayer(i); err != nil {
				msh.With().Panic("failed to set zero-block for genesis layer", i, log.Err(err))
			}
		}
		if err := msh.persistLayerHashes(context.Background(), i, i); err != nil {
			msh.With().Panic("failed to persist hashes for layer", i, log.Err(err))
		}
		msh.setProcessedLayer(i)
	}
	return msh
}

// NewRecoveredMesh creates new instance of mesh with recovered mesh data fom database.
func NewRecoveredMesh(db *DB, atxDb AtxDB, trtl tortoise, txPool txMemPool, state state, logger log.Log) *Mesh {
	msh := NewMesh(db, atxDb, trtl, txPool, state, logger)

	latest, err := msh.GetLatestLayer()
	if err != nil {
		logger.With().Panic("failed to recover latest layer", log.Err(err))
	}
	msh.setLatestLayer(latest)

	lyr, err := msh.GetProcessedLayer()
	if err != nil {
		logger.With().Panic("failed to recover processed layer", log.Err(err))
	}
	msh.setProcessedLayerFromRecoveredData(lyr)

	verified, err := msh.GetVerifiedLayer()
	if err != nil {
		logger.With().Panic("failed to recover latest verified layer", log.Err(err))
	}
	if err = msh.setLatestLayerInState(verified); err != nil {
		logger.With().Panic("failed to recover latest layer in state", log.Err(err))
	}

	_, err = state.Rewind(msh.LatestLayerInState())
	if err != nil {
		logger.With().Panic("failed to load state for layer", msh.LatestLayerInState(), log.Err(err))
	}

	msh.With().Info("recovered mesh from disk",
		log.FieldNamed("latest", msh.LatestLayer()),
		log.FieldNamed("processed", msh.ProcessedLayer()),
		log.String("root_hash", state.GetStateRoot().String()))

	return msh
}

// CacheWarmUp warms up cache with latest blocks.
func (msh *Mesh) CacheWarmUp(layerSize int) {
	start := types.NewLayerID(0)
	if msh.ProcessedLayer().Uint32() > uint32(msh.blockCache.Cap()/layerSize) {
		start = msh.ProcessedLayer().Sub(uint32(msh.blockCache.Cap() / layerSize))
	}

	if err := msh.cacheWarmUpFromTo(start, msh.ProcessedLayer()); err != nil {
		msh.With().Error("cache warm up failed during recovery", log.Err(err))
	}

	msh.Info("cache warm up done")
}

// LatestLayerInState returns the latest layer we applied to state.
func (msh *Mesh) LatestLayerInState() types.LayerID {
	msh.mutex.RLock()
	defer msh.mutex.RUnlock()
	return msh.latestLayerInState
}

// LatestLayer - returns the latest layer we saw from the network.
func (msh *Mesh) LatestLayer() types.LayerID {
	msh.mutex.RLock()
	defer msh.mutex.RUnlock()
	return msh.latestLayer
}

// MissingLayer is a layer in (latestLayerInState, processLayer].
// this layer is missing critical data (valid blocks or transactions)
// and can't be applied to the state.
//
// First valid layer starts with 1. 0 is empty layer and can be ignored.
func (msh *Mesh) MissingLayer() types.LayerID {
	msh.mutex.RLock()
	defer msh.mutex.RUnlock()
	return msh.missingLayer
}

func (msh *Mesh) setMissingLayer(lid types.LayerID) {
	msh.mutex.Lock()
	defer msh.mutex.Unlock()
	msh.missingLayer = lid
}

// setLatestLayer sets the latest layer we saw from the network.
func (msh *Mesh) setLatestLayer(idx types.LayerID) {
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: idx,
		Status:  events.LayerStatusTypeUnknown,
	})
	defer msh.mutex.Unlock()
	msh.mutex.Lock()
	if idx.After(msh.latestLayer) {
		events.ReportNodeStatusUpdate()
		msh.With().Info("set latest known layer", idx)
		msh.latestLayer = idx
		if err := layers.SetStatus(msh.db, idx, layers.Latest); err != nil {
			msh.Error("could not persist latest layer index")
		}
	}
}

// GetLayer returns Layer i from the database.
func (msh *Mesh) GetLayer(i types.LayerID) (*types.Layer, error) {
	ballots, err := msh.LayerBallots(i)
	if err != nil {
		return nil, fmt.Errorf("layer ballots: %w", err)
	}
	blocks, err := msh.LayerBlocks(i)
	if err != nil {
		return nil, fmt.Errorf("layer blocks: %w", err)
	}
	return types.NewExistingLayer(i, ballots, blocks), nil
}

// GetLayerHash returns layer hash.
func (msh *Mesh) GetLayerHash(layerID types.LayerID) types.Hash32 {
	h, err := msh.recoverLayerHash(layerID)
	if err == nil {
		return h
	}
	if errors.Is(err, database.ErrNotFound) {
		// layer hash not persisted. i.e. contextual validity not yet determined
		lyr, err := msh.GetLayer(layerID)
		if err == nil {
			return lyr.Hash()
		}
	}
	return types.EmptyLayerHash
}

// ProcessedLayer returns the last processed layer ID.
func (msh *Mesh) ProcessedLayer() types.LayerID {
	msh.mutex.RLock()
	defer msh.mutex.RUnlock()
	return msh.processedLayer
}

func (msh *Mesh) setProcessedLayerFromRecoveredData(pLayer types.LayerID) {
	msh.mutex.Lock()
	defer msh.mutex.Unlock()
	msh.processedLayer = pLayer
	msh.Event().Info("processed layer set from recovered data", pLayer)
}

func (msh *Mesh) setProcessedLayer(layerID types.LayerID) {
	msh.mutex.Lock()
	defer msh.mutex.Unlock()
	if !layerID.After(msh.processedLayer) {
		msh.With().Info("trying to set processed layer to an older layer",
			log.FieldNamed("processed", msh.processedLayer),
			layerID)
		return
	}

	if layerID.After(msh.maxProcessedLayer) {
		msh.maxProcessedLayer = layerID
	}

	if layerID != msh.processedLayer.Add(1) {
		msh.With().Info("trying to set processed layer out of order",
			log.FieldNamed("processed", msh.processedLayer),
			layerID)
		msh.nextProcessedLayers[layerID] = struct{}{}
		return
	}

	msh.nextProcessedLayers[layerID] = struct{}{}
	lastProcessed := msh.processedLayer
	for i := layerID; !i.After(msh.maxProcessedLayer); i = i.Add(1) {
		_, ok := msh.nextProcessedLayers[i]
		if !ok {
			break
		}
		lastProcessed = i
		delete(msh.nextProcessedLayers, i)
	}
	msh.processedLayer = lastProcessed
	events.ReportNodeStatusUpdate()
	msh.Event().Info("processed layer set", msh.processedLayer)

	if err := msh.persistProcessedLayer(msh.processedLayer); err != nil {
		msh.With().Error("failed to persist processed layer",
			log.FieldNamed("processed", lastProcessed),
			log.Err(err))
	}
}

type validator struct {
	*Mesh
}

// ProcessLayer performs fairly heavy lifting: it triggers tortoise to process the full contents of the layer (i.e.,
// all of its blocks), then to attempt to validate all unvalidated layers up to this layer. It also applies state for
// newly-validated layers.
func (vl *validator) ProcessLayer(ctx context.Context, layerID types.LayerID) error {
	logger := vl.WithContext(ctx).WithFields(layerID)
	logger.Info("processing layer")

	// pass the layer to tortoise for processing
	oldVerified, newVerified, reverted := vl.trtl.HandleIncomingLayer(ctx, layerID)
	logger.With().Info("tortoise results",
		log.Bool("reverted", reverted),
		log.FieldNamed("old_verified", oldVerified),
		log.FieldNamed("new_verified", newVerified))

	// set processed layer even if later code will fail, as that failure is not related
	// to the layer that is being processed
	vl.setProcessedLayer(layerID)

	// check for a state reversion: if tortoise reran and detected changes to historical data, it will request that
	// state be reverted and reapplied. pushLayersToState, below, will handle the reapplication.
	if reverted {
		vl.setLatestLayerInState(oldVerified)
		if err := vl.revertState(ctx, oldVerified); err != nil {
			logger.With().Error("failed to revert state, unable to process layer", log.Err(err))
			return err
		}
	}

	vl.mutex.RLock()
	latestLayerInState := vl.latestLayerInState
	vl.mutex.RUnlock()
	// mesh can't skip layer that failed to complete
	from := minLayer(oldVerified, latestLayerInState).Add(1)
	to := newVerified

	if !to.Before(from) {
		if err := vl.pushLayersToState(ctx, from, to); err != nil {
			logger.With().Error("failed to push layers to state", log.Err(err))
			return err
		}
		if err := vl.persistLayerHashes(ctx, from, to); err != nil {
			logger.With().Error("failed to persist layer hashes", log.Err(err))
			return err
		}
		for lid := from; !lid.After(to); lid = lid.Add(1) {
			events.ReportLayerUpdate(events.LayerUpdate{
				LayerID: lid,
				Status:  events.LayerStatusTypeConfirmed,
			})
		}
	}

	logger.Info("done processing layer")
	return nil
}

func (msh *Mesh) getAggregatedHash(lid types.LayerID) (types.Hash32, error) {
	if !lid.After(types.NewLayerID(1)) {
		return types.EmptyLayerHash, nil
	}
	return layers.GetAggregatedHash(msh.db, lid)
}

func (msh *Mesh) persistLayerHashes(ctx context.Context, from, to types.LayerID) error {
	logger := msh.WithContext(ctx)
	if to.Before(from) {
		logger.With().Panic("verified layer went backward",
			log.FieldNamed("fromLayer", from),
			log.FieldNamed("toLayer", to))
	}

	logger.With().Debug("persisting layer hashes",
		log.FieldNamed("from_layer", from),
		log.FieldNamed("to_layer", to))
	for i := from; !i.After(to); i = i.Add(1) {
		validBlockIDs, err := msh.getValidBlockIDs(ctx, i)
		if err != nil {
			logger.With().Error("failed to get valid block IDs", i, log.Err(err))
			return err
		}

		hash := types.EmptyLayerHash
		if len(validBlockIDs) > 0 {
			hash = types.CalcBlocksHash32(validBlockIDs, nil)
		}
		if err := msh.persistLayerHash(i, hash); err != nil {
			logger.With().Error("failed to persist layer hash", i, log.Err(err))
			return err
		}

		prevHash, err := msh.getAggregatedHash(i.Sub(1))
		if err != nil {
			logger.With().Debug("failed to get previous aggregated hash", i, log.Err(err))
			return err
		}

		logger.With().Debug("got previous aggregatedHash", i, log.String("prevAggHash", prevHash.ShortString()))
		newAggHash := types.CalcBlocksHash32(validBlockIDs, prevHash.Bytes())
		if err := msh.persistAggregatedLayerHash(i, newAggHash); err != nil {
			logger.With().Error("failed to persist aggregated layer hash", i, log.Err(err))
			return err
		}
		logger.With().Info("aggregated hash updated for layer",
			i,
			log.String("hash", hash.ShortString()),
			log.String("aggHash", newAggHash.ShortString()))
	}
	return nil
}

func (msh *Mesh) getValidBlockIDs(ctx context.Context, layerID types.LayerID) ([]types.BlockID, error) {
	logger := msh.WithContext(ctx)
	blocks, err := msh.LayerBlockIds(layerID)
	if err != nil {
		return nil, err
	}
	var validBlockIDs []types.BlockID
	for _, bID := range blocks {
		valid, err := msh.ContextualValidity(bID)
		if err != nil {
			// block contextual validity is determined by layer. if one block in the layer is not determined,
			// the whole layer is not yet verified.
			logger.With().Warning("block contextual validity not yet determined", layerID, bID, log.Err(err))
			return nil, err
		}
		if valid {
			validBlockIDs = append(validBlockIDs, bID)
		}
	}
	return validBlockIDs, nil
}

// apply the state of a range of layers, including re-adding transactions from invalid blocks to the mempool.
func (msh *Mesh) pushLayersToState(ctx context.Context, from, to types.LayerID) error {
	logger := msh.WithContext(ctx).WithFields(
		log.Stringer("from_layer", from),
		log.Stringer("to_layer", to))
	logger.Info("pushing layers to state")
	if from.Before(types.GetEffectiveGenesis()) || to.Before(types.GetEffectiveGenesis()) {
		logger.Panic("tried to push genesis layers")
		return nil
	}

	missing := msh.MissingLayer()
	// we never reapply the state of oldVerified. note that state reversions must be handled separately.
	for layerID := from; !layerID.After(to); layerID = layerID.Add(1) {
		msh.mutex.RLock()
		latestLayerInState := msh.latestLayerInState
		msh.mutex.RUnlock()
		if !layerID.After(latestLayerInState) {
			logger.With().Error("trying to apply layer before currently applied layer",
				log.Stringer("applied_layer", latestLayerInState),
				layerID,
			)
			continue
		}
		if err := msh.pushLayer(ctx, layerID); err != nil {
			msh.setMissingLayer(layerID)
			return err
		}
		if layerID == missing {
			msh.setMissingLayer(types.LayerID{})
		}
	}
	return nil
}

func (msh *Mesh) pushLayer(ctx context.Context, layerID types.LayerID) error {
	layerBlocks, err := msh.LayerBlocks(layerID)
	if err != nil {
		return fmt.Errorf("failed to get layer %s: %w", layerID, err)
	}

	validBlocks, invalidBlocks := msh.BlocksByValidity(layerBlocks)
	var (
		applied    *types.Block
		notApplied = invalidBlocks
		blocks     = types.SortBlocks(validBlocks)
	)

	if len(blocks) > 0 {
		// when tortoise verify multiple blocks in the same layer, we only apply one with the lowest
		// lexicographical sort order
		applied = blocks[0]
		if len(blocks) > 1 {
			notApplied = append(notApplied, blocks[1:]...)
		}
	}

	if err = msh.updateStateWithLayer(ctx, layerID, applied); err != nil {
		return fmt.Errorf("failed to update state %s: %w", layerID, err)
	}

	msh.Event().Info("end of layer state root",
		layerID,
		log.Stringer("state_root", msh.state.GetStateRoot()),
	)

	if err = msh.reInsertTxsToPool(applied, notApplied, layerID); err != nil {
		return fmt.Errorf("failed to reinsert TXs to pool %s: %w", layerID, err)
	}
	return nil
}

// RevertState reverts to state as of a previous layer.
func (msh *Mesh) revertState(ctx context.Context, layerID types.LayerID) error {
	logger := msh.WithContext(ctx).WithFields(layerID)
	logger.Info("attempting to roll back state to previous layer")
	if _, err := msh.state.Rewind(layerID); err != nil {
		return fmt.Errorf("failed to revert state to layer %v: %w", layerID, err)
	}
	return nil
}

func (msh *Mesh) reInsertTxsToPool(applied *types.Block, notApplied []*types.Block, l types.LayerID) error {
	seenTxIds := make(map[types.TransactionID]struct{})
	if applied != nil {
		uniqueTxIds([]*types.Block{applied}, seenTxIds) // run for the side effect, updating seenTxIds
	}
	returnedTxs, missing := msh.GetTransactions(uniqueTxIds(notApplied, seenTxIds))
	if len(missing) > 0 {
		msh.With().Error("could not reinsert transactions", log.Int("missing", len(missing)), l)
	}
	if err := msh.markTransactionsDeleted(returnedTxs...); err != nil {
		return err
	}
	for _, tx := range returnedTxs {
		if err := msh.ValidateNonceAndBalance(tx); err != nil {
			return err
		}
		if err := msh.AddTxToPool(tx); err == nil {
			// We ignore errors here, since they mean that the tx is no longer
			// valid and we shouldn't re-add it.
			msh.With().Info("transaction from contextually invalid block re-added to mempool", tx.ID())
		}
	}
	return nil
}

func (msh *Mesh) applyState(block *types.Block) error {
	var failedTxs []*types.Transaction
	var svmErr error
	rewardByMiner := map[types.Address]uint64{}
	for _, r := range block.Rewards {
		rewardByMiner[r.Address] += r.Amount
	}
	txs, missing := msh.GetTransactions(block.TxIDs)
	if len(missing) > 0 {
		return fmt.Errorf("could not find transactions %v from layer %v", missing, block.LayerIndex)
	}
	// TODO: should miner IDs be sorted in a deterministic order prior to applying rewards?
	failedTxs, svmErr = msh.state.ApplyLayer(block.LayerIndex, txs, rewardByMiner)
	if svmErr != nil {
		msh.With().Error("failed to apply transactions",
			block.LayerIndex, log.Int("num_failed_txs", len(failedTxs)), log.Err(svmErr))
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldVerified`
		return fmt.Errorf("apply layer: %w", svmErr)
	}

	if err := msh.DB.writeTransactionRewards(block.LayerIndex, block.Rewards); err != nil {
		msh.With().Error("cannot write reward to db", log.Err(err))
		return err
	}

	reportRewards(block)

	if err := msh.updateDBTXWithBlockID(block, txs...); err != nil {
		msh.With().Error("failed to update tx block ID in db", log.Err(err))
		return err
	}
	for _, tx := range txs {
		if err := transactions.Applied(msh.db, tx.ID()); err != nil {
			return err
		}
	}
	msh.With().Info("applied transactions",
		block.LayerIndex,
		log.Int("valid_block_txs", len(txs)),
		log.Int("num_failed_txs", len(failedTxs)),
	)
	return nil
}

// ProcessLayerPerHareOutput receives hare output once it finishes running for a given layer.
func (msh *Mesh) ProcessLayerPerHareOutput(ctx context.Context, layerID types.LayerID, blockID types.BlockID) error {
	logger := msh.WithContext(ctx).WithFields(layerID, blockID)
	if blockID == types.EmptyBlockID {
		logger.Info("received empty set from hare")
	} else {
		// double-check we have this block in the mesh
		_, err := msh.GetBlock(blockID)
		if err != nil {
			logger.With().Error("hare terminated with block that is not present in mesh", log.Err(err))
			return err
		}
	}
	// report that hare "approved" this layer
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: layerID,
		Status:  events.LayerStatusTypeApproved,
	})

	logger.Info("saving hare output for layer")
	if err := msh.SaveHareConsensusOutput(ctx, layerID, blockID); err != nil {
		logger.Error("saving layer hare output failed")
	}
	return msh.ProcessLayer(ctx, layerID)
}

// apply the state for a single layer.
func (msh *Mesh) updateStateWithLayer(ctx context.Context, layerID types.LayerID, block *types.Block) error {
	msh.txMutex.Lock()
	defer msh.txMutex.Unlock()
	latest := msh.LatestLayerInState()
	if layerID != latest.Add(1) {
		msh.WithContext(ctx).With().Panic("update state out-of-order",
			log.FieldNamed("verified", layerID),
			log.FieldNamed("latest", latest))
	}

	if block != nil {
		if err := msh.applyState(block); err != nil {
			return err
		}
	}
	if err := msh.setLatestLayerInState(layerID); err != nil {
		return err
	}
	return nil
}

func (msh *Mesh) setLatestLayerInState(lyr types.LayerID) error {
	// Update validated layer only after applying transactions since loading of
	// state depends on processedLayer param.
	msh.mutex.Lock()
	defer msh.mutex.Unlock()
	if err := layers.SetStatus(msh.db, lyr, layers.Applied); err != nil {
		// can happen if database already closed
		msh.Error("could not persist validated layer index %d: %v", lyr, err.Error())
		return fmt.Errorf("put into DB: %w", err)
	}
	msh.latestLayerInState = lyr
	return nil
}

// GetAggregatedLayerHash returns the aggregated layer hash up to the specified layer.
func (msh *Mesh) GetAggregatedLayerHash(layerID types.LayerID) types.Hash32 {
	h, err := layers.GetAggregatedHash(msh.db, layerID)
	if err != nil {
		return types.EmptyLayerHash
	}
	return h
}

func uniqueTxIds(blocks []*types.Block, seenTxIds map[types.TransactionID]struct{}) []types.TransactionID {
	var txIds []types.TransactionID
	for _, b := range blocks {
		for _, id := range b.TxIDs {
			if _, found := seenTxIds[id]; found {
				continue
			}
			txIds = append(txIds, id)
			seenTxIds[id] = struct{}{}
		}
	}
	return txIds
}

var errLayerHasBallot = errors.New("layer has ballot")

var errLayerHasBlock = errors.New("layer has block")

// SetZeroBlockLayer tags lyr as a layer without blocks.
func (msh *Mesh) SetZeroBlockLayer(lyr types.LayerID) error {
	msh.With().Info("tagging zero block layer", lyr)
	// check database for layer
	if l, err := msh.GetLayer(lyr); err != nil {
		// database error
		if !errors.Is(err, sql.ErrNotFound) {
			msh.With().Error("error trying to fetch layer from database", lyr, log.Err(err))
			return err
		}
	} else if len(l.Blocks()) != 0 {
		// layer exists
		msh.With().Error("layer has blocks, cannot tag as zero block layer",
			lyr,
			l,
			log.Int("num_blocks", len(l.Blocks())))
		return errLayerHasBlock
	}

	msh.setLatestLayer(lyr)

	// layer doesn't exist, need to insert new layer
	return msh.AddZeroBlockLayer(lyr)
}

// AddTXsFromProposal adds the TXs in a Proposal into the database.
func (msh *Mesh) AddTXsFromProposal(ctx context.Context, layerID types.LayerID, proposalID types.ProposalID, txIDs []types.TransactionID) error {
	logger := msh.WithContext(ctx).WithFields(layerID, proposalID, log.Int("num_txs", len(txIDs)))
	logger.Debug("adding proposal txs to mesh")

	msh.addTransactionsForBlock(logger, layerID, types.EmptyBlockID, txIDs)

	if err := msh.storeTransactionsFromPool(layerID, types.EmptyBlockID, txIDs); err != nil {
		logger.With().Error("not all txs were processed", log.Err(err))
	}

	msh.setLatestLayer(layerID)
	logger.Info("added proposal's txs to database")
	return nil
}

// AddBallot to the mesh.
func (msh *Mesh) AddBallot(ballot *types.Ballot) error {
	msh.trtl.OnBallot(ballot)
	return msh.DB.AddBallot(ballot)
}

// AddBlockWithTXs adds the block and its TXs in into the database.
func (msh *Mesh) AddBlockWithTXs(ctx context.Context, block *types.Block) error {
	logger := msh.WithContext(ctx).WithFields(block.LayerIndex, block.ID(), log.Int("num_txs", len(block.TxIDs)))
	logger.Debug("adding block txs to mesh")

	msh.trtl.OnBlock(block)

	msh.addTransactionsForBlock(logger, block.LayerIndex, block.ID(), block.TxIDs)
	return msh.AddBlock(block)
}

func (msh *Mesh) addTransactionsForBlock(logger log.Log, layerID types.LayerID, blockID types.BlockID, txIDs []types.TransactionID) {
	if err := msh.storeTransactionsFromPool(layerID, blockID, txIDs); err != nil {
		logger.With().Error("not all txs were processed", log.Err(err))
	}

	msh.setLatestLayer(layerID)
	logger.Info("added txs to database")
}

func (msh *Mesh) invalidateFromPools(txIDs []types.TransactionID) {
	for _, id := range txIDs {
		msh.txPool.Invalidate(id)
	}
}

// storeTransactionsFromPool takes declared txs from provided proposal and writes them to DB. it then invalidates
// the transactions from txpool.
func (msh *Mesh) storeTransactionsFromPool(layerID types.LayerID, blockID types.BlockID, txIDs []types.TransactionID) error {
	// Store transactions (doesn't have to be rolled back if other writes fail)
	if len(txIDs) == 0 {
		return nil
	}
	txs := make([]*types.Transaction, 0, len(txIDs))
	for _, txID := range txIDs {
		tx, err := msh.txPool.Get(txID)
		if err != nil {
			// if the transaction is not in the pool it could have been
			// invalidated by another block
			if has, err := transactions.Has(msh.db, txID); !has {
				return fmt.Errorf("check if tx is in DB: %w", err)
			}
			continue
		}
		txs = append(txs, tx)
	}
	if err := msh.writeTransactions(layerID, blockID, txs...); err != nil {
		return fmt.Errorf("write tx: %w", err)
	}

	// remove txs from pool
	msh.invalidateFromPools(txIDs)
	return nil
}

func reportRewards(block *types.Block) {
	// Report the rewards for each coinbase and each smesherID within each coinbase.
	// This can be thought of as a partition of the reward amongst all the smesherIDs
	// that added the coinbase into the block.
	for _, r := range block.Rewards {
		events.ReportRewardReceived(events.Reward{
			Layer:       block.LayerIndex,
			Total:       r.Amount,
			LayerReward: r.LayerReward,
			Coinbase:    r.Address,
			Smesher:     r.SmesherID,
		})
	}
}

// GetATXs uses GetFullAtx to return a list of atxs corresponding to atxIds requested.
func (msh *Mesh) GetATXs(ctx context.Context, atxIds []types.ATXID) (map[types.ATXID]*types.ActivationTx, []types.ATXID) {
	var mIds []types.ATXID
	atxs := make(map[types.ATXID]*types.ActivationTx, len(atxIds))
	for _, id := range atxIds {
		t, err := msh.GetFullAtx(id)
		if err != nil {
			msh.WithContext(ctx).With().Warning("could not get atx from database", id, log.Err(err))
			mIds = append(mIds, id)
		} else {
			atxs[t.ID()] = t
		}
	}
	return atxs, mIds
}

func getAggregatedLayerHashKey(layerID types.LayerID) []byte {
	var b bytes.Buffer
	b.WriteString("ag")
	b.Write(layerID.Bytes())
	return b.Bytes()
}

func minLayer(i, j types.LayerID) types.LayerID {
	if i.Before(j) {
		return i
	}
	return j
}

func maxLayer(i, j types.LayerID) types.LayerID {
	if i.After(j) {
		return i
	}
	return j
}
