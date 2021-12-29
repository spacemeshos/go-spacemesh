// Package mesh defines the main store point for all the block-mesh objects
// such as proposals, transactions and global state
package mesh

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"sync"

	"github.com/seehuhn/mt19937"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh/metrics"
	"github.com/spacemeshos/go-spacemesh/system"
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
	ProcessLayer(context.Context, *types.Layer)
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
	fetch  system.BlockFetcher
	trtl   tortoise
	txPool txMemPool
	config Config
	// latestLayer is the latest layer this node had seen from blocks
	latestLayer types.LayerID
	// latestLayerInState is the latest layer whose contents have been applied to the state
	latestLayerInState types.LayerID
	// processedLayer is the latest layer whose votes have been processed
	processedLayer      types.LayerID
	nextProcessedLayers map[types.LayerID]struct{}
	maxProcessedLayer   types.LayerID
	mutex               sync.RWMutex
	done                chan struct{}
	txMutex             sync.Mutex
}

// NewMesh creates a new instant of a mesh.
func NewMesh(db *DB, atxDb AtxDB, rewardConfig Config, fetcher system.BlockFetcher, trtl tortoise, txPool txMemPool, state state, logger log.Log) *Mesh {
	msh := &Mesh{
		Log:                 logger,
		fetch:               fetcher,
		trtl:                trtl,
		txPool:              txPool,
		state:               state,
		done:                make(chan struct{}),
		DB:                  db,
		config:              rewardConfig,
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
		lyr, err := msh.GetLayer(i)
		if err != nil {
			msh.With().Panic("failed to get layer", i, log.Err(err))
		}
		if err := msh.persistLayerHashes(context.Background(), i, i); err != nil {
			msh.With().Panic("failed to persist hashes for layer", i, log.Err(err))
		}
		msh.setProcessedLayer(lyr)
	}
	return msh
}

// NewRecoveredMesh creates new instance of mesh with recovered mesh data fom database.
func NewRecoveredMesh(ctx context.Context, db *DB, atxDb AtxDB, rewardConfig Config, fetcher system.BlockFetcher, trtl tortoise, txPool txMemPool, state state, logger log.Log) *Mesh {
	msh := NewMesh(db, atxDb, rewardConfig, fetcher, trtl, txPool, state, logger)

	latest, err := db.general.Get(constLATEST)
	if err != nil {
		logger.With().Panic("failed to recover latest layer", log.Err(err))
	}
	msh.setLatestLayer(types.BytesToLayerID(latest))

	lyr, err := msh.GetProcessedLayer()
	if err != nil {
		logger.With().Panic("failed to recover processed layer", log.Err(err))
	}
	msh.setProcessedLayerFromRecoveredData(lyr)

	verified, err := db.general.Get(VERIFIED)
	if err != nil {
		logger.With().Panic("failed to recover latest verified layer", log.Err(err))
	}
	if err = msh.setLatestLayerInState(types.BytesToLayerID(verified)); err != nil {
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
	defer msh.mutex.RUnlock()
	msh.mutex.RLock()
	return msh.latestLayerInState
}

// LatestLayer - returns the latest layer we saw from the network.
func (msh *Mesh) LatestLayer() types.LayerID {
	defer msh.mutex.RUnlock()
	msh.mutex.RLock()
	return msh.latestLayer
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
		if err := msh.general.Put(constLATEST, idx.Bytes()); err != nil {
			msh.Error("could not persist latest layer index")
		}
	}
}

// GetLayer returns Layer i from the database.
func (msh *Mesh) GetLayer(i types.LayerID) (*types.Layer, error) {
	mBlocks, err := msh.LayerBlocks(i)
	if err != nil {
		return nil, fmt.Errorf("layer blocks: %w", err)
	}

	l := types.NewLayer(i)
	l.SetBlocks(mBlocks)

	return l, nil
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

func (msh *Mesh) setProcessedLayer(layer *types.Layer) {
	msh.mutex.Lock()
	defer msh.mutex.Unlock()
	if !layer.Index().After(msh.processedLayer) {
		msh.With().Info("trying to set processed layer to an older layer",
			log.FieldNamed("processed", msh.processedLayer),
			layer.Index())
		return
	}

	if layer.Index().After(msh.maxProcessedLayer) {
		msh.maxProcessedLayer = layer.Index()
	}

	if layer.Index() != msh.processedLayer.Add(1) {
		msh.With().Info("trying to set processed layer out of order",
			log.FieldNamed("processed", msh.processedLayer),
			layer.Index())
		msh.nextProcessedLayers[layer.Index()] = struct{}{}
		return
	}

	msh.nextProcessedLayers[layer.Index()] = struct{}{}
	lastProcessed := msh.processedLayer
	for i := layer.Index(); !i.After(msh.maxProcessedLayer); i = i.Add(1) {
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
func (vl *validator) ProcessLayer(ctx context.Context, lyr *types.Layer) {
	layerID := lyr.Index()
	logger := vl.WithContext(ctx).WithFields(layerID)
	logger.Info("validate layer")

	// pass the layer to tortoise for processing
	oldVerified, newVerified, reverted := vl.trtl.HandleIncomingLayer(ctx, layerID)
	logger.With().Info("tortoise results",
		log.Bool("reverted", reverted),
		log.FieldNamed("old_verified", oldVerified),
		log.FieldNamed("new_verified", newVerified))

	// check for a state reversion: if tortoise reran and detected changes to historical data, it will request that
	// state be reverted and reapplied. pushLayersToState, below, will handle the reapplication.
	if reverted {
		if err := vl.revertState(ctx, oldVerified); err != nil {
			logger.With().Error("failed to revert state, unable to validate layer", log.Err(err))
			return
		}
	}

	if newVerified.After(oldVerified) {
		vl.pushLayersToState(ctx, oldVerified, newVerified)
		if err := vl.persistLayerHashes(ctx, oldVerified.Add(1), newVerified); err != nil {
			logger.With().Error("failed to persist layer hashes", log.Err(err))
		}
		for lid := oldVerified.Add(1); !lid.After(newVerified); lid = lid.Add(1) {
			events.ReportLayerUpdate(events.LayerUpdate{
				LayerID: lid,
				Status:  events.LayerStatusTypeConfirmed,
			})
		}
	}

	vl.setProcessedLayer(lyr)
	logger.Info("done validating layer")
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

		prevHash, err := msh.getAggregatedLayerHash(i.Sub(1))
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
	lyr, err := msh.GetLayer(layerID)
	if err != nil {
		logger.With().Error("failed to get layer", layerID, log.Err(err))
		return nil, err
	}
	var validBlockIDs []types.BlockID
	for _, bID := range lyr.BlocksIDs() {
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
func (msh *Mesh) pushLayersToState(ctx context.Context, oldVerified, newVerified types.LayerID) {
	logger := msh.WithContext(ctx).WithFields(
		log.FieldNamed("old_verified", oldVerified),
		log.FieldNamed("new_verified", newVerified))
	logger.Info("pushing layers to state")
	if oldVerified.Before(types.GetEffectiveGenesis()) || newVerified.Before(types.GetEffectiveGenesis()) {
		logger.Panic("tried to push genesis layers")
	}

	// we never reapply the state of oldVerified. note that state reversions must be handled separately.
	for layerID := oldVerified.Add(1); !layerID.After(newVerified); layerID = layerID.Add(1) {
		l, err := msh.GetLayer(layerID)
		// TODO: propagate/handle error
		if err != nil || l == nil {
			logger.With().Error("failed to get layer", layerID, log.Err(err))
			return
		}
		validBlocks, invalidBlocks := msh.BlocksByValidity(l.Blocks())
		msh.updateStateWithLayer(ctx, types.NewExistingLayer(layerID, validBlocks))
		msh.Event().Info("end of layer state root",
			layerID,
			log.String("state_root", util.Bytes2Hex(msh.state.GetStateRoot().Bytes())),
		)
		msh.reInsertTxsToPool(validBlocks, invalidBlocks, l.Index())
	}
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

func (msh *Mesh) reInsertTxsToPool(validBlocks, invalidBlocks []*types.Block, l types.LayerID) {
	seenTxIds := make(map[types.TransactionID]struct{})
	uniqueTxIds(validBlocks, seenTxIds) // run for the side effect, updating seenTxIds
	returnedTxs, missing := msh.GetTransactions(uniqueTxIds(invalidBlocks, seenTxIds))
	if len(missing) > 0 {
		msh.Panic("could not find transactions %v from layer %v", missing, l)
	}

	grouped, accounts := msh.removeFromUnappliedTxs(returnedTxs)
	for account := range accounts {
		msh.removeRejectedFromAccountTxs(account, grouped)
	}
	for _, tx := range returnedTxs {
		err := msh.ValidateNonceAndBalance(tx)
		if err == nil {
			err = msh.AddTxToPool(tx)
		}

		if err == nil {
			// We ignore errors here, since they mean that the tx is no longer
			// valid and we shouldn't re-add it.
			msh.With().Info("transaction from contextually invalid block re-added to mempool", tx.ID())
		}
	}
}

func (msh *Mesh) applyState(l *types.Layer) {
	// Aggregate all blocks' rewards.
	validBlockTxs := extractUniqueOrderedTransactions(msh.Log, l, msh.DB)
	// The reason we are serializing the types.NodeID to a string instead of using it directly as a
	// key in the map is due to Golang's restriction on only Comparable types used as map keys. Since
	// the types.NodeID contains a slice, it is not comparable and hence cannot be used as a map key.
	//
	// TODO: fix this when changing the types.NodeID struct, see
	// https://github.com/spacemeshos/go-spacemesh/issues/2269.
	coinbasesAndSmeshers, coinbases := getCoinbasesAndSmeshers(msh.Log, l, msh.AtxDB)
	var failedTxs []*types.Transaction
	var svmErr error

	// TODO: should miner IDs be sorted in a deterministic order prior to applying rewards?
	if len(coinbases) > 0 {
		rewards := calculateRewards(l, validBlockTxs, msh.config, coinbases)
		rewardByMiner := map[types.Address]uint64{}
		for _, miner := range coinbases {
			rewardByMiner[miner] += rewards.blockTotalReward
		}
		failedTxs, svmErr = msh.state.ApplyLayer(l.Index(), validBlockTxs, rewardByMiner)
		msh.logRewards(&rewards)
		reportRewards(msh.Log, &rewards, coinbasesAndSmeshers)

		if err := msh.DB.writeTransactionRewards(l.Index(), coinbasesAndSmeshers, rewards.blockTotalReward, rewards.blockLayerReward); err != nil {
			msh.Log.Error("cannot write reward to db")
		}
	} else {
		msh.With().Info("no valid blocks for layer", l.Index())
		failedTxs, svmErr = msh.state.ApplyLayer(l.Index(), validBlockTxs, map[types.Address]uint64{})
	}

	if svmErr != nil {
		msh.With().Error("failed to apply transactions",
			l.Index(), log.Int("num_failed_txs", len(failedTxs)), log.Err(svmErr))
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldVerified`
	}
	msh.removeFromUnappliedTxs(validBlockTxs)
	msh.With().Info("applied transactions",
		log.Int("valid_block_txs", len(validBlockTxs)),
		l.Index(),
		log.Int("num_failed_txs", len(failedTxs)),
	)

	msh.setLatestLayerInState(l.Index())
}

// ProcessLayerPerHareOutput receives hare output once it finishes running for a given layer.
func (msh *Mesh) ProcessLayerPerHareOutput(ctx context.Context, validatedLayer types.LayerID, blockIDs []types.BlockID) {
	logger := msh.WithContext(ctx).WithFields(validatedLayer)

	// when ProcessLayerPerHareOutput is called by hare, we may not have all the blocks in the hare output.
	// so try harder to fetch the blocks from peers.
	if len(blockIDs) > 0 && !validatedLayer.GetEpoch().IsGenesis() {
		if err := msh.fetch.GetBlocks(ctx, blockIDs); err != nil {
			logger.With().Warning("not all blocks are available locally", validatedLayer, log.Err(err))
		}
	}

	var blocks []*types.Block
	for _, blockID := range blockIDs {
		block, err := msh.GetBlock(blockID)
		if err != nil {
			// stop processing this hare result, wait until tortoise pushes this layer into state
			logger.With().Error("hare terminated with block that is not present in mesh",
				blockID,
				log.Err(err))
			return
		}
		blocks = append(blocks, block)
	}

	metrics.LayerNumBlocks.WithLabelValues().Observe(float64(len(blockIDs)))

	// report that hare "approved" this layer
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: validatedLayer,
		Status:  events.LayerStatusTypeApproved,
	})

	logger.With().Info("saving hare output for layer", log.Int("num_hare_output", len(blocks)))
	if err := msh.SaveHareConsensusOutput(ctx, validatedLayer, types.BlockIDs(blocks)); err != nil {
		logger.With().Error("saving hare output failed", log.Err(err))
	}
	lyr := types.NewExistingLayer(validatedLayer, blocks)
	msh.ProcessLayer(ctx, lyr)
}

// apply the state for a single layer. stores layer results for early layers, and applies previously stored results for
// now-older layers.
func (msh *Mesh) updateStateWithLayer(ctx context.Context, layer *types.Layer) {
	msh.txMutex.Lock()
	defer msh.txMutex.Unlock()
	latest := msh.LatestLayerInState()
	if layer.Index() != latest.Add(1) {
		msh.WithContext(ctx).With().Panic("update state out-of-order",
			log.FieldNamed("validated", layer.Index()),
			log.FieldNamed("latest", latest))
	}
	msh.applyState(layer)
}

func (msh *Mesh) setLatestLayerInState(lyr types.LayerID) error {
	// Update validated layer only after applying transactions since loading of
	// state depends on processedLayer param.
	msh.mutex.Lock()
	defer msh.mutex.Unlock()
	if err := msh.general.Put(VERIFIED, lyr.Bytes()); err != nil {
		// can happen if database already closed
		msh.Error("could not persist validated layer index %d: %v", lyr, err.Error())
		return fmt.Errorf("put into DB: %w", err)
	}
	msh.latestLayerInState = lyr
	return nil
}

// GetAggregatedLayerHash returns the aggregated layer hash up to the specified layer.
func (msh *Mesh) GetAggregatedLayerHash(layerID types.LayerID) types.Hash32 {
	h, err := msh.getAggregatedLayerHash(layerID)
	if err != nil {
		return types.EmptyLayerHash
	}
	return h
}

func (msh *Mesh) getAggregatedLayerHash(layerID types.LayerID) (types.Hash32, error) {
	if layerID.Before(types.NewLayerID(1)) {
		return types.EmptyLayerHash, nil
	}
	var hash types.Hash32
	bts, err := msh.general.Get(getAggregatedLayerHashKey(layerID))
	if err == nil {
		hash.SetBytes(bts)
		return hash, nil
	}
	return hash, fmt.Errorf("get from DB: %w", err)
}

func extractUniqueOrderedTransactions(logger log.Log, l *types.Layer, mdb *DB) (validBlockTxs []*types.Transaction) {
	validBlocks := l.Blocks()

	// Deterministically sort valid blocks
	types.SortBlocks(validBlocks)

	// Initialize a Mersenne Twister seeded with layerHash
	blockHash := types.CalcBlockHash32Presorted(types.BlockIDs(validBlocks), nil)
	mt := mt19937.New()
	mt.SeedFromSlice(toUint64Slice(blockHash.Bytes()))
	rng := rand.New(mt)

	// Perform a Fisher-Yates shuffle on the blocks
	rng.Shuffle(len(validBlocks), func(i, j int) {
		validBlocks[i], validBlocks[j] = validBlocks[j], validBlocks[i]
	})

	// Get and return unique transactions
	seenTxIds := make(map[types.TransactionID]struct{})
	txs, missing := mdb.GetTransactions(uniqueTxIds(validBlocks, seenTxIds))
	if len(missing) > 0 {
		logger.Panic("could not find transactions %v from layer %v", missing, l)
	}
	return txs
}

func toUint64Slice(b []byte) []uint64 {
	l := len(b)
	var s []uint64
	for i := 0; i < l; i += 8 {
		s = append(s, binary.LittleEndian.Uint64(b[i:util.Min(l, i+8)]))
	}
	return s
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

var errLayerHasBlock = errors.New("layer has block")

// SetZeroBlockLayer tags lyr as a layer without blocks.
func (msh *Mesh) SetZeroBlockLayer(lyr types.LayerID) error {
	msh.With().Info("tagging zero block layer", lyr)
	// check database for layer
	if l, err := msh.GetLayer(lyr); err != nil {
		// database error
		if !errors.Is(err, database.ErrNotFound) {
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

	// layer doesnt exist, need to insert new layer
	return msh.AddZeroBlockLayer(lyr)
}

// AddTXsFromProposal adds the TXs in a Proposal into the database.
func (msh *Mesh) AddTXsFromProposal(context.Context, *types.Proposal) error {
	// TODO impement me
	return nil
}

// AddProposalWithTxs adds a proposal to the database
// p - the proposal to add
// txs - proposal txs that we don't have in our tx database yet.
func (msh *Mesh) AddProposalWithTxs(ctx context.Context, p *types.Proposal) error {
	logger := msh.WithContext(ctx).WithFields(p.ID())
	logger.With().Debug("adding proposal to mesh", p.Fields()...)

	if !p.LayerIndex.After(msh.ProcessedLayer()) {
		logger.With().Warning("proposal is late",
			log.FieldNamed("processed_layer", msh.ProcessedLayer()),
			log.FieldNamed("miner_id", p.SmesherID()))
	}

	if err := msh.storeTransactionsFromPool(p); err != nil {
		logger.With().Error("not all txs were processed", log.Err(err))
	}

	// Store proposal (delete if storing ATXs fails)
	if err := msh.DB.AddProposal(p); err != nil {
		logger.With().Error("failed to add proposal", log.Err(err))
		return err
	}

	msh.setLatestLayer(p.LayerIndex)
	events.ReportNewProposal(p)
	logger.Info("added proposal to database")
	return nil
}

func (msh *Mesh) invalidateFromPools(txIDs []types.TransactionID) {
	for _, id := range txIDs {
		msh.txPool.Invalidate(id)
	}
}

// storeTransactionsFromPool takes declared txs from provided proposal and writes them to DB. it then invalidates
// the transactions from txpool.
func (msh *Mesh) storeTransactionsFromPool(p *types.Proposal) error {
	// Store transactions (doesn't have to be rolled back if other writes fail)
	if len(p.TxIDs) == 0 {
		return nil
	}
	txs := make([]*types.Transaction, 0, len(p.TxIDs))
	for _, txID := range p.TxIDs {
		tx, err := msh.txPool.Get(txID)
		if err != nil {
			// if the transaction is not in the pool it could have been
			// invalidated by another block
			if has, err := msh.transactions.Has(txID.Bytes()); !has {
				return fmt.Errorf("check if tx is in DB: %w", err)
			}
			continue
		}
		txs = append(txs, tx)
	}
	if err := msh.writeTransactions(p, txs...); err != nil {
		return fmt.Errorf("could not write transactions of block %v database: %v", p.ID(), err)
	}

	if err := msh.addToUnappliedTxs(txs, p.LayerIndex); err != nil {
		return fmt.Errorf("failed to add to unappliedTxs: %v", err)
	}

	// remove txs from pool
	msh.invalidateFromPools(p.TxIDs)

	return nil
}

type layerRewardsInfo struct {
	types.LayerID
	feesReward          uint64
	layerReward         uint64
	numBlocks           uint64
	blockTotalReward    uint64
	blockTotalRewardMod uint64
	blockLayerReward    uint64
	blockLayerRewardMod uint64
}

func (info *layerRewardsInfo) totalReward() uint64 {
	return info.feesReward + info.layerReward
}

func (msh *Mesh) logRewards(rewards *layerRewardsInfo) {
	msh.With().Info("reward calculated",
		rewards.LayerID,
		log.Uint64("num_blocks", rewards.numBlocks),
		log.Uint64("total_reward", rewards.totalReward()),
		log.Uint64("layer_reward", rewards.layerReward),
		log.Uint64("block_total_reward", rewards.blockTotalReward),
		log.Uint64("block_layer_reward", rewards.blockLayerReward),
		log.Uint64("total_reward_remainder", rewards.blockTotalRewardMod),
		log.Uint64("layer_reward_remainder", rewards.blockLayerRewardMod),
	)
}

func calculateRewards(l *types.Layer, txs []*types.Transaction, params Config, coinbases []types.Address) layerRewardsInfo {
	rewards := layerRewardsInfo{}

	rewards.LayerID = l.Index()
	for _, tx := range txs {
		rewards.feesReward += tx.GetFee()
	}

	rewards.layerReward = calculateLayerReward(l.Index(), params)
	rewards.numBlocks = uint64(len(coinbases))

	rewards.blockTotalReward, rewards.blockTotalRewardMod = calculateActualRewards(l.Index(), rewards.totalReward(), rewards.numBlocks)
	rewards.blockLayerReward, rewards.blockLayerRewardMod = calculateActualRewards(l.Index(), rewards.layerReward, rewards.numBlocks)

	return rewards
}

func reportRewards(logger log.Log, rewards *layerRewardsInfo, coinbasesAndSmeshers map[types.Address]map[string]uint64) {
	// Report the rewards for each coinbase and each smesherID within each coinbase.
	// This can be thought of as a partition of the reward amongst all the smesherIDs
	// that added the coinbase into the block.
	for account, smesherAccountEntry := range coinbasesAndSmeshers {
		for smesherString, cnt := range smesherAccountEntry {
			smesherEntry, err := types.StringToNodeID(smesherString)
			if err != nil {
				logger.With().Error("unable to convert bytes to nodeid", log.Err(err),
					log.String("smesher_string", smesherString))
				return
			}
			events.ReportRewardReceived(events.Reward{
				Layer:       rewards.LayerID,
				Total:       cnt * rewards.blockTotalReward,
				LayerReward: cnt * rewards.blockLayerReward,
				Coinbase:    account,
				Smesher:     *smesherEntry,
			})
		}
	}
}

func getCoinbasesAndSmeshers(logger log.Log, l *types.Layer, atxDB AtxDB) (coinbasesAndSmeshers map[types.Address]map[string]uint64, coinbases []types.Address) {
	coinbases = make([]types.Address, 0, len(l.Blocks()))
	// the reason we are serializing the types.NodeID to a string instead of using it directly as a
	// key in the map is due to Golang's restriction on only Comparable types used as map keys. Since
	// the types.NodeID contains a slice, it is not comparable and hence cannot be used as a map key
	// TODO: fix this when changing the types.NodeID struct, see https://github.com/spacemeshos/go-spacemesh/issues/2269
	coinbasesAndSmeshers = make(map[types.Address]map[string]uint64)
	for _, bl := range l.Blocks() {
		if bl.AtxID == *types.EmptyATXID {
			logger.With().Info("skipping reward distribution for block with no atx", bl.LayerIndex, bl.ID())
			continue
		}
		atx, err := atxDB.GetAtxHeader(bl.AtxID)
		if err != nil {
			logger.With().Warning("atx from block not found in db", log.Err(err), bl.ID(), bl.AtxID)
			continue
		}
		coinbases = append(coinbases, atx.Coinbase)
		// create a 2 dimensional map where the entries are
		// coinbasesAndSmeshers[coinbase_id][smesher_id] = number of blocks this pair has created
		if _, exists := coinbasesAndSmeshers[atx.Coinbase]; !exists {
			coinbasesAndSmeshers[atx.Coinbase] = make(map[string]uint64)
		}
		coinbasesAndSmeshers[atx.Coinbase][atx.NodeID.String()]++
	}

	return coinbasesAndSmeshers, coinbases
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
