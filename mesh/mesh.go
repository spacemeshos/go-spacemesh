package mesh

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/seehuhn/mt19937"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"math/rand"

	"math/big"

	"sync"
)

const (
	layerSize = 200
	Genesis   = types.LayerID(0)
)

var TRUE = []byte{1}
var FALSE = []byte{0}
var LATEST = []byte("latest")
var LAYERHASH = []byte("layer hash")
var PROCESSED = []byte("proccessed")
var TORTOISE = []byte("tortoise")
var VERIFIED = []byte("verified") //refers to layers we pushed into the state

type MeshValidator interface {
	HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID)
	LatestComplete() types.LayerID
	PersistTortoise() error
	HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID)
}

type TxProcessor interface {
	ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error)
	ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int)
	ValidateSignature(s types.Signed) (types.Address, error)
	AddressExists(addr types.Address) bool
	ValidateNonceAndBalance(transaction *types.Transaction) error
	GetLayerApplied(txId types.TransactionId) *types.LayerID
	GetStateRoot() types.Hash32
	LoadState(layer types.LayerID) error
}

type TxMemPoolInValidator interface {
	Invalidate(id types.TransactionId)
}

type AtxMemPoolInValidator interface {
	Invalidate(id types.AtxId)
}

type AtxDB interface {
	ProcessAtxs(atxs []*types.ActivationTx) error
	GetAtxHeader(id types.AtxId) (*types.ActivationTxHeader, error)
	GetFullAtx(id types.AtxId) (*types.ActivationTx, error)
	SyntacticallyValidateAtx(atx *types.ActivationTx) error
}

type BlockBuilder interface {
	ValidateAndAddTxToPool(tx *types.Transaction) error
}

type Mesh struct {
	log.Log
	*MeshDB
	AtxDB
	TxProcessor
	MeshValidator
	blockBuilder       BlockBuilder
	txInvalidator      TxMemPoolInValidator
	atxInvalidator     AtxMemPoolInValidator
	config             Config
	processedLayer     types.LayerID
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

func NewMesh(db *MeshDB, atxDb AtxDB, rewardConfig Config, mesh MeshValidator, txInvalidator TxMemPoolInValidator, atxInvalidator AtxMemPoolInValidator, pr TxProcessor, logger log.Log) *Mesh {
	ll := &Mesh{
		Log:             logger,
		MeshValidator:   mesh,
		txInvalidator:   txInvalidator,
		atxInvalidator:  atxInvalidator,
		TxProcessor:     pr,
		done:            make(chan struct{}),
		MeshDB:          db,
		config:          rewardConfig,
		AtxDB:           atxDb,
		nextValidLayers: make(map[types.LayerID]*types.Layer),
	}

	return ll
}

func NewRecoveredMesh(db *MeshDB, atxDb AtxDB, rewardConfig Config, mesh MeshValidator, txInvalidator TxMemPoolInValidator, atxInvalidator AtxMemPoolInValidator, pr TxProcessor, logger log.Log) *Mesh {
	msh := NewMesh(db, atxDb, rewardConfig, mesh, txInvalidator, atxInvalidator, pr, logger)

	latest, err := db.general.Get(LATEST)
	if err != nil {
		logger.Panic("could not recover latest layer: %v", err)
	}
	msh.latestLayer = types.LayerID(util.BytesToUint64(latest))

	processed, err := db.general.Get(PROCESSED)
	if err != nil {
		logger.Panic("could not recover processed layer: %v", err)
	}
	msh.processedLayer = types.LayerID(util.BytesToUint64(processed))

	if msh.layerHash, err = db.general.Get(LAYERHASH); err != nil {
		logger.With().Error("could not recover latest layer hash", log.Err(err))
	}

	verified, err := db.general.Get(VERIFIED)
	if err != nil {
		logger.Panic("could not recover latest verified layer: %v", err)
	}
	msh.latestLayerInState = types.LayerID(util.BytesToUint64(verified))

	start := types.LayerID(0)
	if msh.processedLayer > types.LayerID(db.blockCache.Cap()) {
		start = msh.processedLayer - types.LayerID(db.blockCache.Cap())
	}

	if err := msh.CacheWarmUp(start, msh.processedLayer); err != nil {
		logger.Error("cache warm up failed during recovery", err)
	}

	err = pr.LoadState(msh.LatestLayerInState())
	if err != nil {
		logger.Panic("cannot load state for layer %v, message: %v", msh.LatestLayerInState(), err)
	}
	// in case we load a state that was not fully played
	if msh.LatestLayerInState()+1 < msh.MeshValidator.LatestComplete() {
		// todo: add test for this case, or add random kill test on node
		logger.Info("playing layers %v to %v to state", msh.LatestLayerInState()+1, msh.MeshValidator.LatestComplete())
		msh.pushLayersToState(msh.LatestLayerInState()+1, msh.MeshValidator.LatestComplete())
	}

	msh.With().Info("recovered mesh from disc",
		log.Uint64("latest_layer", msh.latestLayer.Uint64()),
		log.Uint64("validated_layer", msh.processedLayer.Uint64()),
		log.String("layer_hash", util.Bytes2Hex(msh.layerHash)),
		log.String("root_hash", pr.GetStateRoot().String()))

	return msh
}

func (m *Mesh) SetBlockBuilder(blockBuilder BlockBuilder) {
	m.blockBuilder = blockBuilder
}

func (m *Mesh) ProcessedLayer() types.LayerID {
	defer m.lvMutex.RUnlock()
	m.lvMutex.RLock()
	return m.processedLayer
}

func (m *Mesh) LatestLayerInState() types.LayerID {
	defer m.pMutex.RUnlock()
	m.pMutex.RLock()
	return m.latestLayerInState
}

// LatestLayer - returns the latest layer we saw from the network
func (m *Mesh) LatestLayer() types.LayerID {
	defer m.lkMutex.RUnlock()
	m.lkMutex.RLock()
	return m.latestLayer
}

func (m *Mesh) SetLatestLayer(idx types.LayerID) {
	defer m.lkMutex.Unlock()
	m.lkMutex.Lock()
	if idx > m.latestLayer {
		m.Info("set latest known layer to %v", idx)
		m.latestLayer = idx
		if err := m.general.Put(LATEST, idx.ToBytes()); err != nil {
			m.Error("could not persist Latest layer index")
		}
	}
}

func (m *Mesh) GetLayer(index types.LayerID) (*types.Layer, error) {
	mBlocks, err := m.LayerBlocks(index)
	if err != nil {
		return nil, err
	}

	l := types.NewLayer(index)
	l.SetBlocks(mBlocks)

	return l, nil
}

func (m *Mesh) HandleLateBlock(b *types.Block) {
	m.Info("Validate late block %s", b.Id())
	oldPbase, newPbase := m.MeshValidator.HandleLateBlock(b)
	m.pushLayersToState(oldPbase, newPbase)
	if err := m.PersistTortoise(); err != nil {
		m.Error("could not persist Tortoise on late block %s from layer index %d", b.Id(), b.Layer())
	}
}

func (m *Mesh) ValidateLayer(lyr *types.Layer) {
	m.Info("Validate layer %d", lyr.Index())
	oldPbase, newPbase := m.HandleIncomingLayer(lyr)
	m.lvMutex.Lock()
	m.processedLayer = lyr.Index()
	if err := m.PersistTortoise(); err != nil {
		m.Error("could not persist Tortoise layer index %d", lyr.Index())
	}
	if err := m.general.Put(PROCESSED, lyr.Index().ToBytes()); err != nil {
		m.Error("could not persist validated layer index %d", lyr.Index())
	}
	m.lvMutex.Unlock()
	m.pushLayersToState(oldPbase, newPbase)
	m.Info("done validating layer %v", lyr.Index())
}

func (m *Mesh) pushLayersToState(oldPbase types.LayerID, newPbase types.LayerID) {
	for layerId := oldPbase; layerId < newPbase; layerId++ {
		l, err := m.GetLayer(layerId)
		if err != nil || l == nil {
			m.With().Error("failed to get layer", log.LayerId(layerId.Uint64()), log.Err(err))
			return
		}
		validBlocks, _ := m.BlocksByValidity(l.Blocks())
		m.updateStateWithLayer(layerId, types.NewExistingLayer(layerId, validBlocks))
	}
	m.persistLayerHash()
}

func (m *Mesh) applyState(layerId types.LayerID, l *types.Layer) {
	m.AccumulateRewards(l, m.config)
	m.PushTransactions(l)
	m.logStateRoot(layerId)
	m.setLayerHash(l)
	m.setLatestLayerInState(layerId)
}

func (m *Mesh) HandleValidatedLayer(validatedLayer types.LayerID, layer []types.BlockID) {
	blocks := []*types.Block{}

	for _, blockId := range layer {
		block, err := m.GetBlock(blockId)
		if err != nil {
			//todo: can this happen?
			log.Panic("hare terminated with block that is not present in mesh")
		}
		blocks = append(blocks, block)
	}
	lyr := types.NewExistingLayer(validatedLayer, blocks)
	m.updateStateWithLayer(validatedLayer, lyr)
}

func (m *Mesh) updateStateWithLayer(validatedLayer types.LayerID, layer *types.Layer) {
	m.txMutex.Lock()
	defer m.txMutex.Unlock()
	latest := m.LatestLayerInState()
	if validatedLayer <= latest {
		log.Info("result received after state has been advanced for layer %v, latest: %v", validatedLayer, latest)
		return
	}
	if m.maxValidatedLayer < validatedLayer {
		m.maxValidatedLayer = validatedLayer
	}
	if validatedLayer > latest+1 {
		log.Info("early layer result was received for layer %v, max validated so far %v", validatedLayer, m.maxValidatedLayer)
		m.nextValidLayers[validatedLayer] = layer
		return
	}
	m.applyState(validatedLayer, layer)
	for i := validatedLayer + 1; i <= m.maxValidatedLayer; i++ {
		nxtLayer, has := m.nextValidLayers[i]
		if !has {
			break
		}
		m.applyState(i, nxtLayer)
		delete(m.nextValidLayers, i)
	}
}

func (m *Mesh) setLatestLayerInState(lyr types.LayerID) {
	// update validated layer only after applying transactions since loading of state depends on processedLayer param.
	m.pMutex.Lock()
	if err := m.general.Put(VERIFIED, lyr.ToBytes()); err != nil {
		m.Panic("could not persist validated layer index %d", lyr)
	}
	m.latestLayerInState = lyr
	m.pMutex.Unlock()
}

func (m *Mesh) logStateRoot(layerId types.LayerID) {
	m.Event().Info("end of layer state root",
		log.LayerId(layerId.Uint64()),
		log.String("state_root", util.Bytes2Hex(m.TxProcessor.GetStateRoot().Bytes())),
	)
}

func (m *Mesh) setLayerHash(layer *types.Layer) {
	validBlocks, _ := m.BlocksByValidity(layer.Blocks())
	m.layerHash = types.CalcBlocksHash32(types.BlockIds(validBlocks), m.layerHash).Bytes()

	m.Event().Info("new layer hash",
		log.LayerId(layer.Index().Uint64()),
		log.String("layer_hash", util.Bytes2Hex(m.layerHash)))
}

func (m *Mesh) persistLayerHash() {
	if err := m.general.Put(LAYERHASH, m.layerHash); err != nil {
		m.With().Error("failed to persist layer hash", log.Err(err), log.LayerId(m.processedLayer.Uint64()),
			log.String("layer_hash", util.Bytes2Hex(m.layerHash)))
	}
}

func (m *Mesh) ExtractUniqueOrderedTransactions(l *types.Layer) (validBlockTxs []*types.Transaction) {
	// this flow is deactivated since we assume all blocks that get here either passed hare or tortoise
	validBlocks := l.Blocks()

	// Deterministically sort valid blocks
	types.SortBlocks(validBlocks)

	// Initialize a Mersenne Twister seeded with layerHash
	blockHash := types.CalcBlockHash32Presorted(types.BlockIds(validBlocks), nil)
	mt := mt19937.New()
	mt.SeedFromSlice(toUint64Slice(blockHash.Bytes()))
	rng := rand.New(mt)

	// Perform a Fisher-Yates shuffle on the blocks
	rng.Shuffle(len(validBlocks), func(i, j int) {
		validBlocks[i], validBlocks[j] = validBlocks[j], validBlocks[i]
	})

	// Get and return unique transactions
	seenTxIds := map[types.TransactionId]struct{}{}
	return m.getTxs(uniqueTxIds(validBlocks, seenTxIds), l)
}

func toUint64Slice(b []byte) []uint64 {
	l := len(b)
	var s []uint64
	for i := 0; i < l; i += 8 {
		s = append(s, binary.LittleEndian.Uint64(b[i:util.Min(l, i+8)]))
	}
	return s
}

func uniqueTxIds(blocks []*types.Block, seenTxIds map[types.TransactionId]struct{}) []types.TransactionId {
	var txIds []types.TransactionId
	for _, b := range blocks {
		for _, id := range b.TxIds {
			if _, found := seenTxIds[id]; found {
				continue
			}
			txIds = append(txIds, id)
			seenTxIds[id] = struct{}{}
		}
	}
	return txIds
}

func (m *Mesh) getTxs(txIds []types.TransactionId, l *types.Layer) []*types.Transaction {
	txs, missing := m.GetTransactions(txIds)
	if len(missing) != 0 {
		m.Panic("could not find transactions %v from layer %v", missing, l.Index())
	}
	return txs
}

func (m *Mesh) PushTransactions(l *types.Layer) {
	validBlockTxs := m.ExtractUniqueOrderedTransactions(l)
	numFailedTxs, err := m.ApplyTransactions(l.Index(), validBlockTxs)
	if err != nil {
		m.With().Error("failed to apply transactions",
			log.LayerId(l.Index().Uint64()), log.Int("num_failed_txs", numFailedTxs), log.Err(err))
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldBase`
	}
	m.removeFromUnappliedTxs(validBlockTxs, l.Index())
	m.With().Info("applied transactions",
		log.Int("valid_block_txs", len(validBlockTxs)),
		log.LayerId(l.Index().Uint64()),
		log.Int("num_failed_txs", numFailedTxs),
	)
}

//todo consider adding a boolean for layer validity instead error
func (m *Mesh) GetVerifiedLayer(i types.LayerID) (*types.Layer, error) {
	m.lMutex.RLock()
	if i > types.LayerID(m.processedLayer) {
		m.lMutex.RUnlock()
		m.Debug("failed to get layer  ", i, " layer not verified yet")
		return nil, errors.New("layer not verified yet")
	}
	m.lMutex.RUnlock()
	return m.GetLayer(i)
}

func (m *Mesh) GetLatestView() []types.BlockID {
	//todo: think about whether we want to use the most recent layer or the recent verified layer
	layer, err := m.GetLayer(m.LatestLayer())
	if err != nil {
		panic("got an error trying to read latest view")
	}
	view := make([]types.BlockID, 0, len(layer.Blocks()))
	for _, blk := range layer.Blocks() {
		view = append(view, blk.Id())
	}
	return view
}

func (m *Mesh) AddBlock(blk *types.Block) error {
	m.Debug("add block %d", blk.Id())
	if err := m.MeshDB.AddBlock(blk); err != nil {
		m.Warning("failed to add block %v  %v", blk.Id(), err)
		return err
	}
	m.SetLatestLayer(blk.Layer())
	//new block add to orphans
	m.handleOrphanBlocks(blk)

	//invalidate txs and atxs from pool
	m.invalidateFromPools(&blk.MiniBlock)
	return nil
}

func (m *Mesh) AddBlockWithTxs(blk *types.Block, txs []*types.Transaction, atxs []*types.ActivationTx) error {
	m.With().Debug("adding block", log.BlockId(blk.Id().String()))

	// Store transactions (doesn't have to be rolled back if other writes fail)
	if len(txs) > 0 {
		if err := m.writeTransactions(blk.LayerIndex, txs); err != nil {
			return fmt.Errorf("could not write transactions of block %v database: %v", blk.Id(), err)
		}

		if err := m.addToUnappliedTxs(txs, blk.LayerIndex); err != nil {
			return fmt.Errorf("failed to add to unappliedTxs: %v", err)
		}
	}

	// Store block (delete if storing ATXs fails)
	if err := m.MeshDB.AddBlock(blk); err != nil && err != ErrAlreadyExist {
		m.With().Error("failed to add block", log.BlockId(blk.Id().String()), log.Err(err))
		return err
	}

	// Store ATXs (atomically, delete the block on failure)
	if err := m.AtxDB.ProcessAtxs(atxs); err != nil {
		// Roll back adding the block (delete it)
		if err := m.blocks.Delete(blk.Id().ToBytes()); err != nil {
			m.With().Warning("failed to roll back adding a block", log.Err(err), log.BlockId(blk.Id().String()))
		}
		return fmt.Errorf("failed to process ATXs: %v", err)
	}

	m.SetLatestLayer(blk.Layer())
	//new block add to orphans
	m.handleOrphanBlocks(blk)

	//invalidate txs and atxs from pool
	m.invalidateFromPools(&blk.MiniBlock)

	events.Publish(events.NewBlock{Id: blk.Id().String(), Atx: blk.ATXID.ShortString(), Layer: uint64(blk.LayerIndex)})
	m.With().Info("added block to database ", log.BlockId(blk.Id().String()), log.LayerId(uint64(blk.LayerIndex)))
	return nil
}

func (m *Mesh) invalidateFromPools(blk *types.MiniBlock) {
	for _, id := range blk.TxIds {
		m.txInvalidator.Invalidate(id)
	}
	m.atxInvalidator.Invalidate(blk.ATXID)
	for _, id := range blk.AtxIds {
		m.atxInvalidator.Invalidate(id)
	}
}

//todo better thread safety
func (m *Mesh) handleOrphanBlocks(blk *types.Block) {
	m.orphMutex.Lock()
	defer m.orphMutex.Unlock()
	if _, ok := m.orphanBlocks[blk.Layer()]; !ok {
		m.orphanBlocks[blk.Layer()] = make(map[types.BlockID]struct{})
	}
	m.orphanBlocks[blk.Layer()][blk.Id()] = struct{}{}
	m.Debug("Added block %s to orphans", blk.Id())
	for _, b := range blk.ViewEdges {
		for layerId, layermap := range m.orphanBlocks {
			if _, has := layermap[b]; has {
				m.Log.Debug("delete block ", b, "from orphans")
				delete(layermap, b)
				if len(layermap) == 0 {
					delete(m.orphanBlocks, layerId)
				}
				break
			}
		}
	}
}

func (m *Mesh) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	m.orphMutex.RLock()
	defer m.orphMutex.RUnlock()
	ids := map[types.BlockID]struct{}{}
	for key, val := range m.orphanBlocks {
		if key < l {
			for bid := range val {
				ids[bid] = struct{}{}
			}
		}
	}

	blocks, err := m.LayerBlockIds(l - 1)
	if err != nil {
		return nil, errors.New(fmt.Sprint("failed getting latest layer ", err))
	}

	//add last layer blocks
	for _, b := range blocks {
		ids[b] = struct{}{}
	}

	idArr := make([]types.BlockID, 0, len(ids))
	for i := range ids {
		idArr = append(idArr, i)
	}

	idArr = types.SortBlockIds(idArr)

	m.Info("orphans for layer %d are %v", l, idArr)
	return idArr, nil
}

func (m *Mesh) AccumulateRewards(l *types.Layer, params Config) {
	ids := make([]types.Address, 0, len(l.Blocks()))
	for _, bl := range l.Blocks() {
		if bl.ATXID == *types.EmptyAtxId {
			m.With().Info("skipping reward distribution for block with no ATX",
				log.LayerId(uint64(bl.LayerIndex)), log.BlockId(bl.Id().String()))
			continue
		}
		atx, err := m.AtxDB.GetAtxHeader(bl.ATXID)
		if err != nil {
			m.With().Warning("Atx from block not found in db", log.Err(err), log.BlockId(bl.Id().String()), log.AtxId(bl.ATXID.ShortString()))
			continue
		}
		ids = append(ids, atx.Coinbase)

	}

	if len(ids) == 0 {
		m.With().Info("no valid blocks for layer ", log.LayerId(uint64(l.Index())))
		return
	}

	// aggregate all blocks' rewards
	txs := m.ExtractUniqueOrderedTransactions(l)

	totalReward := &big.Int{}
	for _, tx := range txs {
		totalReward.Add(totalReward, new(big.Int).SetUint64(tx.Fee))
	}

	layerReward := CalculateLayerReward(l.Index(), params)
	totalReward.Add(totalReward, layerReward)

	numBlocks := big.NewInt(int64(len(ids)))

	blockTotalReward := calculateActualRewards(l.Index(), totalReward, numBlocks)
	m.ApplyRewards(l.Index(), ids, blockTotalReward)

	blockLayerReward := calculateActualRewards(l.Index(), layerReward, numBlocks)
	err := m.writeTransactionRewards(l.Index(), ids, blockTotalReward, blockLayerReward)
	if err != nil {
		m.Error("cannot write reward to db")
	}
	//todo: should miner id be sorted in a deterministic order prior to applying rewards?

}

var GenesisBlock = types.NewExistingBlock(0, []byte("genesis"))

func GenesisLayer() *types.Layer {
	l := types.NewLayer(Genesis)
	l.AddBlock(GenesisBlock)
	return l
}

func (m *Mesh) GetATXs(atxIds []types.AtxId) (map[types.AtxId]*types.ActivationTx, []types.AtxId) {
	var mIds []types.AtxId
	atxs := make(map[types.AtxId]*types.ActivationTx, len(atxIds))
	for _, id := range atxIds {
		t, err := m.GetFullAtx(id)
		if err != nil {
			m.Warning("could not get atx %v  from database, %v", id.ShortString(), err)
			mIds = append(mIds, id)
		} else {
			atxs[t.Id()] = t
		}
	}
	return atxs, mIds
}

// TEST ONLY
func NewSignedTx(nonce uint64, rec types.Address, amount, gas, fee uint64, signer *signing.EdSigner) (*types.Transaction, error) {
	inner := types.InnerTransaction{
		AccountNonce: nonce,
		Recipient:    rec,
		Amount:       amount,
		GasLimit:     gas,
		Fee:          fee,
	}

	buf, err := types.InterfaceToBytes(&inner)
	if err != nil {
		return nil, err
	}

	sst := &types.Transaction{
		InnerTransaction: inner,
		Signature:        [64]byte{},
	}

	copy(sst.Signature[:], signer.Sign(buf))
	addr := types.Address{}
	addr.SetBytes(signer.PublicKey().Bytes())
	sst.SetOrigin(addr)

	return sst, nil
}
