// Package mesh defines the main store point for all the persisted mesh objects
// such as ATXs, ballots and blocks.
package mesh

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"sync"

	"go.uber.org/atomic"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

var (
	errMissingHareOutput = errors.New("missing hare output")
	errLayerHasBlock     = errors.New("layer has block")
)

// AtxDB holds logic for working with atxs.
type AtxDB interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
	GetFullAtx(id types.ATXID) (*types.ActivationTx, error)
}

// Mesh is the logic layer above our mesh.DB database.
type Mesh struct {
	logger log.Log
	cdb    *datastore.CachedDB

	conState conservativeState
	trtl     tortoise

	mu sync.Mutex
	// latestLayer is the latest layer this node had seen from blocks
	latestLayer atomic.Value
	// latestLayerInState is the latest layer whose contents have been applied to the state
	latestLayerInState atomic.Value
	// processedLayer is the latest layer whose votes have been processed
	processedLayer atomic.Value
	// see doc for MissingLayer()
	missingLayer        atomic.Value
	nextProcessedLayers map[types.LayerID]struct{}
	maxProcessedLayer   types.LayerID

	// minUpdatedLayer is the earliest layer that have contextual validity updated.
	// since we optimistically apply blocks to state whenever hare terminates a layer,
	// if contextual validity changed for blocks in that layer, we need to
	// double-check whether we have applied the correct block for that layer.
	minUpdatedLayer atomic.Value
}

// NewMesh creates a new instant of a mesh.
func NewMesh(cdb *datastore.CachedDB, trtl tortoise, state conservativeState, logger log.Log) (*Mesh, error) {
	msh := &Mesh{
		logger:              logger,
		cdb:                 cdb,
		trtl:                trtl,
		conState:            state,
		nextProcessedLayers: make(map[types.LayerID]struct{}),
	}
	msh.latestLayer.Store(types.LayerID{})
	msh.latestLayerInState.Store(types.LayerID{})
	msh.processedLayer.Store(types.LayerID{})
	msh.minUpdatedLayer.Store(types.LayerID{})

	lid, err := ballots.LatestLayer(cdb)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("get latest layer %w", err)
	}

	if err == nil && lid != (types.LayerID{}) {
		msh.recoverFromDB(lid)
		return msh, nil
	}

	gLid := types.GetEffectiveGenesis()
	for i := types.NewLayerID(1); !i.After(gLid); i = i.Add(1) {
		if i.Before(gLid) {
			if err = msh.SetZeroBlockLayer(i); err != nil {
				msh.logger.With().Panic("failed to set zero-block for genesis layer", i, log.Err(err))
			}
		}
		if err = layers.SetApplied(msh.cdb, i, types.EmptyBlockID); err != nil {
			msh.logger.With().Panic("failed to set applied layer", i, log.Err(err))
		}
		if err = msh.persistLayerHashes(context.Background(), i, []types.BlockID{types.EmptyBlockID}); err != nil {
			msh.logger.With().Panic("failed to persist hashes for layer", i, log.Err(err))
		}
		if err = msh.setProcessedLayer(i); err != nil {
			msh.logger.With().Panic("failed to set processed layer", i, log.Err(err))
		}
		msh.setLatestLayerInState(i)
	}

	gLayer := types.GenesisLayer()
	for _, b := range gLayer.Ballots() {
		msh.logger.With().Info("adding genesis ballot", b.ID(), b.LayerIndex)
		if err = ballots.Add(msh.cdb, b); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			msh.logger.With().Error("error inserting genesis ballot to db", b.ID(), b.LayerIndex, log.Err(err))
		}
	}
	for _, b := range gLayer.Blocks() {
		msh.logger.With().Info("adding genesis block", b.ID(), b.LayerIndex)
		if err = blocks.Add(msh.cdb, b); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			msh.logger.With().Error("error inserting genesis block to db", b.ID(), b.LayerIndex, log.Err(err))
		}
		if err = blocks.SetValid(msh.cdb, b.ID()); err != nil {
			msh.logger.With().Error("error setting genesis block valid", b.ID(), b.LayerIndex, log.Err(err))
		}
	}
	if err = layers.SetHareOutput(msh.cdb, gLayer.Index(), types.GenesisBlockID); err != nil {
		log.With().Error("error inserting genesis block as hare output to db", gLayer.Index(), log.Err(err))
	}
	msh.setLatestLayer(gLid)
	return msh, nil
}

func (msh *Mesh) recoverFromDB(latest types.LayerID) {
	msh.setLatestLayer(latest)

	lyr, err := layers.GetProcessed(msh.cdb)
	if err != nil {
		msh.logger.With().Panic("failed to recover processed layer", log.Err(err))
	}
	msh.processedLayer.Store(lyr)

	applied, err := layers.GetLastApplied(msh.cdb)
	if err != nil {
		msh.logger.With().Panic("failed to recover latest applied layer", log.Err(err))
	}
	msh.setLatestLayerInState(applied)

	root := types.Hash32{}
	if applied.After(types.GetEffectiveGenesis()) {
		root, err = msh.conState.RevertState(msh.LatestLayerInState())
		if err != nil {
			msh.logger.With().Panic("failed to load state for layer", msh.LatestLayerInState(), log.Err(err))
		}
	}

	msh.logger.With().Info("recovered mesh from disk",
		log.Stringer("latest", msh.LatestLayer()),
		log.Stringer("processed", msh.ProcessedLayer()),
		log.Stringer("root_hash", root))
}

func (msh *Mesh) resetMinUpdatedLayer(from types.LayerID) {
	if msh.minUpdatedLayer.CompareAndSwap(from, types.LayerID{}) {
		msh.logger.With().Debug("min updated layer reset", log.Uint32("from", from.Uint32()))
	}
}

func (msh *Mesh) getMinUpdatedLayer() types.LayerID {
	value := msh.minUpdatedLayer.Load()
	if value == nil {
		return types.LayerID{}
	}
	return value.(types.LayerID)
}

// UpdateBlockValidity is the callback used when a block's contextual validity is updated by tortoise.
func (msh *Mesh) UpdateBlockValidity(bid types.BlockID, lid types.LayerID, newValid bool) error {
	msh.logger.With().Debug("updating validity for block", lid, bid)
	oldValid, err := blocks.IsValid(msh.cdb, bid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return fmt.Errorf("error reading contextual validity of block %v: %w", bid, err)
	}

	if oldValid != newValid {
		for {
			minUpdated := msh.getMinUpdatedLayer()
			if minUpdated != (types.LayerID{}) && !lid.Before(minUpdated) {
				break
			}
			if msh.minUpdatedLayer.CompareAndSwap(minUpdated, lid) {
				msh.logger.With().Debug("min updated layer set for block", lid, bid)
				break
			}
		}
	}

	if err = msh.saveContextualValidity(bid, lid, newValid); err != nil {
		return err
	}
	return nil
}

// saveContextualValidity persists opinion on block to the database.
func (msh *Mesh) saveContextualValidity(id types.BlockID, lid types.LayerID, valid bool) error {
	msh.logger.With().Debug("save block contextual validity", id, lid, log.Bool("validity", valid))
	if valid {
		return blocks.SetValid(msh.cdb, id)
	}
	return blocks.SetInvalid(msh.cdb, id)
}

// LatestLayerInState returns the latest layer we applied to state.
func (msh *Mesh) LatestLayerInState() types.LayerID {
	return msh.latestLayerInState.Load().(types.LayerID)
}

// LatestLayer - returns the latest layer we saw from the network.
func (msh *Mesh) LatestLayer() types.LayerID {
	return msh.latestLayer.Load().(types.LayerID)
}

// MissingLayer is a layer in (latestLayerInState, processLayer].
// this layer is missing critical data (valid blocks or transactions)
// and can't be applied to the state.
//
// First valid layer starts with 1. 0 is empty layer and can be ignored.
func (msh *Mesh) MissingLayer() types.LayerID {
	value := msh.missingLayer.Load()
	if value == nil {
		return types.LayerID{}
	}
	return value.(types.LayerID)
}

// setLatestLayer sets the latest layer we saw from the network.
func (msh *Mesh) setLatestLayer(lid types.LayerID) {
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: lid,
		Status:  events.LayerStatusTypeUnknown,
	})
	for {
		current := msh.LatestLayer()
		if !lid.After(current) {
			return
		}
		if msh.latestLayer.CompareAndSwap(current, lid) {
			events.ReportNodeStatusUpdate()
			msh.logger.With().Info("set latest known layer", lid)
		}
	}
}

// GetLayer returns GetLayer i from the database.
func (msh *Mesh) GetLayer(lid types.LayerID) (*types.Layer, error) {
	blts, err := ballots.Layer(msh.cdb, lid)
	if err != nil {
		return nil, fmt.Errorf("layer ballots: %w", err)
	}
	blks, err := blocks.Layer(msh.cdb, lid)
	if err != nil {
		return nil, fmt.Errorf("layer blks: %w", err)
	}
	hash, err := layers.GetHash(msh.cdb, lid)
	if err != nil {
		hash = types.CalcBlockHash32Presorted(types.ToBlockIDs(blks), nil)
	}
	return types.NewExistingLayer(lid, hash, blts, blks), nil
}

// ProcessedLayer returns the last processed layer ID.
func (msh *Mesh) ProcessedLayer() types.LayerID {
	return msh.processedLayer.Load().(types.LayerID)
}

func (msh *Mesh) setProcessedLayer(layerID types.LayerID) error {
	processed := msh.ProcessedLayer()
	if !layerID.After(processed) {
		msh.logger.With().Info("trying to set processed layer to an older layer",
			log.FieldNamed("processed", processed),
			layerID)
		return nil
	}

	if layerID.After(msh.maxProcessedLayer) {
		msh.maxProcessedLayer = layerID
	}

	if layerID != processed.Add(1) {
		msh.logger.With().Info("trying to set processed layer out of order",
			log.FieldNamed("processed", processed),
			layerID)
		msh.nextProcessedLayers[layerID] = struct{}{}
		return nil
	}

	msh.nextProcessedLayers[layerID] = struct{}{}
	for i := layerID; !i.After(msh.maxProcessedLayer); i = i.Add(1) {
		_, ok := msh.nextProcessedLayers[i]
		if !ok {
			break
		}
		processed = i
		delete(msh.nextProcessedLayers, i)
	}

	if err := layers.SetProcessed(msh.cdb, processed); err != nil {
		return fmt.Errorf("failed to set processed layer %v: %w", processed, err)
	}
	msh.processedLayer.Store(processed)
	events.ReportNodeStatusUpdate()
	msh.logger.Event().Info("processed layer set", processed)
	return nil
}

func (msh *Mesh) revertMaybe(ctx context.Context, logger log.Log, newVerified types.LayerID) error {
	minUpdated := msh.getMinUpdatedLayer()
	if minUpdated == (types.LayerID{}) {
		// no contextual validity update since the last ProcessLayer() call
		return nil
	}

	msh.resetMinUpdatedLayer(minUpdated)
	if minUpdated.After(msh.LatestLayerInState()) {
		// nothing to do
		return nil
	}

	var revertTo types.LayerID
	for lid := minUpdated; !lid.After(newVerified); lid = lid.Add(1) {
		valids, err := msh.layerValidBlocks(ctx, lid, newVerified)
		if err != nil {
			return err
		}

		bid := types.EmptyBlockID
		block := msh.getBlockToApply(valids)
		if block != nil {
			bid = block.ID()
		}
		applied, err := layers.GetApplied(msh.cdb, lid)
		if err != nil {
			return fmt.Errorf("get applied %v: %w", lid, err)
		}
		if bid != applied {
			revertTo = lid.Sub(1)
			break
		}
	}

	if revertTo == (types.LayerID{}) {
		// all the applied blocks are correct
		return nil
	}

	logger = logger.WithFields(log.Stringer("revert_to", revertTo))
	logger.Info("reverting state to layer")
	if err := msh.revertState(logger, revertTo); err != nil {
		logger.With().Error("failed to revert state",
			log.Uint32("revert_to", revertTo.Uint32()),
			log.Err(err))
		return err
	}
	msh.setLatestLayerInState(revertTo)
	logger.With().Info("reverted state to layer", log.Uint32("revert_to", revertTo.Uint32()))
	return nil
}

// ProcessLayer performs fairly heavy lifting: it triggers tortoise to process the full contents of the layer (i.e.,
// all of its blocks), then to attempt to validate all unvalidated layers up to this layer. It also applies state for
// newly-validated layers.
func (msh *Mesh) ProcessLayer(ctx context.Context, layerID types.LayerID) error {
	msh.mu.Lock()
	defer msh.mu.Unlock()

	logger := msh.logger.WithContext(ctx).WithFields(layerID)
	logger.Info("processing layer")

	// pass the layer to tortoise for processing
	newVerified := msh.trtl.HandleIncomingLayer(ctx, layerID)
	logger.With().Info("tortoise results", log.FieldNamed("verified", newVerified))

	// set processed layer even if later code will fail, as that failure is not related
	// to the layer that is being processed
	if err := msh.setProcessedLayer(layerID); err != nil {
		return err
	}

	if err := msh.revertMaybe(ctx, logger, newVerified); err != nil {
		return err
	}

	// mesh can't skip layer that failed to complete
	from := msh.LatestLayerInState().Add(1)
	to := layerID
	if from == msh.MissingLayer() {
		to = msh.maxProcessedLayer
	}

	if !to.Before(from) {
		if err := msh.pushLayersToState(ctx, from, to, newVerified); err != nil {
			logger.With().Error("failed to push layers to state", log.Err(err))
			return err
		}
	}

	logger.Info("done processing layer")
	return nil
}

func (msh *Mesh) persistLayerHashes(ctx context.Context, lid types.LayerID, bids []types.BlockID) error {
	logger := msh.logger.WithContext(ctx)
	logger.With().Debug("persisting layer hash", lid, log.Int("num_blocks", len(bids)))
	types.SortBlockIDs(bids)
	var (
		hash     = types.CalcBlocksHash32(bids, nil)
		prevHash = types.EmptyLayerHash
		err      error
	)
	if prevLid := lid.Sub(1); prevLid.Uint32() > 0 {
		prevHash, err = layers.GetAggregatedHash(msh.cdb, prevLid)
		if err != nil {
			logger.With().Error("failed to get previous aggregated hash", lid, log.Err(err))
			return err
		}
	}

	logger.With().Debug("got previous aggregatedHash", lid, log.String("prevAggHash", prevHash.ShortString()))
	newAggHash := types.CalcBlocksHash32(bids, prevHash.Bytes())
	if err = layers.SetHashes(msh.cdb, lid, hash, newAggHash); err != nil {
		logger.With().Error("failed to set layer hashes", lid, log.Err(err))
		return err
	}
	logger.With().Info("layer hashes updated",
		lid,
		log.String("hash", hash.ShortString()),
		log.String("agg_hash", newAggHash.ShortString()))
	return nil
}

// apply the state of a range of layers, including re-adding transactions from invalid blocks to the mempool.
func (msh *Mesh) pushLayersToState(ctx context.Context, from, to, latestVerified types.LayerID) error {
	logger := msh.logger.WithContext(ctx).WithFields(
		log.Stringer("from_layer", from),
		log.Stringer("to_layer", to))
	logger.Info("pushing layers to state")
	if from.Before(types.GetEffectiveGenesis()) || to.Before(types.GetEffectiveGenesis()) {
		logger.Panic("tried to push genesis layers")
	}

	missing := msh.MissingLayer()
	// we never reapply the state of oldVerified. note that state reversions must be handled separately.
	for layerID := from; !layerID.After(to); layerID = layerID.Add(1) {
		if !layerID.After(msh.LatestLayerInState()) {
			logger.With().Error("trying to apply layer before currently applied layer",
				log.Stringer("applied_layer", msh.LatestLayerInState()),
				layerID,
			)
			continue
		}
		if err := msh.pushLayer(ctx, layerID, latestVerified); err != nil {
			msh.missingLayer.Store(layerID)
			return err
		}
		if layerID == missing {
			msh.missingLayer.Store(types.LayerID{})
		}
	}

	return nil
}

// TODO: change this per conclusion in https://community.spacemesh.io/t/new-history-reversal-attack-and-mitigation/268
func (msh *Mesh) getBlockToApply(validBlocks []*types.Block) *types.Block {
	if len(validBlocks) == 0 {
		return nil
	}
	sorted := types.SortBlocks(validBlocks)
	return sorted[0]
}

func (msh *Mesh) layerValidBlocks(ctx context.Context, layerID, latestVerified types.LayerID) ([]*types.Block, error) {
	lyrBlocks, err := blocks.Layer(msh.cdb, layerID)
	if err != nil {
		msh.logger.WithContext(ctx).With().Error("failed to get layer blocks", layerID, log.Err(err))
		return nil, fmt.Errorf("failed to get layer blocks %s: %w", layerID, err)
	}
	var validBlocks []*types.Block

	if layerID.After(latestVerified) {
		// tortoise has not verified this layer yet, simply apply the block that hare certified
		bid, err := layers.GetHareOutput(msh.cdb, layerID)
		if err != nil {
			msh.logger.WithContext(ctx).With().Error("failed to get hare output", layerID, log.Err(err))
			return nil, fmt.Errorf("%w: get hare output %v", errMissingHareOutput, err.Error())
		}
		// hare output an empty layer
		if bid == types.EmptyBlockID {
			return nil, nil
		}
		for _, b := range lyrBlocks {
			if b.ID() == bid {
				validBlocks = append(validBlocks, b)
				break
			}
		}
		return validBlocks, nil
	}

	for _, b := range lyrBlocks {
		valid, err := blocks.IsValid(msh.cdb, b.ID())
		// block contextual validity is determined by layer. if one block in the layer is not determined,
		// the whole layer is not yet verified.
		if err != nil {
			msh.logger.With().Warning("block contextual validity not yet determined", b.ID(), b.LayerIndex, log.Err(err))
			return nil, fmt.Errorf("get block validity: %w", err)
		}
		if valid {
			validBlocks = append(validBlocks, b)
		}
	}
	return validBlocks, nil
}

func (msh *Mesh) pushLayer(ctx context.Context, layerID, latestVerified types.LayerID) error {
	valids, err := msh.layerValidBlocks(ctx, layerID, latestVerified)
	if err != nil {
		return err
	}
	toApply := msh.getBlockToApply(valids)

	if err = msh.updateStateWithLayer(ctx, layerID, toApply); err != nil {
		return fmt.Errorf("failed to update state %s: %w", layerID, err)
	}

	root, err := msh.conState.GetStateRoot()
	if err != nil {
		return fmt.Errorf("failed to oad state root after update: %w", err)
	}

	msh.logger.Event().Info("end of layer state root",
		layerID,
		log.Stringer("state_root", root),
	)

	if err = msh.persistLayerHashes(ctx, layerID, types.ToBlockIDs(valids)); err != nil {
		msh.logger.With().Error("failed to persist layer hashes", layerID, log.Err(err))
		return err
	}
	return nil
}

// revertState reverts to state as of a previous layer.
func (msh *Mesh) revertState(logger log.Log, revertTo types.LayerID) error {
	root, err := msh.conState.RevertState(revertTo)
	if err != nil {
		return fmt.Errorf("revert state to layer %v: %w", revertTo, err)
	}
	logger.With().Info("successfully reverted state", log.Stringer("state_root", root))

	if err = layers.UnsetAppliedFrom(msh.cdb, revertTo.Add(1)); err != nil {
		logger.With().Error("failed to unset applied layer", log.Err(err))
		return fmt.Errorf("unset applied from layer %v: %w", revertTo.Add(1), err)
	}
	logger.Info("successfully unset applied layer")
	return nil
}

func (msh *Mesh) applyState(block *types.Block) error {
	failedTxs, err := msh.conState.ApplyLayer(block)
	if err != nil {
		msh.logger.With().Error("failed to apply transactions",
			block.LayerIndex,
			log.Int("num_failed_txs", len(failedTxs)),
			log.Err(err))
		return fmt.Errorf("apply layer: %w", err)
	}

	msh.logger.With().Info("applied transactions",
		block.LayerIndex,
		log.Int("valid_block_txs", len(block.TxIDs)),
		log.Int("num_failed_txs", len(failedTxs)),
	)
	return nil
}

// ProcessLayerPerHareOutput receives hare output once it finishes running for a given layer.
func (msh *Mesh) ProcessLayerPerHareOutput(ctx context.Context, layerID types.LayerID, blockID types.BlockID) error {
	logger := msh.logger.WithContext(ctx).WithFields(layerID, blockID)
	if blockID == types.EmptyBlockID {
		logger.Info("received empty set from hare")
	} else {
		// double-check we have this block in the mesh
		_, err := blocks.Get(msh.cdb, blockID)
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
	if err := layers.SetHareOutput(msh.cdb, layerID, blockID); err != nil {
		logger.With().Error("failed to save hare output", log.Err(err))
		return err
	}
	return msh.ProcessLayer(ctx, layerID)
}

// apply the state for a single layer.
func (msh *Mesh) updateStateWithLayer(ctx context.Context, layerID types.LayerID, block *types.Block) error {
	if latest := msh.LatestLayerInState(); layerID != latest.Add(1) {
		msh.logger.WithContext(ctx).With().Panic("update state out-of-order",
			log.FieldNamed("verified", layerID),
			log.FieldNamed("latest", latest))
	}

	applied := types.EmptyBlockID
	if block != nil {
		if err := msh.applyState(block); err != nil {
			return err
		}
		applied = block.ID()
	}

	if err := layers.SetApplied(msh.cdb, layerID, applied); err != nil {
		return fmt.Errorf("set applied for %v/%v: %w", layerID, applied, err)
	}
	msh.setLatestLayerInState(layerID)
	return nil
}

func (msh *Mesh) setLatestLayerInState(lyr types.LayerID) {
	msh.latestLayerInState.Store(lyr)
}

// SetZeroBlockLayer tags lyr as a layer without blocks.
func (msh *Mesh) SetZeroBlockLayer(lyr types.LayerID) error {
	msh.logger.With().Info("tagging zero block layer", lyr)
	// check database for layer
	if l, err := msh.GetLayer(lyr); err != nil {
		// database error
		if !errors.Is(err, sql.ErrNotFound) {
			msh.logger.With().Error("error trying to fetch layer from database", lyr, log.Err(err))
			return err
		}
	} else if len(l.Blocks()) != 0 {
		// layer exists
		msh.logger.With().Error("layer has blocks, cannot tag as zero block layer",
			lyr,
			l,
			log.Int("num_blocks", len(l.Blocks())))
		return errLayerHasBlock
	}

	msh.setLatestLayer(lyr)

	// layer doesn't exist, need to insert new layer
	return layers.SetHareOutput(msh.cdb, lyr, types.EmptyBlockID)
}

// AddTXsFromProposal adds the TXs in a Proposal into the database.
func (msh *Mesh) AddTXsFromProposal(ctx context.Context, layerID types.LayerID, proposalID types.ProposalID, txIDs []types.TransactionID) error {
	logger := msh.logger.WithContext(ctx).WithFields(layerID, proposalID, log.Int("num_txs", len(txIDs)))
	if err := msh.conState.LinkTXsWithProposal(layerID, proposalID, txIDs); err != nil {
		logger.With().Error("failed to link proposal txs", log.Err(err))
		return err
	}
	msh.setLatestLayer(layerID)
	logger.Debug("associated txs to proposal")
	return nil
}

// AddBallot to the mesh.
func (msh *Mesh) AddBallot(ballot *types.Ballot) error {
	if err := msh.addBallot(ballot); err != nil {
		return err
	}
	msh.trtl.OnBallot(ballot)
	return nil
}

func (msh *Mesh) addBallot(b *types.Ballot) error {
	mal, err := identities.IsMalicious(msh.cdb, b.SmesherID().Bytes())
	if err != nil {
		return err
	}
	if mal {
		b.SetMalicious()
	}

	// it is important to run add ballot and set identity to malicious atomically
	tx, err := msh.cdb.Tx(context.Background())
	if err != nil {
		return err
	}
	defer tx.Release()

	if err := ballots.Add(tx, b); err != nil && !errors.Is(err, sql.ErrObjectExists) {
		return err
	}
	if !mal {
		count, err := ballots.CountByPubkeyLayer(tx, b.LayerIndex, b.SmesherID().Bytes())
		if err != nil {
			return err
		}
		if count > 1 {
			if err := identities.SetMalicious(tx, b.SmesherID().Bytes()); err != nil {
				return err
			}
			b.SetMalicious()
			msh.logger.With().Warning("smesher produced more than one ballot in the same layer",
				log.Stringer("smesher", b.SmesherID()),
				log.Inline(b),
			)
		}
	}
	return tx.Commit()
}

// AddBlockWithTXs adds the block and its TXs in into the database.
func (msh *Mesh) AddBlockWithTXs(ctx context.Context, block *types.Block) error {
	logger := msh.logger.WithContext(ctx).WithFields(block.LayerIndex, block.ID(), log.Int("num_txs", len(block.TxIDs)))
	if err := msh.conState.LinkTXsWithBlock(block.LayerIndex, block.ID(), block.TxIDs); err != nil {
		logger.With().Error("failed to link block txs", log.Err(err))
		return err
	}
	msh.setLatestLayer(block.LayerIndex)
	logger.Debug("associated txs to block")

	if err := blocks.Add(msh.cdb, block); err != nil && !errors.Is(err, sql.ErrObjectExists) {
		return err
	}
	msh.trtl.OnBlock(block)
	return nil
}

// GetATXs uses GetFullAtx to return a list of atxs corresponding to atxIds requested.
func (msh *Mesh) GetATXs(ctx context.Context, atxIds []types.ATXID) (map[types.ATXID]*types.ActivationTx, []types.ATXID) {
	var mIds []types.ATXID
	atxs := make(map[types.ATXID]*types.ActivationTx, len(atxIds))
	for _, id := range atxIds {
		t, err := msh.cdb.GetFullAtx(id)
		if err != nil {
			msh.logger.WithContext(ctx).With().Warning("could not get atx from database", id, log.Err(err))
			mIds = append(mIds, id)
		} else {
			atxs[t.ID()] = t
		}
	}
	return atxs, mIds
}

// GetRewards retrieves account's rewards by the coinbase address.
func (msh *Mesh) GetRewards(coinbase types.Address) ([]*types.Reward, error) {
	return rewards.List(msh.cdb, coinbase)
}
