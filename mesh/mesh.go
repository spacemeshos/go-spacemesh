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
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/opinionhash"
	"github.com/spacemeshos/go-spacemesh/tortoise/result"
)

var errMissingHareOutput = errors.New("missing hare output")

// Mesh is the logic layer above our mesh.DB database.
type Mesh struct {
	logger log.Log
	cdb    *datastore.CachedDB
	clock  layerClock

	executor *Executor
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
func NewMesh(cdb *datastore.CachedDB, c layerClock, trtl system.Tortoise, exec *Executor, state conservativeState, logger log.Log) (*Mesh, error) {
	msh := &Mesh{
		logger:              logger,
		cdb:                 cdb,
		clock:               c,
		trtl:                trtl,
		executor:            exec,
		conState:            state,
		nextProcessedLayers: make(map[types.LayerID]struct{}),
	}
	msh.latestLayer.Store(types.LayerID(0))
	msh.latestLayerInState.Store(types.LayerID(0))
	msh.processedLayer.Store(types.LayerID(0))

	lid, err := ballots.LatestLayer(cdb)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("get latest layer %w", err)
	}

	if err == nil && lid != 0 {
		msh.recoverFromDB(lid)
		return msh, nil
	}

	gLid := types.GetEffectiveGenesis()
	if err = cdb.WithTx(context.Background(), func(dbtx *sql.Tx) error {
		for i := types.LayerID(1); !i.After(gLid); i = i.Add(1) {
			if err = layers.SetProcessed(dbtx, i); err != nil {
				return fmt.Errorf("mesh init: %w", err)
			}
			if err = layers.SetApplied(dbtx, i, types.EmptyBlockID); err != nil {
				return fmt.Errorf("mesh init: %w", err)
			}
		}
		return persistLayerHashes(dbtx, gLid, nil)
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

	if applied.After(types.GetEffectiveGenesis()) {
		if err = msh.executor.Revert(context.Background(), applied); err != nil {
			msh.logger.With().Fatal("failed to load state for layer", msh.LatestLayerInState(), log.Err(err))
		}
	}

	msh.logger.With().Info("recovered mesh from disk",
		log.Stringer("latest", msh.LatestLayer()),
		log.Stringer("processed", msh.ProcessedLayer()))
}

// LatestLayerInState returns the latest layer we applied to state.
func (msh *Mesh) LatestLayerInState() types.LayerID {
	return msh.latestLayerInState.Load().(types.LayerID)
}

// LatestLayer - returns the latest layer we saw from the network.
func (msh *Mesh) LatestLayer() types.LayerID {
	return msh.latestLayer.Load().(types.LayerID)
}

// MeshHash returns the aggregated mesh hash at the specified layer.
func (msh *Mesh) MeshHash(lid types.LayerID) (types.Hash32, error) {
	return layers.GetAggregatedHash(msh.cdb, lid)
}

// MissingLayer is a layer in (latestLayerInState, processLayer].
// this layer is missing critical data (valid blocks or transactions)
// and can't be applied to the state.
//
// First valid layer starts with 1. 0 is empty layer and can be ignored.
func (msh *Mesh) MissingLayer() types.LayerID {
	value := msh.missingLayer.Load()
	if value == nil {
		return 0
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
	return types.NewExistingLayer(lid, blts, blks), nil
}

// ProcessedLayer returns the last processed layer ID.
func (msh *Mesh) ProcessedLayer() types.LayerID {
	return msh.processedLayer.Load().(types.LayerID)
}

func (msh *Mesh) setProcessedLayer(logger log.Log, layerID types.LayerID) error {
	processed := msh.ProcessedLayer()
	if !layerID.After(processed) {
		logger.With().Info("trying to set processed layer to an older layer",
			log.Uint32("processed", processed.Uint32()),
			layerID)
		return nil
	}

	if layerID.After(msh.maxProcessedLayer) {
		msh.maxProcessedLayer = layerID
	}

	if layerID != processed.Add(1) {
		logger.With().Info("trying to set processed layer out of order",
			log.Uint32("processed", processed.Uint32()),
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

func (msh *Mesh) processValidityUpdates(ctx context.Context, logger log.Log, updated map[types.LayerID]map[types.BlockID]bool) error {
	if len(updated) == 0 && msh.validityUpdates == nil {
		return nil
	}
	if msh.validityUpdates == nil {
		msh.validityUpdates = updated
	} else {
		for lid, bvs := range updated {
			if _, ok := msh.validityUpdates[lid]; !ok {
				msh.validityUpdates[lid] = map[types.BlockID]bool{}
			}
			for bid, valid := range bvs {
				msh.validityUpdates[lid][bid] = valid
			}
		}
	}
	from, to := updatesRange(msh.validityUpdates)
	results, err := msh.trtl.Results(from, to)
	if err != nil {
		return err
	}
	if missing := missingBlocks(results); len(missing) > 0 {
		return &types.ErrorMissing{MissingData: types.MissingData{Blocks: missing}}
	}
	logger.With().Info("processing validity update",
		log.Uint32("from", from.Uint32()),
		log.Uint32("to", to.Uint32()),
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

	var (
		changed types.LayerID
		inState = msh.LatestLayerInState()
	)
	for _, layer := range results {
		if layer.Layer.After(inState) {
			continue
		}
		bid := layer.FirstValid()
		if bid.IsEmpty() {
			bid = layer.FirstHare()
		}
		applied, err := layers.GetApplied(msh.cdb, layer.Layer)
		if err != nil {
			return fmt.Errorf("get applied %v: %w", layer.Layer, err)
		}
		if bid == applied {
			continue
		}
		logger.With().Debug("incorrect block applied",
			log.Uint32("layer_id", layer.Layer.Uint32()),
			log.Stringer("expected", bid),
			log.Stringer("applied", applied),
		)
		changed = minNonZero(changed, layer.Layer)
	}

	if changed != 0 {
		revertTo := changed.Sub(1)
		logger.Info("reverting state",
			log.Uint32("revert_to", revertTo.Uint32()),
		)
		if err := msh.revertState(ctx, revertTo); err != nil {
			return fmt.Errorf("revert state to %v: %w", revertTo, err)
		}
		msh.setLatestLayerInState(revertTo)
	}

	// only persist block validity *after* the state has been checked.
	// if validity is persisted and the node restarts before the state is checked for revert,
	// upon restarts, tortoise will use persisted validity, and there is no way for mesh
	// to determine whether a state revert should be done.
	if err := msh.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		for i := range results {
			for _, block := range results[i].Blocks {
				if !block.Data {
					continue
				}
				if err := blocks.UpdateValid(dbtx, block.Header.ID, block.Valid); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("save block validity: %w", err)
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

	updated := msh.trtl.Updates()
	if err := msh.processValidityUpdates(ctx, logger, updated); err != nil {
		return err
	}

	// mesh can't skip layer that failed to complete
	from := msh.LatestLayerInState().Add(1)
	to := layerID
	if from == msh.MissingLayer() {
		to = msh.maxProcessedLayer
	}

	if !to.Before(from) {
		if err := msh.pushLayersToState(ctx, logger, from, to); err != nil {
			return err
		}
	}
	logger.Debug("finished processing layer")
	return nil
}

func missingBlocks(results []result.Layer) []types.BlockID {
	var response []types.BlockID
	for _, layer := range results {
		for _, block := range layer.Blocks {
			if (block.Valid || block.Hare) && !block.Data {
				response = append(response, block.Header.ID)
			}
		}
	}
	return response
}

func persistLayerHashes(dbtx *sql.Tx, lid types.LayerID, valids []*types.Block) error {
	sortBlocks(valids)
	var (
		hasher = opinionhash.New()
		err    error
	)
	if lid.After(types.GetEffectiveGenesis()) {
		prev, err := layers.GetAggregatedHash(dbtx, lid.Sub(1))
		if err != nil {
			return fmt.Errorf("get previous aggregated hash %v: %w", lid, err)
		}
		hasher.WritePrevious(prev)
	}
	for _, block := range valids {
		hasher.WriteSupport(block.ID(), block.TickHeight)
	}
	if err = layers.SetMeshHash(dbtx, lid, hasher.Hash()); err != nil {
		return fmt.Errorf("persist hashes %v: %w", lid, err)
	}
	return nil
}

// apply the state of a range of layers, including re-adding transactions from invalid blocks to the mempool.
func (msh *Mesh) pushLayersToState(ctx context.Context, logger log.Log, from, to types.LayerID) error {
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
		if err := msh.pushLayer(ctx, logger, layerID); err != nil {
			msh.missingLayer.Store(layerID)
			return err
		}
		if layerID == missing {
			msh.missingLayer.Store(types.LayerID(0))
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

func layerValidBlocks(logger log.Log, cdb *datastore.CachedDB, layerID types.LayerID, updates map[types.BlockID]bool) ([]*types.Block, error) {
	lyrBlocks, err := blocks.Layer(cdb, layerID)
	if err != nil {
		return nil, fmt.Errorf("get db layer blocks %s: %w", layerID, err)
	}

	if len(lyrBlocks) == 0 {
		logger.Info("layer has no blocks")
		return nil, nil
	}

	var valids, invalids []*types.Block
	for _, b := range lyrBlocks {
		if v, ok := updates[b.ID()]; ok {
			if v {
				valids = append(valids, b)
			} else {
				invalids = append(invalids, b)
			}
		} else if v, err = blocks.IsValid(cdb, b.ID()); err == nil {
			if v {
				valids = append(valids, b)
			} else {
				invalids = append(invalids, b)
			}
		} else if !errors.Is(err, blocks.ErrValidityNotDecided) {
			return nil, fmt.Errorf("get block validity: %w", err)
		}
	}

	if len(valids) > 0 {
		return valids, nil
	}

	if len(invalids) > 0 {
		logger.Info("layer blocks are all invalid")
		return nil, nil
	}

	// tortoise has not verified this layer yet, simply apply the block that hare certified
	bid, err := certificates.GetHareOutput(cdb, layerID)
	if err != nil {
		return nil, fmt.Errorf("%w: get hare output %v", errMissingHareOutput, err.Error())
	}
	// hare output an empty layer, or the network have multiple valid certificates
	if bid == types.EmptyBlockID {
		return nil, nil
	}
	for _, b := range lyrBlocks {
		if b.ID() == bid {
			logger.With().Info("hare output as valid block", b.ID())
			valids = append(valids, b)
			break
		}
	}
	return valids, nil
}

func (msh *Mesh) pushLayer(ctx context.Context, logger log.Log, lid types.LayerID) error {
	if latest := msh.LatestLayerInState(); lid != latest.Add(1) {
		logger.With().Fatal("update state out-of-order", log.Stringer("latest", latest))
	}

	valids, err := layerValidBlocks(logger, msh.cdb, lid, map[types.BlockID]bool{})
	if err != nil {
		return err
	}
	if err = msh.applyState(ctx, logger, lid, valids); err != nil {
		return err
	}
	msh.setLatestLayerInState(lid)
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

	if err := msh.executor.Execute(ctx, lid, block); err != nil {
		return fmt.Errorf("execute block %v/%v: %w", lid, applied, err)
	}
	logger.With().Info("block executed", log.Stringer("applied", applied))
	return msh.persistState(ctx, logger, lid, applied, valids)
}

func (msh *Mesh) persistState(ctx context.Context, logger log.Log, lid types.LayerID, applied types.BlockID, valids []*types.Block) error {
	if err := msh.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		if err := layers.SetApplied(dbtx, lid, applied); err != nil {
			return fmt.Errorf("set applied for %v/%v: %w", lid, applied, err)
		}
		if err := persistLayerHashes(dbtx, lid, valids); err != nil {
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
	logger.With().Info("state persisted", log.Stringer("applied", applied))
	return nil
}

// revertState reverts the conservative state / vm and updates mesh's internal state.
// ideally everything happens here should be atomic.
// see https://github.com/spacemeshos/go-spacemesh/issues/3333
func (msh *Mesh) revertState(ctx context.Context, revertTo types.LayerID) error {
	if err := msh.executor.Revert(ctx, revertTo); err != nil {
		return fmt.Errorf("revert state to layer %v: %w", revertTo, err)
	}
	if err := layers.UnsetAppliedFrom(msh.cdb, revertTo.Add(1)); err != nil {
		return fmt.Errorf("unset applied layer %v: %w", revertTo.Add(1), err)
	}
	return nil
}

func (msh *Mesh) saveHareOutput(ctx context.Context, logger log.Log, lid types.LayerID, bid types.BlockID) error {
	logger.Info("saving hare output for layer")
	var (
		certs []certificates.CertValidity
		err   error
	)
	if err = msh.cdb.WithTx(ctx, func(tx *sql.Tx) error {
		// check if a certificate has been generated or sync'ed.
		// - node generated the certificate when it collected enough certify messages
		// - hare outputs are processed in layer order. i.e. when hare fails for a previous layer N,
		//   a later layer N+x has to wait for syncer to fetch block certificate in layer N for the mesh
		//   to make progress. therefore, by the time layer N+x is processed, syncer may already have
		//   sync'ed the certificate for this layer
		certs, err = certificates.Get(tx, lid)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return fmt.Errorf("mesh get cert %v: %w", lid, err)
		}
		saved := false
		for _, c := range certs {
			if c.Block == bid {
				saved = true
				break
			} else if c.Valid {
				err = certificates.SetHareOutputInvalid(tx, lid, bid)
				if err != nil {
					return fmt.Errorf("save invalid hare output %v/%v: %w", lid, bid, err)
				}
				saved = true
				break
			}
		}
		if !saved {
			err = certificates.SetHareOutput(tx, lid, bid)
			if err != nil {
				return fmt.Errorf("save valid hare output %v/%v: %w", lid, bid, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	switch len(certs) {
	case 0:
		msh.trtl.OnHareOutput(lid, bid)
	case 1:
		logger.With().Info("already synced certificate",
			log.Stringer("cert_block_id", certs[0].Block),
			log.Bool("cert_valid", certs[0].Valid))
	default: // more than 1 cert
		logger.With().Warning("multiple certs found in network",
			log.Object("certificates", log.ObjectMarshallerFunc(func(encoder zapcore.ObjectEncoder) error {
				for _, cert := range certs {
					encoder.AddString("block_id", cert.Block.String())
					encoder.AddBool("valid", cert.Valid)
				}
				return nil
			})))
	}
	return nil
}

// ProcessLayerPerHareOutput receives hare output once it finishes running for a given layer.
func (msh *Mesh) ProcessLayerPerHareOutput(ctx context.Context, layerID types.LayerID, blockID types.BlockID, executed bool) error {
	logger := msh.logger.WithContext(ctx).WithFields(layerID, blockID)
	var (
		block *types.Block
		err   error
	)
	if blockID == types.EmptyBlockID {
		logger.Info("received empty set from hare")
	} else {
		// double-check we have this block in the mesh
		block, err = blocks.Get(msh.cdb, blockID)
		if err != nil {
			return fmt.Errorf("failed to lookup hare output %v: %w", blockID, err)
		}
	}
	// report that hare "approved" this layer
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: layerID,
		Status:  events.LayerStatusTypeApproved,
	})

	if err = msh.saveHareOutput(ctx, logger, layerID, blockID); err != nil {
		return err
	}

	if executed {
		if err = msh.persistState(ctx, logger, layerID, block.ID(), []*types.Block{block}); err != nil {
			return err
		}
		msh.setLatestLayerInState(layerID)
	}
	return msh.ProcessLayer(ctx, layerID)
}

func (msh *Mesh) setLatestLayerInState(lyr types.LayerID) {
	msh.latestLayerInState.Store(lyr)
}

// SetZeroBlockLayer advances the latest layer in the network with a layer
// that has no data.
func (msh *Mesh) SetZeroBlockLayer(ctx context.Context, lid types.LayerID) {
	msh.setLatestLayer(msh.logger.WithContext(ctx), lid)
}

// AddTXsFromProposal adds the TXs in a Proposal into the database.
func (msh *Mesh) AddTXsFromProposal(ctx context.Context, layerID types.LayerID, proposalID types.ProposalID, txIDs []types.TransactionID) error {
	logger := msh.logger.WithContext(ctx).WithFields(layerID, proposalID, log.Int("num_txs", len(txIDs)))
	if err := msh.conState.LinkTXsWithProposal(layerID, proposalID, txIDs); err != nil {
		return fmt.Errorf("link proposal txs: %v/%v: %w", layerID, proposalID, err)
	}
	msh.setLatestLayer(logger, layerID)
	logger.Debug("associated txs to proposal")
	return nil
}

// AddBallot to the mesh.
func (msh *Mesh) AddBallot(ctx context.Context, ballot *types.Ballot) (*types.MalfeasanceProof, error) {
	malicious, err := msh.cdb.IsMalicious(ballot.SmesherID)
	if err != nil {
		return nil, err
	}
	if malicious {
		ballot.SetMalicious()
	}
	var proof *types.MalfeasanceProof
	// ballots.LayerBallotByNodeID and ballots.Add should be atomic
	// otherwise concurrent ballots.Add from the same smesher may not be noticed
	if err = msh.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		if !malicious {
			prev, err := ballots.LayerBallotByNodeID(dbtx, ballot.Layer, ballot.SmesherID)
			if err != nil && !errors.Is(err, sql.ErrNotFound) {
				return err
			}
			if prev != nil && prev.ID() != ballot.ID() {
				var ballotProof types.BallotProof
				for i, b := range []*types.Ballot{prev, ballot} {
					ballotProof.Messages[i] = types.BallotProofMsg{
						InnerMsg: types.BallotMetadata{
							Layer:   b.Layer,
							MsgHash: types.BytesToHash(b.HashInnerBytes()),
						},
						Signature: b.Signature,
						SmesherID: b.SmesherID,
					}
				}
				proof = &types.MalfeasanceProof{
					Layer: ballot.Layer,
					Proof: types.Proof{
						Type: types.MultipleBallots,
						Data: &ballotProof,
					},
				}
				if err = msh.cdb.AddMalfeasanceProof(ballot.SmesherID, proof, dbtx); err != nil {
					return err
				}
				ballot.SetMalicious()
				msh.logger.With().Warning("smesher produced more than one ballot in the same layer",
					log.Stringer("smesher", ballot.SmesherID),
					log.Object("prev", prev),
					log.Object("curr", ballot),
				)
			}
		}
		if err = ballots.Add(dbtx, ballot); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return proof, nil
}

// AddBlockWithTXs adds the block and its TXs in into the database.
func (msh *Mesh) AddBlockWithTXs(ctx context.Context, block *types.Block) error {
	logger := msh.logger.WithContext(ctx).WithFields(block.LayerIndex, block.ID(), log.Int("num_txs", len(block.TxIDs)))
	if err := msh.conState.LinkTXsWithBlock(block.LayerIndex, block.ID(), block.TxIDs); err != nil {
		return fmt.Errorf("link block txs: %v/%v: %w", block.LayerIndex, block.ID(), err)
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

func (msh *Mesh) EpochAtxs(epoch types.EpochID) ([]types.ATXID, error) {
	return atxs.GetIDsByEpoch(msh.cdb, epoch)
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

func updatesRange(updates map[types.LayerID]map[types.BlockID]bool) (from types.LayerID, to types.LayerID) {
	for lid := range updates {
		from = minNonZero(from, lid)
		to = maxNonZero(to, lid)
	}
	return from, to
}

func minNonZero(i, j types.LayerID) types.LayerID {
	if i == 0 {
		return j
	} else if j == 0 {
		return i
	} else if i < j {
		return i
	}
	return j
}

func maxNonZero(i, j types.LayerID) types.LayerID {
	if i == 0 {
		return j
	} else if j == 0 {
		return i
	} else if i > j {
		return i
	}
	return j
}
