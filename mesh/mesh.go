// Package mesh defines the main store point for all the block-mesh objects
// such as blocks, transactions and global state
package mesh

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"sync"

	"github.com/seehuhn/mt19937"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh/metrics"
)

const (
	layerSize = 200
)

var (
	constTrue      = []byte{1}
	constFalse     = []byte{0}
	constLATEST    = []byte("latest")
	constPROCESSED = []byte("processed")
)

// VERIFIED refers to layers we pushed into the state
var VERIFIED = []byte("verified")

type tortoise interface {
	HandleIncomingLayer(context.Context, types.LayerID) (oldPbase, newPbase types.LayerID, reverted bool)
	LatestComplete() types.LayerID
	Persist(context.Context) error
	HandleLateBlocks(context.Context, []*types.Block) (types.LayerID, types.LayerID)
}

// Validator interface to be used in tests to mock validation flow
type Validator interface {
	ValidateLayer(context.Context, *types.Layer)
}

type txProcessor interface {
	ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error)
	ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int)
	AddressExists(addr types.Address) bool
	ValidateNonceAndBalance(transaction *types.Transaction) error
	GetLayerApplied(txID types.TransactionID) *types.LayerID
	GetLayerStateRoot(layer types.LayerID) (types.Hash32, error)
	GetStateRoot() types.Hash32
	LoadState(layer types.LayerID) error
	ValidateAndAddTxToPool(tx *types.Transaction) error
	GetBalance(addr types.Address) uint64
	GetNonce(addr types.Address) uint64
	GetAllAccounts() (*types.MultipleAccountsState, error)
}

type txMemPool interface {
	Invalidate(id types.TransactionID)
	Get(id types.TransactionID) (*types.Transaction, error)
	Put(id types.TransactionID, tx *types.Transaction)
}

// AtxDB holds logic for working with atxs
type AtxDB interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
	GetFullAtx(id types.ATXID) (*types.ActivationTx, error)
	SyntacticallyValidateAtx(ctx context.Context, atx *types.ActivationTx) error
}

// Mesh is the logic layer above our mesh.DB database
type Mesh struct {
	log.Log
	*DB
	AtxDB
	txProcessor
	Validator
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
	nextValidLayers     map[types.LayerID]*types.Layer
	maxValidatedLayer   types.LayerID
	txMutex             sync.Mutex
}

// NewMesh creates a new instant of a mesh
func NewMesh(db *DB, atxDb AtxDB, rewardConfig Config, trtl tortoise, txPool txMemPool, pr txProcessor, logger log.Log) *Mesh {
	msh := &Mesh{
		Log:                 logger,
		trtl:                trtl,
		txPool:              txPool,
		txProcessor:         pr,
		done:                make(chan struct{}),
		DB:                  db,
		config:              rewardConfig,
		AtxDB:               atxDb,
		nextProcessedLayers: make(map[types.LayerID]struct{}),
		nextValidLayers:     make(map[types.LayerID]*types.Layer),
		latestLayer:         types.GetEffectiveGenesis(),
		latestLayerInState:  types.GetEffectiveGenesis(),
	}

	msh.Validator = &validator{Mesh: msh}
	gLyr := types.GetEffectiveGenesis()
	for i := types.NewLayerID(1); i.Before(gLyr); i = i.Add(1) {
		if err := msh.SetZeroBlockLayer(i); err != nil {
			msh.With().Panic("failed to set zero-block for genesis layer", i, log.Err(err))
		}
	}
	if err := msh.persistLayerHashes(context.Background(), types.NewLayerID(1), gLyr); err != nil {
		msh.With().Panic("failed to persist hashes for genesis layers", log.Err(err))
	}
	return msh
}

// NewRecoveredMesh creates new instance of mesh with recovered mesh data fom database
func NewRecoveredMesh(ctx context.Context, db *DB, atxDb AtxDB, rewardConfig Config, trtl tortoise, txPool txMemPool, pr txProcessor, logger log.Log) *Mesh {
	msh := NewMesh(db, atxDb, rewardConfig, trtl, txPool, pr, logger)

	latest, err := db.general.Get(constLATEST)
	if err != nil {
		logger.With().Panic("failed to recover latest layer", log.Err(err))
	}
	msh.setLatestLayer(types.BytesToLayerID(latest))

	lyr, err := msh.recoverProcessedLayer()
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

	err = pr.LoadState(msh.LatestLayerInState())
	if err != nil {
		logger.With().Panic("failed to load state for layer", msh.LatestLayerInState(), log.Err(err))
	}
	// in case we load a state that was not fully played
	nextLayer := msh.LatestLayerInState().Add(1)
	lastComplete := msh.trtl.LatestComplete()
	if nextLayer.Before(msh.trtl.LatestComplete()) {
		// todo: add test for this case, or add random kill test on node
		logger.With().Info("playing layers to state",
			log.FieldNamed("from_layer", nextLayer),
			log.FieldNamed("to_layer", lastComplete))
		msh.pushLayersToState(ctx, nextLayer, lastComplete)
	}

	msh.With().Info("recovered mesh from disk",
		log.FieldNamed("latest", msh.LatestLayer()),
		log.FieldNamed("processed", msh.ProcessedLayer()),
		log.FieldNamed("verified", msh.trtl.LatestComplete()),
		log.String("root_hash", pr.GetStateRoot().String()))

	return msh
}

// CacheWarmUp warms up cache with latest blocks
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

// LatestLayerInState returns the latest layer we applied to state
func (msh *Mesh) LatestLayerInState() types.LayerID {
	defer msh.mutex.RUnlock()
	msh.mutex.RLock()
	return msh.latestLayerInState
}

// LatestLayer - returns the latest layer we saw from the network
func (msh *Mesh) LatestLayer() types.LayerID {
	defer msh.mutex.RUnlock()
	msh.mutex.RLock()
	return msh.latestLayer
}

// setLatestLayer sets the latest layer we saw from the network
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

// GetLayer returns Layer i from the database
func (msh *Mesh) GetLayer(i types.LayerID) (*types.Layer, error) {
	mBlocks, err := msh.LayerBlocks(i)
	if err != nil {
		return nil, err
	}

	l := types.NewLayer(i)
	l.SetBlocks(mBlocks)

	return l, nil
}

// GetLayerHash returns layer hash for received blocks
func (msh *Mesh) GetLayerHash(layerID types.LayerID) types.Hash32 {
	h, err := msh.recoverLayerHash(layerID)
	if err == nil {
		return h
	}
	if err == database.ErrNotFound {
		// layer hash not persisted. i.e. contextual validity not yet determined
		lyr, err := msh.GetLayer(layerID)
		if err == nil {
			return lyr.Hash()
		}
	}
	return types.EmptyLayerHash
}

// ProcessedLayer returns the last processed layer ID
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

// ValidateLayer performs fairly heavy lifting: it triggers tortoise to process the full contents of the layer (i.e.,
// all of its blocks), then to attempt to validate all unvalidated layers up to this layer. It also applies state for
// newly-validated layers.
// TODO: rename this. When tortoise passes a layer, we call that "verify." "Validate" sounds too similar and it's
//   confusing that a layer can be validated without being verified.
//   See https://github.com/spacemeshos/go-spacemesh/issues/2669
func (vl *validator) ValidateLayer(ctx context.Context, lyr *types.Layer) {
	layerID := lyr.Index()
	logger := vl.WithContext(ctx).WithFields(layerID)
	logger.Info("validate layer")

	// pass the layer to tortoise for processing
	oldPbase, newPbase, reverted := vl.trtl.HandleIncomingLayer(ctx, layerID)
	logger.With().Info("tortoise results",
		log.Bool("reverted", reverted),
		log.FieldNamed("old_pbase", oldPbase),
		log.FieldNamed("new_pbase", newPbase))

	// check for a state reversion: if tortoise reran and detected changes to historical data, it will request that
	// state be reverted and reapplied. pushLayersToState, below, will handle the reapplication.
	if reverted {
		if err := vl.revertState(ctx, oldPbase); err != nil {
			logger.With().Error("failed to revert state, unable to validate layer", log.Err(err))
			return
		}
	}

	if err := vl.trtl.Persist(ctx); err != nil {
		logger.With().Error("could not persist tortoise", log.Err(err))
	}
	vl.pushLayersToState(ctx, oldPbase, newPbase)
	if newPbase.After(oldPbase) {
		if err := vl.persistLayerHashes(ctx, oldPbase.Add(1), newPbase); err != nil {
			logger.With().Error("failed to persist layer hashes", log.Err(err))
		}
	}
	vl.setProcessedLayer(lyr)
	for newlyVerifiedLayer := oldPbase.Add(1); !newlyVerifiedLayer.After(newPbase); newlyVerifiedLayer = newlyVerifiedLayer.Add(1) {
		events.ReportLayerUpdate(events.LayerUpdate{
			LayerID: newlyVerifiedLayer,
			Status:  events.LayerStatusTypeConfirmed,
		})
	}
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

// HandleLateBlock process a late (contextually invalid) block.
func (msh *Mesh) HandleLateBlock(ctx context.Context, b *types.Block) {
	msh.WithContext(ctx).With().Info("validate late block", b.ID())
	// TODO: handle late blocks in batches, see https://github.com/spacemeshos/go-spacemesh/issues/2412
	oldPbase, newPbase := msh.trtl.HandleLateBlocks(ctx, []*types.Block{b})
	if err := msh.trtl.Persist(ctx); err != nil {
		msh.WithContext(ctx).With().Error("could not persist tortoise on late block", b.ID(), b.Layer())
	}
	msh.pushLayersToState(ctx, oldPbase, newPbase)
}

// apply the state of a range of layers, including re-adding transactions from invalid blocks to the mempool
func (msh *Mesh) pushLayersToState(ctx context.Context, oldPbase, newPbase types.LayerID) {
	logger := msh.WithContext(ctx).WithFields(
		log.FieldNamed("old_pbase", oldPbase),
		log.FieldNamed("new_pbase", newPbase))
	logger.Info("pushing layers to state")

	// TODO: does this need to be hardcoded? can we use types.GetEffectiveGenesis instead?
	//   see https://github.com/spacemeshos/go-spacemesh/issues/2670
	layerTwo := types.NewLayerID(2)
	if oldPbase.Before(layerTwo) {
		msh.With().Warning("tried to push layer < 2",
			log.FieldNamed("old_pbase", oldPbase),
			log.FieldNamed("new_pbase", newPbase))
		if newPbase.Before(types.NewLayerID(3)) {
			return
		}
		oldPbase = layerTwo.Sub(1) // since we add one, below
	}

	// we never reapply the state of oldPbase. note that state reversions must be handled separately.
	for layerID := oldPbase.Add(1); !layerID.After(newPbase); layerID = layerID.Add(1) {
		l, err := msh.GetLayer(layerID)
		// TODO: propagate/handle error
		if err != nil || l == nil {
			if layerID.GetEpoch().IsGenesis() {
				logger.With().Info("failed to get layer (expected for genesis layers)", layerID, log.Err(err))
			} else {
				logger.With().Error("failed to get layer", layerID, log.Err(err))
			}
			return
		}
		validBlocks, invalidBlocks := msh.BlocksByValidity(l.Blocks())
		msh.updateStateWithLayer(ctx, types.NewExistingLayer(layerID, validBlocks))
		msh.Event().Info("end of layer state root",
			layerID,
			log.String("state_root", util.Bytes2Hex(msh.txProcessor.GetStateRoot().Bytes())),
		)
		msh.reInsertTxsToPool(validBlocks, invalidBlocks, l.Index())
	}
}

// RevertState reverts to state as of a previous layer
func (msh *Mesh) revertState(ctx context.Context, layerID types.LayerID) error {
	logger := msh.WithContext(ctx).WithFields(layerID)
	logger.Info("attempting to roll back state to previous layer")
	if err := msh.LoadState(layerID); err != nil {
		return fmt.Errorf("failed to revert state to layer %v: %w", layerID, err)
	}
	return nil
}

func (msh *Mesh) reInsertTxsToPool(validBlocks, invalidBlocks []*types.Block, l types.LayerID) {
	seenTxIds := make(map[types.TransactionID]struct{})
	uniqueTxIds(validBlocks, seenTxIds) // run for the side effect, updating seenTxIds
	returnedTxs := msh.getTxs(uniqueTxIds(invalidBlocks, seenTxIds), l)
	grouped, accounts := msh.removeFromUnappliedTxs(returnedTxs)
	for account := range accounts {
		msh.removeRejectedFromAccountTxs(account, grouped, l)
	}
	for _, tx := range returnedTxs {
		if err := msh.ValidateAndAddTxToPool(tx); err == nil {
			// We ignore errors here, since they mean that the tx is no longer valid and we shouldn't re-add it
			msh.With().Info("transaction from contextually invalid block re-added to mempool", tx.ID())
		}
	}
}

func (msh *Mesh) applyState(l *types.Layer) {
	msh.accumulateRewards(l, msh.config)
	msh.pushTransactions(l)
	msh.setLatestLayerInState(l.Index())
}

// HandleValidatedLayer receives hare output once it finishes running for a given layer
func (msh *Mesh) HandleValidatedLayer(ctx context.Context, validatedLayer types.LayerID, layer []types.BlockID) {
	logger := msh.WithContext(ctx).WithFields(validatedLayer)
	var blocks []*types.Block

	for _, blockID := range layer {
		block, err := msh.GetBlock(blockID)
		if err != nil {
			// stop processing this hare result, wait until tortoise pushes this layer into state
			logger.Error("hare terminated with block that is not present in mesh")
			return
		}
		blocks = append(blocks, block)
	}

	metrics.LayerNumBlocks.Observe(float64(len(layer)))

	// report that hare "approved" this layer
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: validatedLayer,
		Status:  events.LayerStatusTypeApproved,
	})

	logger.With().Info("saving input vector for layer", log.Int("count_valid_blocks", len(blocks)))

	if err := msh.SaveLayerInputVectorByID(ctx, validatedLayer, types.BlockIDs(blocks)); err != nil {
		logger.Error("saving layer input vector failed")
	}
	lyr := types.NewExistingLayer(validatedLayer, blocks)
	msh.ValidateLayer(ctx, lyr)
}

func (msh *Mesh) getInvalidBlocksByHare(ctx context.Context, hareLayer *types.Layer) (invalid []*types.Block) {
	dbLayer, err := msh.GetLayer(hareLayer.Index())
	if err != nil {
		msh.WithContext(ctx).With().Error("failed to get layer", log.Err(err))
		return
	}
	exists := make(map[types.BlockID]struct{})
	for _, block := range hareLayer.Blocks() {
		exists[block.ID()] = struct{}{}
	}

	for _, block := range dbLayer.Blocks() {
		if _, has := exists[block.ID()]; !has {
			invalid = append(invalid, block)
		}
	}
	return
}

// apply the state for a single layer. stores layer results for early layers, and applies previously stored results for
// now-older layers.
func (msh *Mesh) updateStateWithLayer(ctx context.Context, layer *types.Layer) {
	msh.txMutex.Lock()
	defer msh.txMutex.Unlock()
	latest := msh.LatestLayerInState()
	if !layer.Index().After(latest) {
		msh.WithContext(ctx).With().Warning("result received after state has advanced, late block received?",
			log.FieldNamed("validated", layer.Index()),
			log.FieldNamed("latest", latest))
		return
	}
	if msh.maxValidatedLayer.Before(layer.Index()) {
		msh.maxValidatedLayer = layer.Index()
	}
	if layer.Index().After(latest.Add(1)) {
		msh.WithContext(ctx).With().Warning("early layer result received",
			log.FieldNamed("validated", layer.Index()),
			log.FieldNamed("max_validated", msh.maxValidatedLayer),
			log.FieldNamed("latest", latest))
		msh.nextValidLayers[layer.Index()] = layer
		return
	}
	msh.applyState(layer)
	for i := layer.Index().Add(1); !i.After(msh.maxValidatedLayer); i = i.Add(1) {
		nxtLayer, has := msh.nextValidLayers[i]
		if !has {
			break
		}
		msh.applyState(nxtLayer)
		delete(msh.nextValidLayers, i)
	}
}

func (msh *Mesh) setLatestLayerInState(lyr types.LayerID) error {
	// update validated layer only after applying transactions since loading of state depends on processedLayer param.
	msh.mutex.Lock()
	defer msh.mutex.Unlock()
	if err := msh.general.Put(VERIFIED, lyr.Bytes()); err != nil {
		// can happen if database already closed
		msh.Error("could not persist validated layer index %d: %v", lyr, err.Error())
		return err
	}
	msh.latestLayerInState = lyr
	return nil
}

// GetAggregatedLayerHash returns the aggregated layer hash up to the specified layer
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
	return hash, err
}

func (msh *Mesh) extractUniqueOrderedTransactions(l *types.Layer) (validBlockTxs []*types.Transaction) {
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
	return msh.getTxs(uniqueTxIds(validBlocks, seenTxIds), l.Index())
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

func (msh *Mesh) getTxs(txIds []types.TransactionID, l types.LayerID) []*types.Transaction {
	txs, missing := msh.GetTransactions(txIds)
	if len(missing) != 0 {
		msh.Panic("could not find transactions %v from layer %v", missing, l)
	}
	return txs
}

func (msh *Mesh) pushTransactions(l *types.Layer) {
	validBlockTxs := msh.extractUniqueOrderedTransactions(l)
	numFailedTxs, err := msh.ApplyTransactions(l.Index(), validBlockTxs)
	if err != nil {
		msh.With().Error("failed to apply transactions",
			l.Index(), log.Int("num_failed_txs", numFailedTxs), log.Err(err))
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldPbase`
	}
	msh.removeFromUnappliedTxs(validBlockTxs)
	msh.With().Info("applied transactions",
		log.Int("valid_block_txs", len(validBlockTxs)),
		l.Index(),
		log.Int("num_failed_txs", numFailedTxs),
	)
}

var errLayerHasBlock = errors.New("layer has block")

// SetZeroBlockLayer tags lyr as a layer without blocks
func (msh *Mesh) SetZeroBlockLayer(lyr types.LayerID) error {
	msh.With().Info("tagging zero block layer", lyr)
	// check database for layer
	if l, err := msh.GetLayer(lyr); err != nil {
		// database error
		if err != database.ErrNotFound {
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

// AddBlockWithTxs adds a block to the database
// blk - the block to add
// txs - block txs that we dont have in our tx database yet
func (msh *Mesh) AddBlockWithTxs(ctx context.Context, blk *types.Block) error {
	logger := msh.WithContext(ctx).WithFields(blk.ID())
	logger.With().Debug("adding block to mesh", blk.Fields()...)

	if err := msh.StoreTransactionsFromPool(blk); err != nil {
		logger.With().Error("not all txs were processed", log.Err(err))
	}

	// Store block (delete if storing ATXs fails)
	if err := msh.DB.AddBlock(blk); err != nil {
		if err == ErrAlreadyExist {
			return nil
		}
		logger.With().Error("failed to add block", log.Err(err))
		return err
	}

	msh.setLatestLayer(blk.Layer())
	// add new block to orphans
	msh.handleOrphanBlocks(blk)
	events.ReportNewBlock(blk)
	logger.Info("added block to database")
	return nil
}

func (msh *Mesh) invalidateFromPools(blk *types.MiniBlock) {
	for _, id := range blk.TxIDs {
		msh.txPool.Invalidate(id)
	}
}

// StoreTransactionsFromPool takes declared txs from provided block blk and writes them to DB. it then invalidates
// the transactions from txpool
func (msh *Mesh) StoreTransactionsFromPool(blk *types.Block) error {
	// Store transactions (doesn't have to be rolled back if other writes fail)
	if len(blk.TxIDs) == 0 {
		return nil
	}
	txs := make([]*types.Transaction, 0, len(blk.TxIDs))
	for _, txID := range blk.TxIDs {
		tx, err := msh.txPool.Get(txID)
		if err != nil {
			// if the transaction is not in the pool it could have been
			// invalidated by another block
			if has, err := msh.transactions.Has(txID.Bytes()); !has {
				return err
			}
			continue
		}
		if err = tx.CalcAndSetOrigin(); err != nil {
			return err
		}
		txs = append(txs, tx)
	}
	if err := msh.writeTransactions(blk, txs...); err != nil {
		return fmt.Errorf("could not write transactions of block %v database: %v", blk.ID(), err)
	}

	if err := msh.addToUnappliedTxs(txs, blk.LayerIndex); err != nil {
		return fmt.Errorf("failed to add to unappliedTxs: %v", err)
	}

	// remove txs from pool
	msh.invalidateFromPools(&blk.MiniBlock)

	return nil
}

// todo better thread safety
func (msh *Mesh) handleOrphanBlocks(blk *types.Block) {
	msh.mutex.Lock()
	defer msh.mutex.Unlock()
	if _, ok := msh.orphanBlocks[blk.Layer()]; !ok {
		msh.orphanBlocks[blk.Layer()] = make(map[types.BlockID]struct{})
	}
	msh.orphanBlocks[blk.Layer()][blk.ID()] = struct{}{}
	msh.With().Debug("added block to orphans", blk.ID())
	for _, b := range append(blk.ForDiff, append(blk.AgainstDiff, blk.NeutralDiff...)...) {
		for layerID, layermap := range msh.orphanBlocks {
			if _, has := layermap[b]; has {
				msh.With().Debug("delete block from orphans", b)
				delete(layermap, b)
				if len(layermap) == 0 {
					delete(msh.orphanBlocks, layerID)
				}
				break
			}
		}
	}
}

// GetOrphanBlocksBefore returns all known orphan blocks with layerID < l
func (msh *Mesh) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	msh.mutex.RLock()
	defer msh.mutex.RUnlock()
	ids := map[types.BlockID]struct{}{}
	for key, val := range msh.orphanBlocks {
		if key.Before(l) {
			for bid := range val {
				ids[bid] = struct{}{}
			}
		}
	}

	blocks, err := msh.LayerBlockIds(l.Sub(1))
	if err != nil {
		return nil, fmt.Errorf("failed getting latest layer %v err %v", l.Sub(1), err)
	}

	// add last layer blocks
	for _, b := range blocks {
		ids[b] = struct{}{}
	}

	idArr := make([]types.BlockID, 0, len(ids))
	for i := range ids {
		idArr = append(idArr, i)
	}

	idArr = types.SortBlockIDs(idArr)

	msh.Info("orphans for layer %d are %v", l, idArr)
	return idArr, nil
}

func (msh *Mesh) accumulateRewards(l *types.Layer, params Config) {
	coinbases := make([]types.Address, 0, len(l.Blocks()))
	// the reason we are serializing the types.NodeID to a string instead of using it directly as a
	// key in the map is due to Golang's restriction on only Comparable types used as map keys. Since
	// the types.NodeID contains a slice, it is not comparable and hence cannot be used as a map key
	// TODO: fix this when changing the types.NodeID struct, see https://github.com/spacemeshos/go-spacemesh/issues/2269
	coinbasesAndSmeshers := make(map[types.Address]map[string]uint64)
	for _, bl := range l.Blocks() {
		if bl.ATXID == *types.EmptyATXID {
			msh.With().Info("skipping reward distribution for block with no atx", bl.LayerIndex, bl.ID())
			continue
		}
		atx, err := msh.AtxDB.GetAtxHeader(bl.ATXID)
		if err != nil {
			msh.With().Warning("atx from block not found in db", log.Err(err), bl.ID(), bl.ATXID)
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

	if len(coinbases) == 0 {
		msh.With().Info("no valid blocks for layer", l.Index())
		return
	}

	// aggregate all blocks' rewards
	txs := msh.extractUniqueOrderedTransactions(l)

	totalReward := &big.Int{}
	for _, tx := range txs {
		totalReward.Add(totalReward, new(big.Int).SetUint64(tx.Fee))
	}

	layerReward := calculateLayerReward(l.Index(), params)
	totalReward.Add(totalReward, layerReward)

	numBlocks := big.NewInt(int64(len(coinbases)))

	blockTotalReward, blockTotalRewardMod := calculateActualRewards(l.Index(), totalReward, numBlocks)

	// NOTE: We don't _report_ rewards when we apply them. This is because applying rewards just requires
	// the recipient (i.e., coinbase) account, whereas reporting requires knowing the associated smesherid
	// as well. We report rewards below once we unpack the data structure containing the association between
	// rewards and smesherids.

	// Applying rewards (here), reporting them, and adding them to the database (below) should be atomic. Right now,
	// they're not. Also, ApplyRewards does not return an error if it fails.
	// TODO: fix this.
	msh.ApplyRewards(l.Index(), coinbases, blockTotalReward)

	blockLayerReward, blockLayerRewardMod := calculateActualRewards(l.Index(), layerReward, numBlocks)
	msh.With().Info("reward calculated",
		l.Index(),
		log.Uint64("num_blocks", numBlocks.Uint64()),
		log.Uint64("total_reward", totalReward.Uint64()),
		log.Uint64("layer_reward", layerReward.Uint64()),
		log.Uint64("block_total_reward", blockTotalReward.Uint64()),
		log.Uint64("block_layer_reward", blockLayerReward.Uint64()),
		log.Uint64("total_reward_remainder", blockTotalRewardMod.Uint64()),
		log.Uint64("layer_reward_remainder", blockLayerRewardMod.Uint64()),
	)

	// Report the rewards for each coinbase and each smesherID within each coinbase.
	// This can be thought of as a partition of the reward amongst all the smesherIDs
	// that added the coinbase into the block.
	for account, smesherAccountEntry := range coinbasesAndSmeshers {
		for smesherString, cnt := range smesherAccountEntry {
			smesherEntry, err := types.StringToNodeID(smesherString)
			if err != nil {
				msh.With().Error("unable to convert bytes to nodeid", log.Err(err),
					log.String("smesher_string", smesherString))
				return
			}
			events.ReportRewardReceived(events.Reward{
				Layer:       l.Index(),
				Total:       cnt * blockTotalReward.Uint64(),
				LayerReward: cnt * blockLayerReward.Uint64(),
				Coinbase:    account,
				Smesher:     *smesherEntry,
			})
		}
	}
	if err := msh.writeTransactionRewards(l.Index(), coinbasesAndSmeshers, blockTotalReward, blockLayerReward); err != nil {
		msh.Error("cannot write reward to db")
	}
	// todo: should miner id be sorted in a deterministic order prior to applying rewards?
}

// GenesisBlock is a is the first static block that xists at the beginning of each network. it exist one layer before actual blocks could be created
func GenesisBlock() *types.Block {
	return types.NewExistingBlock(types.GetEffectiveGenesis(), []byte("genesis"), nil)
}

// GenesisLayer generates layer 0 should be removed after the genesis flow is implemented
func GenesisLayer() *types.Layer {
	l := types.NewLayer(types.GetEffectiveGenesis())
	l.AddBlock(GenesisBlock())
	return l
}

// GetATXs uses GetFullAtx to return a list of atxs corresponding to atxIds requested
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
