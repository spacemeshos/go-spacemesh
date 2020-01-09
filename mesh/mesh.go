package mesh

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/seehuhn/mt19937"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/sha256-simd"
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"

	"math/big"

	"sync"
)

const (
	layerSize   = 200
	Genesis     = types.LayerID(0)
	GenesisId   = 420
	TxCacheSize = 1000
)

var TRUE = []byte{1}
var FALSE = []byte{0}
var LATEST = []byte("latest")
var VALIDATED = []byte("validated")
var TORTOISE = []byte("tortoise")

type MeshValidator interface {
	HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID)
	HandleLateBlock(bl *types.Block)
}

type TxProcessor interface {
	ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error)
	ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int)
	ValidateSignature(s types.Signed) (types.Address, error)
	AddressExists(addr types.Address) bool
	ValidateNonceAndBalance(transaction *types.Transaction) error
	GetLayerApplied(txId types.TransactionId) *types.LayerID
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
	blockBuilder   BlockBuilder
	txInvalidator  TxMemPoolInValidator
	atxInvalidator AtxMemPoolInValidator
	config         Config
	validatedLayer types.LayerID
	latestLayer    types.LayerID
	lMutex         sync.RWMutex
	lkMutex        sync.RWMutex
	lcMutex        sync.RWMutex
	lvMutex        sync.RWMutex
	orphMutex      sync.RWMutex
	done           chan struct{}
}

func NewMesh(db *MeshDB, atxDb AtxDB, rewardConfig Config, mesh MeshValidator, txInvalidator TxMemPoolInValidator, atxInvalidator AtxMemPoolInValidator, pr TxProcessor, logger log.Log) *Mesh {
	ll := &Mesh{
		Log:            logger,
		MeshValidator:  mesh,
		txInvalidator:  txInvalidator,
		atxInvalidator: atxInvalidator,
		TxProcessor:    pr,
		done:           make(chan struct{}),
		MeshDB:         db,
		config:         rewardConfig,
		AtxDB:          atxDb,
	}

	return ll
}

func NewRecoveredMesh(db *MeshDB, atxDb AtxDB, rewardConfig Config, mesh MeshValidator, txInvalidator TxMemPoolInValidator, atxInvalidator AtxMemPoolInValidator, pr TxProcessor, logger log.Log) *Mesh {
	ll := NewMesh(db, atxDb, rewardConfig, mesh, txInvalidator, atxInvalidator, pr, logger)

	latest, err := db.general.Get(LATEST)
	if err != nil {
		logger.Panic("could not recover latest layer")
	}

	ll.latestLayer = types.LayerID(util.BytesToUint64(latest))

	validated, err := db.general.Get(VALIDATED)
	if err != nil {
		logger.Panic("could not recover  validated layer")
	}

	ll.validatedLayer = types.LayerID(util.BytesToUint64(validated))
	ll.Info("recovered mesh from disc latest layer %d validated layer %d", ll.latestLayer, ll.validatedLayer)

	return ll
}

func (m *Mesh) SetBlockBuilder(blockBuilder BlockBuilder) {
	m.blockBuilder = blockBuilder
}

func (m *Mesh) ValidatedLayer() types.LayerID {
	defer m.lvMutex.RUnlock()
	m.lvMutex.RLock()
	return m.validatedLayer
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

func (m *Mesh) ValidateLayer(lyr *types.Layer) {
	m.Info("Validate layer %d", lyr.Index())

	oldPbase, newPbase := m.HandleIncomingLayer(lyr)
	//oldPbase, newPbase := lyr.Index()-2, lyr.Index()-1
	//m.Info("old_pbase, new_pbase: %v, %v", oldPbase, newPbase)
	m.lvMutex.Lock()
	m.validatedLayer = lyr.Index()
	if err := m.general.Put(VALIDATED, lyr.Index().ToBytes()); err != nil {
		m.Error("could not persist validated layer index %d", lyr.Index())
	}
	m.lvMutex.Unlock()

	for layerId := oldPbase; layerId < newPbase; layerId++ {
		m.AccumulateRewards(layerId, m.config)
		if err := m.PushTransactions(layerId); err != nil {
			m.With().Error("failed to push transactions", log.Err(err))
			break
		}
	}
	m.Info("done validating layer %v", lyr.Index())
}

func (m *Mesh) ExtractUniqueOrderedTransactions(l *types.Layer) (validBlockTxs, invalidBlockTxs []*types.Transaction) {
	// Separate blocks by validity
	var validBlocks, invalidBlocks []*types.Block
	for _, b := range l.Blocks() {
		valid, err := m.ContextualValidity(b.Id())
		if err != nil {
			m.With().Error("could not get contextual validity", log.BlockId(b.Id().String()), log.Err(err))
		}
		if valid {
			validBlocks = append(validBlocks, b)
		} else {
			invalidBlocks = append(invalidBlocks, b)
		}
	}

	// Deterministically sort valid blocks
	validBlocks = types.SortBlocks(validBlocks)

	// `layerHash` is the sha256 sum of sorted layer block IDs
	layerHash := sha256.New()
	for _, b := range validBlocks {
		layerHash.Write(b.Id().AsHash32().Bytes())
	}

	// Initialize a Mersenne Twister seeded with layerHash
	mt := mt19937.New()
	mt.SeedFromSlice(toUint64Slice(layerHash.Sum(nil)))
	rng := rand.New(mt)

	// Perform a Fisher-Yates shuffle on the blocks
	rng.Shuffle(len(validBlocks), func(i, j int) {
		validBlocks[i], validBlocks[j] = validBlocks[j], validBlocks[i]
	})

	// Get and return unique transactions
	seenTxIds := map[types.TransactionId]struct{}{}
	return m.getTxs(uniqueTxIds(validBlocks, seenTxIds), l), m.getTxs(uniqueTxIds(invalidBlocks, seenTxIds), l)
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

func (m *Mesh) PushTransactions(layerId types.LayerID) error {
	l, err := m.GetLayer(layerId)
	if err != nil || l == nil {
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldBase`
		return fmt.Errorf("failed to retrieve layer %v: %v", layerId, err)
	}

	validBlockTxs, invalidBlockTxs := m.ExtractUniqueOrderedTransactions(l)
	numFailedTxs, err := m.ApplyTransactions(layerId, validBlockTxs)
	if err != nil {
		m.With().Error("failed to apply transactions",
			log.LayerId(uint64(layerId)), log.Int("num_failed_txs", numFailedTxs), log.Err(err))
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldBase`
	}
	m.removeFromUnappliedTxs(validBlockTxs, invalidBlockTxs, layerId)
	for _, tx := range invalidBlockTxs {
		err = m.blockBuilder.ValidateAndAddTxToPool(tx)
		// We ignore errors here, since they mean that the tx is no longer valid and we shouldn't re-add it
		if err == nil {
			m.With().Info("transaction from contextually invalid block re-added to mempool",
				log.TxId(tx.Id().ShortString()))
		}
	}
	m.With().Info("applied transactions",
		log.Int("valid_block_txs", len(validBlockTxs)),
		log.Int("invalid_block_txs", len(invalidBlockTxs)),
		log.LayerId(uint64(layerId)),
		log.Int("num_failed_txs", numFailedTxs),
	)
	return nil
}

//todo consider adding a boolean for layer validity instead error
func (m *Mesh) GetVerifiedLayer(i types.LayerID) (*types.Layer, error) {
	m.lMutex.RLock()
	if i > types.LayerID(m.validatedLayer) {
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
	m.With().Info("added block to database ", log.BlockId(blk.Id().String()))
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

func (m *Mesh) GetUnverifiedLayerBlocks(l types.LayerID) ([]types.BlockID, error) {
	x, err := m.layers.Get(l.ToBytes())
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not retrive layer = %d blocks ", l))
	}
	blockIds, err := types.BytesToBlockIds(x)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not desirialize layer to id array for layer %d ", l))
	}
	arr := make([]types.BlockID, 0, len(blockIds))
	for _, bid := range blockIds {
		arr = append(arr, bid)
	}
	return arr, nil
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

	blocks, err := m.GetUnverifiedLayerBlocks(l - 1)
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

func (m *Mesh) AccumulateRewards(rewardLayer types.LayerID, params Config) {
	l, err := m.GetLayer(rewardLayer)
	if err != nil || l == nil {
		m.Error("") //todo handle error
		return
	}

	ids := make([]types.Address, 0, len(l.Blocks()))
	for _, bl := range l.Blocks() {
		valid, err := m.ContextualValidity(bl.Id())
		if err != nil {
			m.With().Error("could not get contextual validity", log.BlockId(bl.Id().String()), log.Err(err))
		}
		if !valid {
			m.With().Info("Withheld reward for contextually invalid block",
				log.BlockId(bl.Id().String()),
				log.LayerId(uint64(rewardLayer)),
			)
			continue
		}
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
	// aggregate all blocks' rewards
	txs, _ := m.ExtractUniqueOrderedTransactions(l)

	totalReward := &big.Int{}
	for _, tx := range txs {
		totalReward.Add(totalReward, new(big.Int).SetUint64(tx.Fee))
	}

	layerReward := CalculateLayerReward(rewardLayer, params)
	totalReward.Add(totalReward, layerReward)

	numBlocks := big.NewInt(int64(len(l.Blocks())))

	blockTotalReward := calculateActualRewards(totalReward, numBlocks)
	m.ApplyRewards(rewardLayer, ids, blockTotalReward)

	blockLayerReward := calculateActualRewards(layerReward, numBlocks)
	err = m.writeTransactionRewards(rewardLayer, ids, blockTotalReward, blockLayerReward)
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
