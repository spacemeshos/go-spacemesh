// Package mesh defines the main store point for all the block-mesh objects
// such as blocks, transactions and global state
package mesh

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"

	"github.com/seehuhn/mt19937"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"

	"math/big"

	"sync"
)

const (
	layerSize = 200
)

var constTrue = []byte{1}
var constFalse = []byte{0}
var constLATEST = []byte("latest")
var constLAYERHASH = []byte("layer hash")
var constPROCESSED = []byte("processed")

// TORTOISE key for tortoise persistence in database
var TORTOISE = []byte("tortoise")

// VERIFIED refers to layers we pushed into the state
var VERIFIED = []byte("verified")

type tortoise interface {
	HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID)
	LatestComplete() types.LayerID
	Persist() error
	HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID)
}

// Validator interface to be used in tests to mock validation flow
type Validator interface {
	ValidateLayer(layer *types.Layer)
	HandleLateBlock(bl *types.Block)
	ProcessedLayer() types.LayerID
	SetProcessedLayer(lyr types.LayerID)
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
	ProcessAtxs(atxs []*types.ActivationTx) error
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
	GetFullAtx(id types.ATXID) (*types.ActivationTx, error)
	SyntacticallyValidateAtx(atx *types.ActivationTx) error
}

// Mesh is the logic layer above our mesh.DB database
type Mesh struct {
	log.Log
	*DB
	AtxDB
	txProcessor
	Validator
	trtl               tortoise
	txPool             txMemPool
	config             Config
	latestLayer        types.LayerID
	latestLayerInState types.LayerID
	layerHash          []byte
	lMutex             sync.RWMutex
	lkMutex            sync.RWMutex
	lcMutex            sync.RWMutex
	lvMutex            sync.RWMutex
	orphMutex          sync.RWMutex
	pMutex             sync.RWMutex
	done               chan struct{}
	nextValidLayers    map[types.LayerID]*types.Layer
	maxValidatedLayer  types.LayerID
	txMutex            sync.Mutex
}

// NewMesh creates a new instant of a mesh
func NewMesh(db *DB, atxDb AtxDB, rewardConfig Config, mesh tortoise, txInvalidator txMemPool, pr txProcessor, logger log.Log) *Mesh {
	ll := &Mesh{
		Log:                logger,
		trtl:               mesh,
		txPool:             txInvalidator,
		txProcessor:        pr,
		done:               make(chan struct{}),
		DB:                 db,
		config:             rewardConfig,
		AtxDB:              atxDb,
		nextValidLayers:    make(map[types.LayerID]*types.Layer),
		latestLayer:        types.GetEffectiveGenesis(),
		latestLayerInState: types.GetEffectiveGenesis(),
	}

	ll.Validator = &validator{ll, 0}

	return ll
}

// NewRecoveredMesh creates new instance of mesh with recovered mesh data fom database
func NewRecoveredMesh(db *DB, atxDb AtxDB, rewardConfig Config, mesh tortoise, txInvalidator txMemPool, pr txProcessor, logger log.Log) *Mesh {
	msh := NewMesh(db, atxDb, rewardConfig, mesh, txInvalidator, pr, logger)

	latest, err := db.general.Get(constLATEST)
	if err != nil {
		logger.Panic("could not recover latest layer: %v", err)
	}
	msh.latestLayer = types.LayerID(util.BytesToUint64(latest))

	processed, err := db.general.Get(constPROCESSED)
	if err != nil {
		logger.Panic("could not recover processed layer: %v", err)
	}

	msh.SetProcessedLayer(types.LayerID(util.BytesToUint64(processed)))

	if msh.layerHash, err = db.general.Get(constLAYERHASH); err != nil {
		logger.With().Error("could not recover latest layer hash", log.Err(err))
	}

	verified, err := db.general.Get(VERIFIED)
	if err != nil {
		logger.Panic("could not recover latest verified layer: %v", err)
	}
	msh.latestLayerInState = types.LayerID(util.BytesToUint64(verified))

	err = pr.LoadState(msh.LatestLayerInState())
	if err != nil {
		logger.Panic("cannot load state for layer %v, message: %v", msh.LatestLayerInState(), err)
	}
	// in case we load a state that was not fully played
	if msh.LatestLayerInState()+1 < msh.trtl.LatestComplete() {
		// todo: add test for this case, or add random kill test on node
		logger.Info("playing layers %v to %v to state", msh.LatestLayerInState()+1, msh.trtl.LatestComplete())
		msh.pushLayersToState(msh.LatestLayerInState()+1, msh.trtl.LatestComplete())
	}

	msh.With().Info("recovered mesh from disk",
		log.FieldNamed("latest_layer", msh.latestLayer),
		log.FieldNamed("validated_layer", msh.ProcessedLayer()),
		log.String("layer_hash", util.Bytes2Hex(msh.layerHash)),
		log.String("root_hash", pr.GetStateRoot().String()))

	return msh
}

// CacheWarmUp warms up cache with latest blocks
func (msh *Mesh) CacheWarmUp(layerSize int) {
	start := types.LayerID(0)
	if msh.ProcessedLayer() > types.LayerID(msh.blockCache.Cap()/layerSize) {
		start = msh.ProcessedLayer() - types.LayerID(msh.blockCache.Cap()/layerSize)
	}

	if err := msh.cacheWarmUpFromTo(start, msh.ProcessedLayer()); err != nil {
		msh.Error("cache warm up failed during recovery", err)
	}

	msh.Info("cache warm up done")
}

// LatestLayerInState returns the latest layer we applied to state
func (msh *Mesh) LatestLayerInState() types.LayerID {
	defer msh.pMutex.RUnlock()
	msh.pMutex.RLock()
	return msh.latestLayerInState
}

// LatestLayer - returns the latest layer we saw from the network
func (msh *Mesh) LatestLayer() types.LayerID {
	defer msh.lkMutex.RUnlock()
	msh.lkMutex.RLock()
	return msh.latestLayer
}

// SetLatestLayer sets the latest layer we saw from the network
func (msh *Mesh) SetLatestLayer(idx types.LayerID) {
	// Report the status update, as well as the layer itself
	layer, err := msh.GetLayer(idx)
	if err != nil {
		msh.With().Error("error reading layer data", idx, log.Err(err))
	} else {
		events.ReportNewLayer(events.NewLayer{
			Layer:  layer,
			Status: events.LayerStatusTypeUnknown,
		})
	}
	defer msh.lkMutex.Unlock()
	msh.lkMutex.Lock()
	if idx > msh.latestLayer {
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

type validator struct {
	*Mesh
	processedLayer types.LayerID
}

func (vl *validator) ProcessedLayer() types.LayerID {
	defer vl.lvMutex.RUnlock()
	vl.lvMutex.RLock()
	return vl.processedLayer
}

func (vl *validator) SetProcessedLayer(lyr types.LayerID) {
	vl.With().Info("set processed layer", lyr)
	events.ReportNodeStatusUpdate()
	defer vl.lvMutex.Unlock()
	vl.lvMutex.Lock()
	vl.processedLayer = lyr
}

func (vl *validator) HandleLateBlock(b *types.Block) {
	vl.With().Info("validate late block", b.ID())
	oldPbase, newPbase := vl.trtl.HandleLateBlock(b)
	if err := vl.trtl.Persist(); err != nil {
		vl.With().Error("could not persist tortoise on late block", b.ID(), b.Layer())
	}
	vl.pushLayersToState(oldPbase, newPbase)
}

func (vl *validator) ValidateLayer(lyr *types.Layer) {
	vl.With().Info("validate layer", lyr)
	if len(lyr.Blocks()) == 0 {
		vl.With().Info("skip validation of layer with no blocks", lyr)
		vl.SetProcessedLayer(lyr.Index())
		events.ReportNewLayer(events.NewLayer{
			Layer:  lyr,
			Status: events.LayerStatusTypeConfirmed,
		})
		return
	}

	oldPbase, newPbase := vl.trtl.HandleIncomingLayer(lyr)
	vl.SetProcessedLayer(lyr.Index())

	if err := vl.trtl.Persist(); err != nil {
		vl.With().Error("could not persist tortoise", lyr)
	}
	if err := vl.general.Put(constPROCESSED, lyr.Index().Bytes()); err != nil {
		vl.With().Error("could not persist validated layer", lyr)
	}
	vl.pushLayersToState(oldPbase, newPbase)
	events.ReportNewLayer(events.NewLayer{
		Layer:  lyr,
		Status: events.LayerStatusTypeConfirmed,
	})
	vl.With().Info("done validating layer", lyr)
}

func (msh *Mesh) pushLayersToState(oldPbase types.LayerID, newPbase types.LayerID) {
	if oldPbase < 2 {
		msh.With().Warning("tried to push layer < 2",
			log.FieldNamed("old_pbase", oldPbase), log.FieldNamed("new_pbase", newPbase))
		if newPbase < 3 {
			return
		}
		oldPbase = 2
	}
	for layerID := oldPbase; layerID < newPbase; layerID++ {
		l, err := msh.GetLayer(layerID)
		// TODO: propagate/handle error
		if err != nil || l == nil {
			if layerID.GetEpoch().IsGenesis() {
				msh.With().Info("failed to get layer (expected for genesis layers)", layerID, log.Err(err))
			} else {
				msh.With().Error("failed to get layer", layerID, log.Err(err))
			}
			return
		}
		validBlocks, invalidBlocks := msh.BlocksByValidity(l.Blocks())
		msh.updateStateWithLayer(layerID, types.NewExistingLayer(layerID, validBlocks))
		msh.logStateRoot(l.Index())
		msh.persistLayerHashes(l)
		msh.reInsertTxsToPool(validBlocks, invalidBlocks, l.Index())
	}
	msh.persistLastLayerHash()
}

func (msh *Mesh) persistLayerHashes(l *types.Layer) {
	hash := msh.calcValidLayerHash(l)
	msh.persistLayerHash(l.Index(), hash)
	prevHash := types.Hash32{}
	var err error
	if l.Index() > types.GetEffectiveGenesis() {
		prevHash, err = msh.getRunningLayerHash(l.Index() - 1)
		if err != nil {
			msh.With().Error("cannot get running layer hash", l.Index()-1)
			return
		}
	}
	msh.persistRunningLayerHash(l.Index(), types.CalcAggregateHash32(prevHash, l.Hash().Bytes()))
	msh.layerHash = hash.Bytes()
}

func (msh *Mesh) reInsertTxsToPool(validBlocks, invalidBlocks []*types.Block, l types.LayerID) {
	seenTxIds := make(map[types.TransactionID]struct{})
	uniqueTxIds(validBlocks, seenTxIds)
	returnedTxs := msh.getTxs(uniqueTxIds(invalidBlocks, seenTxIds), l)
	grouped, accounts := msh.removeFromUnappliedTxs(returnedTxs)
	for account := range accounts {
		msh.removeRejectedFromAccountTxs(account, grouped, l)
	}
	for _, tx := range returnedTxs {
		err := msh.ValidateAndAddTxToPool(tx)
		// We ignore errors here, since they mean that the tx is no longer valid and we shouldn't re-add it
		if err == nil {
			msh.With().Info("transaction from contextually invalid block re-added to mempool", tx.ID())
		}
	}
}

func (msh *Mesh) applyState(l *types.Layer) {
	msh.accumulateRewards(l, msh.config)
	msh.pushTransactions(l)
	msh.setLatestLayerInState(l.Index())
	events.ReportNewLayer(events.NewLayer{
		Layer:  l,
		Status: events.LayerStatusTypeApproved,
	})
}

// HandleValidatedLayer handles layer valid blocks as decided by hare
func (msh *Mesh) HandleValidatedLayer(ctx context.Context, validatedLayer types.LayerID, layer []types.BlockID) {
	var blocks []*types.Block

	for _, blockID := range layer {
		block, err := msh.GetBlock(blockID)
		if err != nil {
			// stop processing this hare result, wait until tortoise pushes this layer into state
			msh.WithContext(ctx).Error("hare terminated with block that is not present in mesh")
			return
		}
		blocks = append(blocks, block)
	}
	lyr := types.NewExistingLayer(validatedLayer, blocks)
	invalidBlocks := msh.getInvalidBlocksByHare(ctx, lyr)
	// Reporting of the validated layer happens deep inside this call stack, below
	// updateStateWithLayer, inside applyState. No need to report here.
	msh.updateStateWithLayer(validatedLayer, lyr)
	msh.reInsertTxsToPool(blocks, invalidBlocks, lyr.Index())

	// get the full layer incl invalid blocks
	for _, bl := range invalidBlocks {
		lyr.AddBlock(bl)
	}

	msh.Log.With().Info("mesh validating layer", lyr.Index().Field(), log.Int("valid_blocks", len(blocks)), log.Int("invalid_blocks", len(invalidBlocks)))

	if err := msh.SaveLayerInputVectorByID(lyr.Index(), types.BlockIDs(blocks)); err != nil {
		msh.Log.With().Error("Saving layer input vector failed", lyr.Index().Field())
	}
	msh.ValidateLayer(lyr)
}

func (msh *Mesh) getInvalidBlocksByHare(ctx context.Context, hareLayer *types.Layer) (invalid []*types.Block) {
	dbLayer, err := msh.GetLayer(hareLayer.Index())
	if err != nil {
		msh.WithContext(ctx).With().Panic("failed to get layer", log.Err(err))
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

func (msh *Mesh) updateStateWithLayer(validatedLayer types.LayerID, layer *types.Layer) {
	msh.txMutex.Lock()
	defer msh.txMutex.Unlock()
	latest := msh.LatestLayerInState()
	if validatedLayer <= latest {
		msh.With().Warning("result received after state has advanced",
			log.FieldNamed("validatedLayer", validatedLayer),
			log.FieldNamed("latestLayer", latest))
		return
	}
	if msh.maxValidatedLayer < validatedLayer {
		msh.maxValidatedLayer = validatedLayer
	}
	if validatedLayer > latest+1 {
		msh.With().Warning("early layer result received",
			log.FieldNamed("validatedLayer", validatedLayer),
			log.FieldNamed("maxValidatedLayer", msh.maxValidatedLayer),
			log.FieldNamed("latestLayer", latest))
		msh.nextValidLayers[validatedLayer] = layer
		return
	}
	msh.applyState(layer)
	for i := validatedLayer + 1; i <= msh.maxValidatedLayer; i++ {
		nxtLayer, has := msh.nextValidLayers[i]
		if !has {
			break
		}
		msh.applyState(nxtLayer)
		delete(msh.nextValidLayers, i)
	}
}

func (msh *Mesh) setLatestLayerInState(lyr types.LayerID) {
	// update validated layer only after applying transactions since loading of state depends on processedLayer param.
	msh.pMutex.Lock()
	if err := msh.general.Put(VERIFIED, lyr.Bytes()); err != nil {
		msh.Panic("could not persist validated layer index %d", lyr)
	}
	msh.latestLayerInState = lyr
	msh.pMutex.Unlock()
}

func (msh *Mesh) logStateRoot(layerID types.LayerID) {
	msh.Event().Info("end of layer state root", layerID,
		log.String("state_root", util.Bytes2Hex(msh.txProcessor.GetStateRoot().Bytes())),
	)
}

func (msh *Mesh) calcValidLayerHash(layer *types.Layer) types.Hash32 {
	validBlocks, _ := msh.BlocksByValidity(layer.Blocks())
	msh.layerHash = types.CalcBlocksHash32(types.BlockIDs(validBlocks), msh.layerHash).Bytes()

	msh.Event().Info("new layer hash", layer.Index(),
		log.String("layer_hash", util.Bytes2Hex(msh.layerHash)))

	return types.CalcBlocksHash32(types.BlockIDs(validBlocks), msh.layerHash)
}

func (msh *Mesh) persistLastLayerHash() {
	if err := msh.general.Put(constLAYERHASH, msh.layerHash); err != nil {
		msh.With().Error("failed to persist last layer hash", log.Err(err), msh.ProcessedLayer(),
			log.String("layer_hash", util.Bytes2Hex(msh.layerHash)))
	}
}

func (msh *Mesh) persistRunningLayerHash(layerID types.LayerID, hash types.Hash32) {
	if err := msh.general.Put(msh.getRunningLayerHashKey(layerID), hash.Bytes()); err != nil {
		msh.With().Error("failed to persist running layer hash", log.Err(err), msh.ProcessedLayer(),
			log.String("layer_hash", hash.Hex()))
	}
}

func (msh *Mesh) getRunningLayerHash(layerID types.LayerID) (types.Hash32, error) {
	bts, err := msh.general.Get(msh.getRunningLayerHashKey(layerID))
	if err != nil {
		return [32]byte{}, err
	}
	var hash types.Hash32
	hash.SetBytes(bts)
	return hash, nil
}

// GetLayerHashBlocks returns blocks for given hash
func (msh *Mesh) GetLayerHashBlocks(h types.Hash32) []types.BlockID {
	layerIDBytes, err := msh.general.Get(h.Bytes())
	if err != nil {
		msh.Warning("requested unknown layer hash %v", h.Hex())
		return []types.BlockID{}
	}
	l := types.BytesToLayerID(layerIDBytes)
	mBlocks, err := msh.LayerBlockIds(l)
	if err != nil {
		return []types.BlockID{}
	}
	return mBlocks
}

func (msh *Mesh) getLayerBlockHashKey(layerID types.LayerID) []byte {
	return []byte(fmt.Sprintf("layerBlockHash_%v", layerID.Bytes()))
}

func (msh *Mesh) getRunningLayerHashKey(layerID types.LayerID) []byte {
	return []byte(fmt.Sprintf("rLayerHash_%v", layerID.Bytes()))
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
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldBase`
	}
	msh.removeFromUnappliedTxs(validBlockTxs)
	msh.With().Info("applied transactions",
		log.Int("valid_block_txs", len(validBlockTxs)),
		l.Index(),
		log.Int("num_failed_txs", numFailedTxs),
	)
}

// GetProcessedLayer returns a layer only if it has already been processed
func (msh *Mesh) GetProcessedLayer(i types.LayerID) (*types.Layer, error) {
	msh.lMutex.RLock()
	if i > msh.ProcessedLayer() {
		msh.lMutex.RUnlock()
		msh.Debug("failed to get layer  ", i, " layer not verified yet")
		return nil, errors.New("layer not verified yet")
	}
	msh.lMutex.RUnlock()
	return msh.GetLayer(i)
}

// AddBlock adds a block to the database ignoring the block txs/atxs
// ***USED ONLY FOR TESTS***
func (msh *Mesh) AddBlock(blk *types.Block) error {
	msh.Debug("add block %d", blk.ID())
	if err := msh.DB.AddBlock(blk); err != nil {
		msh.Warning("failed to add block %v  %v", blk.ID(), err)

		if blk.ID() != GenesisBlock().ID() {
			return err
		}
	}

	l, err := msh.GetLayer(blk.LayerIndex)
	if err != nil {
		return err
	}
	msh.persistLayerHashes(l)
	msh.SetLatestLayer(blk.Layer())
	// new block add to orphans
	msh.handleOrphanBlocks(blk)

	// invalidate txs and atxs from pool
	msh.invalidateFromPools(&blk.MiniBlock)
	return nil
}

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
		return fmt.Errorf("layer has blocks")
	}

	msh.SetLatestLayer(lyr)
	lm := msh.getLayerMutex(lyr)
	defer msh.endLayerWorker(lyr)
	lm.m.Lock()
	defer lm.m.Unlock()
	// layer doesnt exist, need to insert new layer
	return msh.AddZeroBlockLayer(lyr)
}

// AddBlockWithTxs adds a block to the database
// blk - the block to add
// txs - block txs that we dont have in our tx database yet
func (msh *Mesh) AddBlockWithTxs(blk *types.Block) error {
	msh.With().Debug("adding block", blk.Fields()...)

	err := msh.StoreTransactionsFromPool(blk)
	if err != nil {
		msh.Log.Error("not all txs were processed %v", err)
	}

	// Store block (delete if storing ATXs fails)
	err = msh.DB.AddBlock(blk)
	if err != nil && err == ErrAlreadyExist {
		return nil
	}

	if err != nil {
		msh.With().Error("failed to add block", blk.ID(), log.Err(err))
		return err
	}

	msh.SetLatestLayer(blk.Layer())
	// new block add to orphans
	msh.handleOrphanBlocks(blk)

	events.ReportNewBlock(blk)
	msh.With().Info("added block to database", blk.Fields()...)
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
	if err := msh.writeTransactions(blk.LayerIndex, txs); err != nil {
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
	msh.orphMutex.Lock()
	defer msh.orphMutex.Unlock()
	if _, ok := msh.orphanBlocks[blk.Layer()]; !ok {
		msh.orphanBlocks[blk.Layer()] = make(map[types.BlockID]struct{})
	}
	msh.orphanBlocks[blk.Layer()][blk.ID()] = struct{}{}
	msh.Debug("Added block %s to orphans", blk.ID())
	for _, b := range append(blk.ForDiff, append(blk.AgainstDiff, blk.NeutralDiff...)...) {
		for layerID, layermap := range msh.orphanBlocks {
			if _, has := layermap[b]; has {
				msh.Log.With().Debug("delete block from orphans", b)
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
	msh.orphMutex.RLock()
	defer msh.orphMutex.RUnlock()
	ids := map[types.BlockID]struct{}{}
	for key, val := range msh.orphanBlocks {
		if key < l {
			for bid := range val {
				ids[bid] = struct{}{}
			}
		}
	}

	blocks, err := msh.LayerBlockIds(l - 1)
	if err != nil {
		return nil, fmt.Errorf("failed getting latest layer %v err %v", l-1, err)
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
	//the reason we are serializing the types.NodeID to a string instead of using it directly as a
	//key in the map is due to Golang's restriction on only Comparable types used as map keys. Since
	//the types.NodeID contains a slice, it is not comparable and hence cannot be used as a map key
	//TODO: fix this when changing the types.NodeID struct, see https://github.com/spacemeshos/go-spacemesh/issues/2269
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
		//create a 2 dimensional map where the entries are
		//coinbasesAndSmeshers[coinbase_id][smesher_id] = number of blocks this pair has created
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
				log.With().Error("unable to convert bytes to nodeid", log.Err(err),
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
