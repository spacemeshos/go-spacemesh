// Package mesh defines the main store point for all the persisted mesh objects
// such as ATXs, ballots and blocks.
package mesh

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"go.uber.org/atomic"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/opinionhash"
)

var errMissingHareOutput = errors.New("missing hare output")

// Mesh is the logic layer above our mesh.DB database.
type Mesh struct {
	logger log.Log
	cdb    *datastore.CachedDB

	conState conservativeState
	trtl     system.Tortoise

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
	validityUpdates     map[types.LayerID]map[types.BlockID]bool
}

// NewMesh creates a new instant of a mesh.
func NewMesh(cdb *datastore.CachedDB, trtl system.Tortoise, state conservativeState, logger log.Log) (*Mesh, error) {
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

	lid, err := ballots.LatestLayer(cdb)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("get latest layer %w", err)
	}

	if err == nil && lid != (types.LayerID{}) {
		msh.recoverFromDB(lid)
		return msh, nil
	}

	gLid := types.GetEffectiveGenesis()
	if err = cdb.WithTx(context.Background(), func(dbtx *sql.Tx) error {
		for i := types.NewLayerID(1); !i.After(gLid); i = i.Add(1) {
			if err = layers.SetProcessed(dbtx, i); err != nil {
				return fmt.Errorf("mesh init: %w", err)
			}
			if err = layers.SetApplied(dbtx, i, types.EmptyBlockID); err != nil {
				return fmt.Errorf("mesh init: %w", err)
			}
		}
		return persistLayerHashes(msh.logger, dbtx, gLid, nil)
	}); err != nil {
		msh.logger.With().Panic("error initialize genesis data", log.Err(err))
	}

	msh.setLatestLayer(msh.logger, gLid)
	msh.processedLayer.Store(gLid)
	msh.setLatestLayerInState(gLid)
	return msh, nil
}

func (msh *Mesh) recoverFromDB(latest types.LayerID) {
	msh.setLatestLayer(msh.logger, latest)

	lyr, err := layers.GetProcessed(msh.cdb)
	if err != nil {
		msh.logger.With().Fatal("failed to recover processed layer", log.Err(err))
	}
	msh.processedLayer.Store(lyr)

	applied, err := layers.GetLastApplied(msh.cdb)
	if err != nil {
		msh.logger.With().Fatal("failed to recover latest applied layer", log.Err(err))
	}
	msh.setLatestLayerInState(applied)

	root := types.Hash32{}
	if applied.After(types.GetEffectiveGenesis()) {
		err = msh.conState.RevertState(msh.LatestLayerInState())
		if err != nil {
			msh.logger.With().Fatal("failed to load state for layer", msh.LatestLayerInState(), log.Err(err))
		}
	}

	msh.logger.With().Info("recovered mesh from disk",
		log.Stringer("latest", msh.LatestLayer()),
		log.Stringer("processed", msh.ProcessedLayer()),
		log.Stringer("root_hash", root))
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
func (msh *Mesh) setLatestLayer(logger log.Log, lid types.LayerID) {
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
			logger.With().Info("set latest known layer", lid)
		}
	}
}

// GetLayer returns GetLayer i from the database.
func (msh *Mesh) GetLayer(lid types.LayerID) (*types.Layer, error) {
	return getLayer(msh.cdb, lid)
}

func getLayer(db sql.Executor, lid types.LayerID) (*types.Layer, error) {
	blts, err := ballots.Layer(db, lid)
	if err != nil {
		return nil, fmt.Errorf("layer ballots: %w", err)
	}
	blks, err := blocks.Layer(db, lid)
	if err != nil {
		return nil, fmt.Errorf("layer blks: %w", err)
	}
	hash, err := layers.GetHash(db, lid)
	if err != nil {
		hash = types.CalcBlockHash32Presorted(types.ToBlockIDs(blks), nil)
	}
	return types.NewExistingLayer(lid, hash, blts, blks), nil
}

// ProcessedLayer returns the last processed layer ID.
func (msh *Mesh) ProcessedLayer() types.LayerID {
	return msh.processedLayer.Load().(types.LayerID)
}

func (msh *Mesh) setProcessedLayer(logger log.Log, layerID types.LayerID) error {
	processed := msh.ProcessedLayer()
	if !layerID.After(processed) {
		logger.With().Info("trying to set processed layer to an older layer",
			log.FieldNamed("processed", processed),
			layerID)
		return nil
	}

	if layerID.After(msh.maxProcessedLayer) {
		msh.maxProcessedLayer = layerID
	}

	if layerID != processed.Add(1) {
		logger.With().Info("trying to set processed layer out of order",
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
	logger.Event().Info("processed layer set", processed)
	return nil
}

func (msh *Mesh) processValidityUpdates(ctx context.Context, logger log.Log, newVerified types.LayerID, updated []types.BlockContextualValidity) error {
	if len(updated) == 0 && msh.validityUpdates == nil {
		return nil
	}
	for _, bv := range updated {
		if msh.validityUpdates == nil {
			msh.validityUpdates = make(map[types.LayerID]map[types.BlockID]bool)
		}
		if _, ok := msh.validityUpdates[bv.Layer]; !ok {
			msh.validityUpdates[bv.Layer] = make(map[types.BlockID]bool)
		}
		msh.validityUpdates[bv.Layer][bv.ID] = bv.Validity
	}
	logger.With().Debug("processing validity update",
		log.Int("num_updates", len(msh.validityUpdates)),
		log.Object("updates", log.ObjectMarshallerFunc(func(encoder log.ObjectEncoder) error {
			for lid, bvs := range msh.validityUpdates {
				encoder.AddUint32("layer_id", lid.Uint32())
				for bid, valid := range bvs {
					encoder.AddString("block_id", bid.String())
					encoder.AddBool("valid", valid)
				}
			}
			return nil
		})))

	var minChanged types.LayerID
	inState := msh.LatestLayerInState()
	for lid, bvs := range msh.validityUpdates {
		if lid.After(inState) {
			continue
		}
		logger := logger.WithFields(lid)
		valids, err := layerValidBlocks(logger, msh.cdb, lid, newVerified, bvs)
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
			logger.With().Info("incorrect block applied",
				log.Stringer("expected", bid),
				log.Stringer("applied", applied))
			if minChanged == (types.LayerID{}) || lid.Before(minChanged) {
				minChanged = lid
			}
		}
	}

	if minChanged != (types.LayerID{}) {
		revertTo := minChanged.Sub(1)
		logger := logger.WithFields(log.Stringer("revert_to", revertTo))
		logger.Info("reverting state")
		if err := msh.revertState(logger, revertTo); err != nil {
			logger.With().Error("failed to revert state", log.Err(err))
			return err
		}
		msh.setLatestLayerInState(revertTo)
	}

	if err := msh.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		for lid, updates := range msh.validityUpdates {
			for bid, valid := range updates {
				logger.With().Info("save block contextual validity",
					bid, lid, log.Bool("validity", valid))
				if valid {
					if err := blocks.SetValid(dbtx, bid); err != nil {
						return err
					}
				} else if err := blocks.SetInvalid(dbtx, bid); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		logger.With().Error("failed to save block validity", log.Err(err))
		return err
	}
	msh.validityUpdates = nil
	return nil
}

// ProcessLayer performs fairly heavy lifting: it triggers tortoise to process the full contents of the layer (i.e.,
// all of its blocks), then to attempt to validate all unvalidated layers up to this layer. It also applies state for
// newly-validated layers.
func (msh *Mesh) ProcessLayer(ctx context.Context, layerID types.LayerID) error {
	logger := msh.logger.WithContext(ctx).WithFields(log.Stringer("processing", layerID))

	// pass the layer to tortoise for processing
	logger.Debug("tallying votes")
	msh.trtl.TallyVotes(ctx, layerID)

	msh.mu.Lock()
	defer msh.mu.Unlock()

	logger.Info("processing layer")

	// set processed layer even if later code will fail, as that failure is not related
	// to the layer that is being processed
	if err := msh.setProcessedLayer(logger, layerID); err != nil {
		return err
	}

	newVerified, updated := msh.trtl.Updates()
	logger = logger.WithFields(log.Stringer("verified", newVerified))
	if err := msh.processValidityUpdates(ctx, logger, newVerified, updated); err != nil {
		return err
	}

	// mesh can't skip layer that failed to complete
	from := msh.LatestLayerInState().Add(1)
	to := layerID
	if from == msh.MissingLayer() {
		to = msh.maxProcessedLayer
	}

	if !to.Before(from) {
		if err := msh.pushLayersToState(ctx, logger, from, to, newVerified); err != nil {
			logger.With().Error("failed to push layers to state", log.Err(err))
			return err
		}
	}
	return nil
}

func persistLayerHashes(logger log.Log, dbtx *sql.Tx, lid types.LayerID, valids []*types.Block) error {
	logger.With().Debug("persisting layer hash", log.Int("num_blocks", len(valids)))
	sortBlocks(valids)
	var (
		hash   = types.CalcBlocksHash32(types.ToBlockIDs(valids), nil)
		hasher = opinionhash.New()
		err    error
	)
	if lid.After(types.GetEffectiveGenesis()) {
		prev, err := layers.GetAggregatedHash(dbtx, lid.Sub(1))
		if err != nil {
			logger.With().Error("failed to get previous aggregated hash", lid, log.Err(err))
			return err
		}
		logger.With().Debug("got previous aggregatedHash", log.String("prevAggHash", prev.ShortString()))
		hasher.WritePrevious(prev)
	}
	for _, block := range valids {
		hasher.WriteSupport(block.ID(), block.TickHeight)
	}
	newAggHash := hasher.Hash()
	if err = layers.SetHashes(dbtx, lid, hash, hasher.Hash()); err != nil {
		logger.With().Error("failed to set layer hashes", log.Err(err))
		return err
	}
	logger.With().Info("layer hashes updated",
		log.String("hash", hash.ShortString()),
		log.String("agg_hash", newAggHash.ShortString()))
	return nil
}

// apply the state of a range of layers, including re-adding transactions from invalid blocks to the mempool.
func (msh *Mesh) pushLayersToState(ctx context.Context, logger log.Log, from, to, latestVerified types.LayerID) error {
	logger = logger.WithFields(
		log.Stringer("from", from),
		log.Stringer("to", to))
	logger.Info("pushing layers to state")
	if from.Before(types.GetEffectiveGenesis()) || to.Before(types.GetEffectiveGenesis()) {
		logger.Fatal("tried to push genesis layers")
	}

	missing := msh.MissingLayer()
	// we never reapply the state of oldVerified. note that state reversions must be handled separately.
	for layerID := from; !layerID.After(to); layerID = layerID.Add(1) {
		logger := logger.WithFields(layerID)
		if !layerID.After(msh.LatestLayerInState()) {
			logger.With().Error("trying to apply layer before currently applied layer",
				log.Stringer("in_state", msh.LatestLayerInState()))
			continue
		}
		if err := msh.pushLayer(ctx, logger, layerID, latestVerified); err != nil {
			msh.missingLayer.Store(layerID)
			return err
		}
		if layerID == missing {
			msh.missingLayer.Store(types.LayerID{})
		}
	}

	return nil
}

func (msh *Mesh) getBlockToApply(validBlocks []*types.Block) *types.Block {
	if len(validBlocks) == 0 {
		return nil
	}
	sorted := sortBlocks(validBlocks)
	return sorted[0]
}

func layerValidBlocks(logger log.Log, cdb *datastore.CachedDB, layerID, latestVerified types.LayerID, updates map[types.BlockID]bool) ([]*types.Block, error) {
	lyrBlocks, err := blocks.Layer(cdb, layerID)
	if err != nil {
		logger.With().Error("failed to get layer blocks", log.Err(err))
		return nil, fmt.Errorf("failed to get layer blocks %s: %w", layerID, err)
	}

	var validBlocks []*types.Block
	logger.With().Debug("getting layer valid blocks", log.Stringer("verified", latestVerified))
	if layerID.After(latestVerified) {
		// tortoise has not verified this layer yet, simply apply the block that hare certified
		bid, err := certificates.GetHareOutput(cdb, layerID)
		if err != nil {
			logger.With().Warning("failed to get hare output", layerID, log.Err(err))
			return nil, fmt.Errorf("%w: get hare output %v", errMissingHareOutput, err.Error())
		}
		// hare output an empty layer, or the network have multiple valid certificates
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
		var valid, found bool
		if updates != nil {
			if v, ok := updates[b.ID()]; ok {
				valid = v
				found = true
			}
		}
		if !found {
			valid, err = blocks.IsValid(cdb, b.ID())
			if err != nil && !errors.Is(err, sql.ErrNotFound) {
				return nil, fmt.Errorf("get block validity: %w", err)
			}
			if err == nil {
				logger.With().Debug("using persisted validity", b.ID(), log.Bool("validity", valid))
			} else {
				logger.With().Debug("block validity not determined", b.ID())
			}
		}
		if valid {
			validBlocks = append(validBlocks, b)
		}
	}
	return validBlocks, nil
}

func (msh *Mesh) pushLayer(ctx context.Context, logger log.Log, lid, verified types.LayerID) error {
	if latest := msh.LatestLayerInState(); lid != latest.Add(1) {
		logger.With().Fatal("update state out-of-order", log.Stringer("latest", latest))
	}

	valids, err := layerValidBlocks(logger, msh.cdb, lid, verified, nil)
	if err != nil {
		return err
	}
	if err = msh.applyState(ctx, logger, lid, valids); err != nil {
		return err
	}
	msh.setLatestLayerInState(lid)

	root, err := msh.conState.GetStateRoot()
	if err != nil {
		return fmt.Errorf("failed to oad state root after update: %w", err)
	}
	logger.Event().Info("end of layer state root", log.Stringer("state_root", root))
	return nil
}

// applyState applies the block to the conservative state / vm and updates mesh's internal state.
// ideally everything happens here should be atomic.
// see https://github.com/spacemeshos/go-spacemesh/issues/3333
func (msh *Mesh) applyState(ctx context.Context, logger log.Log, lid types.LayerID, valids []*types.Block) error {
	applied := types.EmptyBlockID
	block := msh.getBlockToApply(valids)
	if block != nil {
		applied = block.ID()
	}

	if err := msh.conState.ApplyLayer(ctx, lid, block); err != nil {
		logger.With().Error("failed to apply transactions", log.Err(err))
		return fmt.Errorf("apply layer: %w", err)
	}

	logger = logger.WithFields(log.Stringer("applied", applied))
	logger.With().Info("applying block for layer")

	if err := msh.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		if err := layers.SetApplied(dbtx, lid, applied); err != nil {
			return fmt.Errorf("set applied for %v/%v: %w", lid, applied, err)
		}
		if err := persistLayerHashes(logger, dbtx, lid, valids); err != nil {
			logger.With().Error("failed to persist layer hashes", log.Err(err))
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: lid,
		Status:  events.LayerStatusTypeApplied,
	})
	return nil
}

// revertState reverts the conservative state / vm and updates mesh's internal state.
// ideally everything happens here should be atomic.
// see https://github.com/spacemeshos/go-spacemesh/issues/3333
func (msh *Mesh) revertState(logger log.Log, revertTo types.LayerID) error {
	if err := msh.conState.RevertState(revertTo); err != nil {
		return fmt.Errorf("revert state to layer %v: %w", revertTo, err)
	}
	if err := layers.UnsetAppliedFrom(msh.cdb, revertTo.Add(1)); err != nil {
		return fmt.Errorf("unset applied from layer %v: %w", revertTo.Add(1), err)
	}
	stateHash, err := msh.conState.GetStateRoot()
	if err != nil {
		return fmt.Errorf("after revert get state: %w", err)
	}
	logger.With().Info("successfully reverted state", log.Stringer("state_hash", stateHash))
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
	if err := certificates.SetHareOutput(msh.cdb, layerID, blockID); err != nil {
		logger.With().Error("failed to save hare output", log.Err(err))
		return err
	}
	msh.trtl.OnHareOutput(layerID, blockID)
	return msh.ProcessLayer(ctx, layerID)
}

func (msh *Mesh) setLatestLayerInState(lyr types.LayerID) {
	msh.latestLayerInState.Store(lyr)
}

// SetZeroBlockLayer advances the latest layer in the network with a layer
// that truly has no data.
func (msh *Mesh) SetZeroBlockLayer(ctx context.Context, lid types.LayerID) {
	msh.setLatestLayer(msh.logger.WithContext(ctx), lid)
}

// AddTXsFromProposal adds the TXs in a Proposal into the database.
func (msh *Mesh) AddTXsFromProposal(ctx context.Context, layerID types.LayerID, proposalID types.ProposalID, txIDs []types.TransactionID) error {
	logger := msh.logger.WithContext(ctx).WithFields(layerID, proposalID, log.Int("num_txs", len(txIDs)))
	if err := msh.conState.LinkTXsWithProposal(layerID, proposalID, txIDs); err != nil {
		logger.With().Error("failed to link proposal txs", log.Err(err))
		return err
	}
	msh.setLatestLayer(logger, layerID)
	logger.Debug("associated txs to proposal")
	return nil
}

// AddBallot to the mesh.
func (msh *Mesh) AddBallot(ctx context.Context, ballot *types.Ballot) error {
	malicious, err := identities.IsMalicious(msh.cdb, ballot.SmesherID().Bytes())
	if err != nil {
		return err
	}
	if malicious {
		ballot.SetMalicious()
	}
	// ballots.Add and ballots.Count should be atomic
	// otherwise concurrent ballots.Add from the same smesher may not be noticed
	return msh.cdb.WithTx(ctx, func(tx *sql.Tx) error {
		if err := ballots.Add(tx, ballot); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return err
		}
		if !malicious {
			count, err := ballots.CountByPubkeyLayer(tx, ballot.LayerIndex, ballot.SmesherID().Bytes())
			if err != nil {
				return err
			}
			if count > 1 {
				if err := identities.SetMalicious(tx, ballot.SmesherID().Bytes()); err != nil {
					return err
				}
				ballot.SetMalicious()
				msh.logger.With().Warning("smesher produced more than one ballot in the same layer",
					log.Stringer("smesher", ballot.SmesherID()),
					log.Inline(ballot),
				)
			}
		}
		return nil
	})
}

// AddBlockWithTXs adds the block and its TXs in into the database.
func (msh *Mesh) AddBlockWithTXs(ctx context.Context, block *types.Block) error {
	logger := msh.logger.WithContext(ctx).WithFields(block.LayerIndex, block.ID(), log.Int("num_txs", len(block.TxIDs)))
	if err := msh.conState.LinkTXsWithBlock(block.LayerIndex, block.ID(), block.TxIDs); err != nil {
		logger.With().Error("failed to link block txs", log.Err(err))
		return err
	}
	msh.setLatestLayer(logger, block.LayerIndex)
	logger.Debug("associated txs to block")

	// add block to the tortoise before storing it
	// otherwise fetcher will not wait until data is stored in the tortoise
	msh.trtl.OnBlock(block)
	if err := blocks.Add(msh.cdb, block); err != nil && !errors.Is(err, sql.ErrObjectExists) {
		return err
	}
	return nil
}

// GetATXs uses GetFullAtx to return a list of atxs corresponding to atxIds requested.
func (msh *Mesh) GetATXs(ctx context.Context, atxIds []types.ATXID) (map[types.ATXID]*types.VerifiedActivationTx, []types.ATXID) {
	var mIds []types.ATXID
	atxs := make(map[types.ATXID]*types.VerifiedActivationTx, len(atxIds))
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

// sortBlocks sort blocks tick height, if height is equal by lexicographic order.
func sortBlocks(blks []*types.Block) []*types.Block {
	sort.Slice(blks, func(i, j int) bool {
		if blks[i].TickHeight != blks[j].TickHeight {
			return blks[i].TickHeight < blks[j].TickHeight
		}
		return blks[i].ID().Compare(blks[j].ID())
	})
	return blks
}

// LastVerified returns the latest layer verified by tortoise.
func (msh *Mesh) LastVerified() types.LayerID {
	return msh.trtl.LatestComplete()
}
