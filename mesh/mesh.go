// Package mesh defines the main store point for all the persisted mesh objects
// such as ATXs, ballots and blocks.
package mesh

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/atomic"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/spacemeshos/go-spacemesh/system"
)

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
	processedLayer      atomic.Value
	nextProcessedLayers map[types.LayerID]struct{}
	maxProcessedLayer   types.LayerID

	pendingUpdates struct {
		min, max types.LayerID
	}
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

	genesis := types.GetEffectiveGenesis()
	if err = cdb.WithTx(context.Background(), func(dbtx *sql.Tx) error {
		if err = layers.SetProcessed(dbtx, genesis); err != nil {
			return fmt.Errorf("mesh init: %w", err)
		}
		if err = layers.SetApplied(dbtx, genesis, types.EmptyBlockID); err != nil {
			return fmt.Errorf("mesh init: %w", err)
		}
		if err := layers.SetMeshHash(dbtx, genesis, hash.Sum(nil)); err != nil {
			return err
		}
		return nil
	}); err != nil {
		msh.logger.With().Panic("error initialize genesis data", log.Err(err))
	}

	msh.setLatestLayer(msh.logger, genesis)
	msh.processedLayer.Store(genesis)
	msh.setLatestLayerInState(genesis)
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
			logger.With().Debug("set latest known layer", lid)
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
	return types.NewExistingLayer(lid, blts, blks), nil
}

// ProcessedLayer returns the last processed layer ID.
func (msh *Mesh) ProcessedLayer() types.LayerID {
	return msh.processedLayer.Load().(types.LayerID)
}

func (msh *Mesh) setProcessedLayer(layerID types.LayerID) error {
	processed := msh.ProcessedLayer()
	if !layerID.After(processed) {
		msh.logger.With().Debug("trying to set processed layer to an older layer",
			log.Uint32("processed", processed.Uint32()),
			layerID)
		return nil
	}

	if layerID.After(msh.maxProcessedLayer) {
		msh.maxProcessedLayer = layerID
	}

	if layerID != processed.Add(1) {
		msh.logger.With().Debug("trying to set processed layer out of order",
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
	msh.logger.Event().Debug("processed layer set", processed)
	return nil
}

// ensureStateConsistent finds first layer where applied doesn't match
// the block in consensus, and reverts state before that layer
// if such layer is found.
func (msh *Mesh) ensureStateConsistent(ctx context.Context, results []result.Layer) error {
	var changed types.LayerID
	for _, layer := range results {
		applied, err := layers.GetApplied(msh.cdb, layer.Layer)
		if err != nil && errors.Is(err, sql.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("get applied %v: %w", layer.Layer, err)
		}

		if bid := layer.FirstValid(); bid != applied {
			msh.logger.With().Debug("incorrect block applied",
				log.Context(ctx),
				log.Uint32("layer_id", layer.Layer.Uint32()),
				log.Stringer("expected", bid),
				log.Stringer("applied", applied),
			)
			changed = types.MinLayer(changed, layer.Layer)
		}
	}
	if changed == 0 {
		return nil
	}
	revert := changed.Sub(1)
	msh.logger.With().Info("reverting state",
		log.Context(ctx),
		log.Uint32("revert_to", revert.Uint32()),
	)
	if err := msh.executor.Revert(ctx, revert); err != nil {
		return fmt.Errorf("revert state to layer %v: %w", revert, err)
	}
	if err := layers.UnsetAppliedFrom(msh.cdb, revert.Add(1)); err != nil {
		return fmt.Errorf("unset applied layer %v: %w", revert.Add(1), err)
	}
	msh.setLatestLayerInState(revert)
	return nil
}

// ProcessLayer reads latest consensus results and ensures that vm state
// is consistent with results.
// It is safe to call after optimistically executing the block.
func (msh *Mesh) ProcessLayer(ctx context.Context, lid types.LayerID) error {
	msh.mu.Lock()
	defer msh.mu.Unlock()

	msh.logger.With().Debug("processing layer",
		log.Context(ctx),
		log.Uint32("layer_id", lid.Uint32()),
	)

	msh.trtl.TallyVotes(ctx, lid)

	if err := msh.setProcessedLayer(
		lid); err != nil {
		return err
	}
	results := msh.trtl.Updates()
	pending := msh.pendingUpdates.min != 0
	if len(results) > 0 {
		msh.pendingUpdates.min = types.MinLayer(msh.pendingUpdates.min, results[0].Layer)
		msh.pendingUpdates.max = types.MaxLayer(msh.pendingUpdates.max, results[len(results)-1].Layer)
	}
	if next := msh.LatestLayerInState() + 1; next < msh.pendingUpdates.min {
		msh.pendingUpdates.min = next
		pending = true
	}
	if pending {
		var err error
		results, err = msh.trtl.Results(msh.pendingUpdates.min, msh.pendingUpdates.max)
		if err != nil {
			return err
		}
	}
	// TODO(dshulyak) https://github.com/spacemeshos/go-spacemesh/issues/4425
	if len(results) > 0 {
		msh.logger.With().Info("consensus results",
			log.Context(ctx),
			log.Uint32("layer_id", lid.Uint32()),
			log.Array("results", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
				for i := range results {
					encoder.AppendObject(&results[i])
				}
				return nil
			})),
		)
	}
	if missing := missingBlocks(results); len(missing) > 0 {
		return &types.ErrorMissing{MissingData: types.MissingData{Blocks: missing}}
	}
	if err := msh.ensureStateConsistent(ctx, results); err != nil {
		return err
	}
	if err := msh.applyResults(ctx, results); err != nil {
		return err
	}
	msh.pendingUpdates.min = 0
	msh.pendingUpdates.max = 0
	return nil
}

func missingBlocks(results []result.Layer) []types.BlockID {
	var response []types.BlockID
	for _, layer := range results {
		for _, block := range layer.Blocks {
			if (block.Valid || block.Hare || block.Local) && !block.Data {
				response = append(response, block.Header.ID)
			}
		}
	}
	return response
}

func (msh *Mesh) applyResults(ctx context.Context, results []result.Layer) error {
	msh.logger.With().Debug("applying results", log.Context(ctx))
	for _, layer := range results {
		target := layer.FirstValid()
		if !layer.Verified && target.IsEmpty() {
			return nil
		}
		current, err := layers.GetApplied(msh.cdb, layer.Layer)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return fmt.Errorf("get applied %v: %w", layer.Layer, err)
		}
		if current != target || err != nil {
			var block *types.Block
			if !target.IsEmpty() {
				var err error
				block, err = blocks.Get(msh.cdb, target)
				if err != nil {
					return fmt.Errorf("get block: %w", err)
				}
			}
			if err := msh.executor.Execute(ctx, layer.Layer, block); err != nil {
				return fmt.Errorf("execute block %v/%v: %w", layer.Layer, target, err)
			}
		} else {
			msh.logger.With().Debug("correct block already applied",
				log.Context(ctx),
				log.Uint32("layer", layer.Layer.Uint32()),
				log.Stringer("block", current),
			)
		}
		if err := msh.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
			if err := layers.SetApplied(dbtx, layer.Layer, target); err != nil {
				return fmt.Errorf("set applied for %v/%v: %w", layer.Layer, target, err)
			}
			if err := layers.SetMeshHash(dbtx, layer.Layer, layer.Opinion); err != nil {
				return fmt.Errorf("set mesh hash for %v/%v: %w", layer.Layer, layer.Opinion, err)
			}
			for _, block := range layer.Blocks {
				if block.Data && block.Valid {
					if err := blocks.SetValid(dbtx, block.Header.ID); err != nil {
						return err
					}
				} else if block.Data && block.Invalid {
					if err := blocks.SetInvalid(dbtx, block.Header.ID); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if layer.Verified {
			events.ReportLayerUpdate(events.LayerUpdate{
				LayerID: layer.Layer,
				Status:  events.LayerStatusTypeApplied,
			})
		}

		msh.logger.With().Debug("state persisted",
			log.Context(ctx),
			log.Stringer("applied", target),
		)
		if layer.Layer > msh.LatestLayerInState() {
			msh.setLatestLayerInState(layer.Layer)
		}
	}
	return nil
}

func (msh *Mesh) saveHareOutput(ctx context.Context, lid types.LayerID, bid types.BlockID) error {
	msh.logger.With().Debug("saving hare output for layer",
		log.Context(ctx),
		log.Uint32("layer_id", lid.Uint32()),
		log.Stringer("block_id", bid),
	)
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
		msh.logger.With().Info("already synced certificate",
			log.Context(ctx),
			log.Stringer("cert_block_id", certs[0].Block),
			log.Bool("cert_valid", certs[0].Valid))
	default: // more than 1 cert
		msh.logger.With().Warning("multiple certs found in network",
			log.Context(ctx),
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
	if blockID == types.EmptyBlockID {
		msh.logger.With().Info("received empty set from hare",
			log.Context(ctx),
			log.Uint32("layer_id", layerID.Uint32()),
			log.Stringer("block_id", blockID),
		)
	}
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: layerID,
		Status:  events.LayerStatusTypeApproved,
	})
	if err := msh.saveHareOutput(ctx, layerID, blockID); err != nil {
		return err
	}
	if executed {
		if err := layers.SetApplied(msh.cdb, layerID, blockID); err != nil {
			return fmt.Errorf("optimistically applied for %v/%v: %w", layerID, blockID, err)
		}
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
	msh.trtl.OnBlock(block.ToVote())
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

// LastVerified returns the latest layer verified by tortoise.
func (msh *Mesh) LastVerified() types.LayerID {
	return msh.trtl.LatestComplete()
}
