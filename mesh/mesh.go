// Package mesh defines the main store point for all the persisted mesh objects
// such as ATXs, ballots and blocks.
package mesh

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Mesh is the logic layer above our mesh.DB database.
type Mesh struct {
	logger   *zap.Logger
	cdb      *sql.Database
	atxsdata *atxsdata.Data
	clock    layerClock

	executor *Executor
	conState conservativeState
	trtl     system.Tortoise

	missingBlocks chan []types.BlockID

	mu sync.Mutex
	// latestLayer is the latest layer this node had seen from blocks
	latestLayer atomic.Value
	// latestLayerInState is the latest layer whose contents have been applied to the state
	latestLayerInState atomic.Value
	// processedLayer is the latest layer whose votes have been processed
	processedLayer      atomic.Value
	nextProcessedLayers map[types.LayerID]struct{}
	maxProcessedLayer   types.LayerID
}

// NewMesh creates a new instant of a mesh.
func NewMesh(
	db *sql.Database,
	atxsdata *atxsdata.Data,
	c layerClock,
	trtl system.Tortoise,
	exec *Executor,
	state conservativeState,
	logger *zap.Logger,
) (*Mesh, error) {
	msh := &Mesh{
		logger:              logger,
		cdb:                 db,
		atxsdata:            atxsdata,
		clock:               c,
		trtl:                trtl,
		executor:            exec,
		conState:            state,
		nextProcessedLayers: make(map[types.LayerID]struct{}),
		missingBlocks:       make(chan []types.BlockID, 32),
	}
	msh.latestLayer.Store(types.LayerID(0))
	msh.latestLayerInState.Store(types.LayerID(0))
	msh.processedLayer.Store(types.LayerID(0))

	lid, err := ballots.LatestLayer(db)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("get latest layer %w", err)
	}

	if err == nil && lid != 0 {
		msh.recoverFromDB(lid)
		return msh, nil
	}

	genesis := types.GetEffectiveGenesis()
	if err = db.WithTx(context.Background(), func(dbtx *sql.Tx) error {
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
		msh.logger.Panic("error initialize genesis data", zap.Error(err))
	}

	msh.setLatestLayer(genesis)
	msh.processedLayer.Store(genesis)
	msh.setLatestLayerInState(genesis)
	return msh, nil
}

func (msh *Mesh) recoverFromDB(latest types.LayerID) {
	msh.setLatestLayer(latest)

	lyr, err := layers.GetProcessed(msh.cdb)
	if err != nil {
		msh.logger.Fatal("failed to recover processed layer", zap.Error(err))
	}
	msh.processedLayer.Store(lyr)

	applied, err := layers.GetLastApplied(msh.cdb)
	if err != nil {
		msh.logger.Fatal("failed to recover latest applied layer", zap.Error(err))
	}
	msh.setLatestLayerInState(applied)

	if applied.After(types.GetEffectiveGenesis()) {
		if err = msh.executor.Revert(context.Background(), applied); err != nil {
			msh.logger.Fatal("failed to load state for layer",
				zap.Error(err),
				zap.Uint32("layer", msh.LatestLayerInState().Uint32()),
			)
		}
	}
	msh.logger.Info("recovered mesh from disk",
		zap.Stringer("latest", msh.LatestLayer()),
		zap.Stringer("processed", msh.ProcessedLayer()))
}

// LatestLayerInState returns the latest layer we applied to state.
func (msh *Mesh) LatestLayerInState() types.LayerID {
	return msh.latestLayerInState.Load().(types.LayerID)
}

// MissingBlocks returns single consumer channel.
// Consumer by contract is responsible for downloading missing blocks.
func (msh *Mesh) MissingBlocks() <-chan []types.BlockID {
	return msh.missingBlocks
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
func (msh *Mesh) setLatestLayer(lid types.LayerID) {
	if err := events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: lid,
		Status:  events.LayerStatusTypeUnknown,
	}); err != nil {
		msh.logger.Error("Failed to emit updated layer", zap.Uint32("lid", lid.Uint32()), zap.Error(err))
	}
	for {
		current := msh.LatestLayer()
		if !lid.After(current) {
			return
		}
		if msh.latestLayer.CompareAndSwap(current, lid) {
			if err := events.ReportNodeStatusUpdate(); err != nil {
				msh.logger.Error("Failed to emit status update", zap.Error(err))
			}
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

// GetLayerVerified returns the verified, canonical block for a layer (or none for an empty layer).
func (msh *Mesh) GetLayerVerified(lid types.LayerID) (*types.Block, error) {
	applied, err := layers.GetApplied(msh.cdb, lid)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get applied %v: %w", lid, err)
	case applied.IsEmpty():
		return nil, nil
	default:
		return blocks.Get(msh.cdb, applied)
	}
}

// ProcessedLayer returns the last processed layer ID.
func (msh *Mesh) ProcessedLayer() types.LayerID {
	return msh.processedLayer.Load().(types.LayerID)
}

func (msh *Mesh) setProcessedLayer(layerID types.LayerID) error {
	processed := msh.ProcessedLayer()
	if !layerID.After(processed) {
		return nil
	}

	if layerID.After(msh.maxProcessedLayer) {
		msh.maxProcessedLayer = layerID
	}

	if layerID != processed.Add(1) {
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
	if err := events.ReportNodeStatusUpdate(); err != nil {
		msh.logger.Error("Failed to emit status update", zap.Error(err))
	}
	return nil
}

// ensureStateConsistent finds first layer where applied doesn't match
// the block in consensus, and reverts state before that layer
// if such layer is found.
func (msh *Mesh) ensureStateConsistent(ctx context.Context, results []result.Layer) error {
	changed := types.LayerID(math.MaxUint32)
	for _, layer := range results {
		applied, err := layers.GetApplied(msh.cdb, layer.Layer)
		if err != nil && errors.Is(err, sql.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("get applied %v: %w", layer.Layer, err)
		}

		if bid := layer.FirstValid(); bid != applied {
			msh.logger.Debug("incorrect block applied",
				log.ZContext(ctx),
				zap.Uint32("layer_id", layer.Layer.Uint32()),
				zap.Stringer("expected", bid),
				zap.Stringer("applied", applied),
			)
			changed = min(changed, layer.Layer)
		}
	}
	if changed == math.MaxUint32 {
		return nil
	}
	revert := changed.Sub(1)
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

	msh.trtl.TallyVotes(ctx, lid)

	if err := msh.setProcessedLayer(lid); err != nil {
		return err
	}
	results := msh.trtl.Updates()
	next := msh.LatestLayerInState() + 1
	// TODO(dshulyak) https://github.com/spacemeshos/go-spacemesh/issues/4425
	if len(results) > 0 {
		msh.logger.Debug("consensus results",
			log.ZContext(ctx),
			zap.Uint32("layer_id", lid.Uint32()),
			zap.Array("results", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
				for i := range results {
					encoder.AppendObject(&results[i])
				}
				return nil
			})),
		)
	}
	applicable, missing := filterMissing(results, next)
	if err := msh.ensureStateConsistent(ctx, applicable); err != nil {
		return err
	}
	if err := msh.applyResults(ctx, applicable); err != nil {
		return err
	}
	// apply what we were able to download, as it will allow to prune some of the data from tortoise
	if len(missing) > 0 {
		return &MissingBlocksError{Blocks: missing}
	}
	return nil
}

func filterMissing(results []result.Layer, next types.LayerID) ([]result.Layer, []types.BlockID) {
	var (
		missing []types.BlockID
		index   = -1
	)
	for i, layer := range results {
		for _, block := range layer.Blocks {
			if (block.Valid || block.Hare || block.Local) && !block.Data {
				missing = append(missing, block.Header.ID)
				if index == -1 {
					index = i
				}
			}
		}
	}
	if index >= 0 {
		firstMissing := results[index].Layer
		if firstMissing <= next {
			return nil, missing
		}
		return results[:index], missing
	}
	return results, nil
}

func (msh *Mesh) applyResults(ctx context.Context, results []result.Layer) error {
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
			// tortoise will evict layer when OnApplied is called.
			// however we also apply layers before contextual validity was determined (e.g block.Valid field)
			// in such case we would apply block because of hare, and then we may evict event when block.Valid was set
			// but before it was saved to database
			msh.trtl.OnApplied(layer.Layer, layer.Opinion)
			if err := events.ReportLayerUpdate(events.LayerUpdate{
				LayerID: layer.Layer,
				Status:  events.LayerStatusTypeApplied,
			}); err != nil {
				msh.logger.Error("Failed to emit updated layer",
					zap.Uint32("lid", layer.Layer.Uint32()),
					zap.Error(err),
				)
			}
		}
		if layer.Layer > msh.LatestLayerInState() {
			msh.setLatestLayerInState(layer.Layer)
		}
	}
	return nil
}

func (msh *Mesh) saveHareOutput(ctx context.Context, lid types.LayerID, bid types.BlockID) error {
	msh.logger.Debug("saving hare output",
		log.ZContext(ctx),
		zap.Uint32("lid", lid.Uint32()),
		zap.Stringer("block", bid),
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
	if len(certs) > 1 {
		msh.logger.Warn("multiple certificates found in network",
			log.ZContext(ctx),
			zap.Object("certificates", zapcore.ObjectMarshalerFunc(func(encoder zapcore.ObjectEncoder) error {
				for _, cert := range certs {
					encoder.AddString("block_id", cert.Block.String())
					encoder.AddBool("valid", cert.Valid)
				}
				return nil
			})),
		)
		return nil
	}
	// otherwise always notify tortoise about hare output
	msh.trtl.OnHareOutput(lid, bid)
	return nil
}

// ProcessLayerPerHareOutput receives hare output once it finishes running for a given layer.
func (msh *Mesh) ProcessLayerPerHareOutput(
	ctx context.Context,
	layerID types.LayerID,
	blockID types.BlockID,
	executed bool,
) error {
	if err := events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: layerID,
		Status:  events.LayerStatusTypeApproved,
	}); err != nil {
		msh.logger.Error("Failed to emit updated layer", zap.Uint32("lid", layerID.Uint32()), zap.Error(err))
	}
	if err := msh.saveHareOutput(ctx, layerID, blockID); err != nil {
		return err
	}
	if executed {
		if err := layers.SetApplied(msh.cdb, layerID, blockID); err != nil {
			return fmt.Errorf("optimistically applied for %v/%v: %w", layerID, blockID, err)
		}
	}
	err := msh.ProcessLayer(ctx, layerID)
	if err != nil {
		missing := &MissingBlocksError{}
		if errors.As(err, &missing) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case msh.missingBlocks <- missing.Blocks:
			}
		}
	}
	return err
}

func (msh *Mesh) setLatestLayerInState(lyr types.LayerID) {
	msh.latestLayerInState.Store(lyr)
}

// SetZeroBlockLayer advances the latest layer in the network with a layer
// that has no data.
func (msh *Mesh) SetZeroBlockLayer(ctx context.Context, lid types.LayerID) {
	msh.setLatestLayer(lid)
}

// AddTXsFromProposal adds the TXs in a Proposal into the database.
func (msh *Mesh) AddTXsFromProposal(
	ctx context.Context,
	layerID types.LayerID,
	proposalID types.ProposalID,
	txIDs []types.TransactionID,
) error {
	if err := msh.conState.LinkTXsWithProposal(layerID, proposalID, txIDs); err != nil {
		return fmt.Errorf("link proposal txs: %v/%v: %w", layerID, proposalID, err)
	}
	msh.setLatestLayer(layerID)
	return nil
}

// AddBallot to the mesh.
func (msh *Mesh) AddBallot(
	ctx context.Context,
	ballot *types.Ballot,
) (*wire.MalfeasanceProof, error) {
	malicious := msh.atxsdata.IsMalicious(ballot.SmesherID)
	if malicious {
		ballot.SetMalicious()
	}
	var proof *wire.MalfeasanceProof
	// ballots.LayerBallotByNodeID and ballots.Add should be atomic
	// otherwise concurrent ballots.Add from the same smesher may not be noticed
	if err := msh.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		if !malicious {
			prev, err := ballots.LayerBallotByNodeID(dbtx, ballot.Layer, ballot.SmesherID)
			if err != nil && !errors.Is(err, sql.ErrNotFound) {
				return err
			}
			if prev != nil && prev.ID() != ballot.ID() {
				var ballotProof wire.BallotProof
				for i, b := range []*types.Ballot{prev, ballot} {
					ballotProof.Messages[i] = wire.BallotProofMsg{
						InnerMsg: types.BallotMetadata{
							Layer:   b.Layer,
							MsgHash: types.BytesToHash(b.HashInnerBytes()),
						},
						Signature: b.Signature,
						SmesherID: b.SmesherID,
					}
				}
				proof = &wire.MalfeasanceProof{
					Layer: ballot.Layer,
					Proof: wire.Proof{
						Type: wire.MultipleBallots,
						Data: &ballotProof,
					},
				}
				encoded, err := codec.Encode(proof)
				if err != nil {
					msh.logger.Panic("failed to encode MalfeasanceProof", zap.Error(err))
				}
				if err := identities.SetMalicious(dbtx, ballot.SmesherID, encoded, time.Now()); err != nil {
					return fmt.Errorf("add malfeasance proof: %w", err)
				}
				ballot.SetMalicious()
				msh.logger.Warn("smesher produced more than one ballot in the same layer",
					zap.Stringer("smesher", ballot.SmesherID),
					zap.Object("prev", prev),
					zap.Object("curr", ballot),
				)
			}
		}
		if err := ballots.Add(dbtx, ballot); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if proof != nil {
		msh.atxsdata.SetMalicious(ballot.SmesherID)
		msh.trtl.OnMalfeasance(ballot.SmesherID)
	}
	return proof, nil
}

// AddBlockWithTXs adds the block and its TXs in into the database.
func (msh *Mesh) AddBlockWithTXs(ctx context.Context, block *types.Block) error {
	if err := msh.conState.LinkTXsWithBlock(block.LayerIndex, block.ID(), block.TxIDs); err != nil {
		return fmt.Errorf("link block txs: %v/%v: %w", block.LayerIndex, block.ID(), err)
	}
	msh.setLatestLayer(block.LayerIndex)

	// add block to the tortoise before storing it
	// otherwise fetcher will not wait until data is stored in the tortoise
	msh.trtl.OnBlock(block.ToVote())
	if err := blocks.Add(msh.cdb, block); err != nil && !errors.Is(err, sql.ErrObjectExists) {
		return err
	}
	return nil
}

// GetRewardsByCoinbase retrieves account's rewards by the coinbase address.
func (msh *Mesh) GetRewardsByCoinbase(coinbase types.Address) ([]*types.Reward, error) {
	return rewards.ListByCoinbase(msh.cdb, coinbase)
}

// GetRewardsBySmesherId retrieves account's rewards by the smesher ID.
func (msh *Mesh) GetRewardsBySmesherId(smesherID types.NodeID) ([]*types.Reward, error) {
	return rewards.ListBySmesherId(msh.cdb, smesherID)
}

// LastVerified returns the latest layer verified by tortoise.
func (msh *Mesh) LastVerified() types.LayerID {
	return msh.trtl.LatestComplete()
}

type MissingBlocksError struct {
	Blocks []types.BlockID
}

func (e *MissingBlocksError) Error() string {
	if len(e.Blocks) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("missing blocks: ")
	for i, b := range e.Blocks {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(b.String())
	}
	return builder.String()
}
