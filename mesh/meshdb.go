package mesh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/pendingtxs"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

const (
	layerSize = 200
)

// DB represents a mesh database instance.
type DB struct {
	log.Log
	blockCache blockCache

	db *sql.Database

	coinflipMu sync.RWMutex
	coinflips  map[types.LayerID]bool // weak coinflip results from Hare
}

// NewPersistentMeshDB creates an instance of a mesh database.
func NewPersistentMeshDB(path string, blockCacheSize int, logger log.Log) (*DB, error) {
	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", path, err)
	}
	db, err := sql.Open("file:" + filepath.Join(path, "state.sql"))
	if err != nil {
		return nil, fmt.Errorf("open sqlite db %w", err)
	}

	mdb := &DB{
		Log:        logger,
		blockCache: newBlockCache(blockCacheSize * layerSize),
		db:         db,
		coinflips:  make(map[types.LayerID]bool),
	}
	gLayer := types.GenesisLayer()
	for _, b := range gLayer.Ballots() {
		mdb.Log.With().Info("adding genesis ballot", b.ID(), b.LayerIndex)
		if err = mdb.AddBallot(b); err != nil {
			mdb.Log.With().Error("error inserting genesis ballot to db", b.ID(), b.LayerIndex, log.Err(err))
		}
	}
	for _, b := range gLayer.Blocks() {
		mdb.Log.With().Info("adding genesis block", b.ID(), b.LayerIndex)
		if err = mdb.AddBlock(b); err != nil {
			mdb.Log.With().Error("error inserting genesis block to db", b.ID(), b.LayerIndex, log.Err(err))
		}
		if err = mdb.SaveContextualValidity(b.ID(), b.LayerIndex, true); err != nil {
			mdb.Log.With().Error("error inserting genesis block to db", b.ID(), b.LayerIndex, log.Err(err))
		}
	}
	if err = mdb.SaveHareConsensusOutput(context.Background(), gLayer.Index(), types.GenesisBlockID); err != nil {
		log.With().Error("error inserting genesis block as hare output to db", gLayer.Index(), log.Err(err))
	}
	return mdb, err
}

// PersistentData checks to see if db is empty.
func (m *DB) PersistentData() bool {
	lid, err := layers.GetByStatus(m.db, layers.Latest)
	if err != nil && errors.Is(err, sql.ErrNotFound) || (lid == types.LayerID{}) {
		m.Info("database is empty")
		return false
	}
	return true
}

// NewMemMeshDB is a mock used for testing.
func NewMemMeshDB(logger log.Log) *DB {
	mdb := &DB{
		Log:        logger,
		blockCache: newBlockCache(100 * layerSize),
		db:         sql.InMemory(),
		coinflips:  make(map[types.LayerID]bool),
	}
	gLayer := types.GenesisLayer()
	for _, b := range gLayer.Ballots() {
		mdb.Log.With().Info("adding genesis ballot", b.ID(), b.LayerIndex)
		_ = mdb.AddBallot(b)
	}
	for _, b := range gLayer.Blocks() {
		// these are only used for testing so we can safely ignore errors here
		mdb.Log.With().Info("adding genesis block", b.ID(), b.LayerIndex)
		_ = mdb.AddBlock(b)
		_ = mdb.SaveContextualValidity(b.ID(), b.LayerIndex, true)
	}
	if err := mdb.SaveHareConsensusOutput(context.Background(), gLayer.Index(), types.GenesisBlockID); err != nil {
		logger.With().Error("error inserting genesis block as hare output to db", gLayer, log.Err(err))
	}
	return mdb
}

// Close closes all resources.
func (m *DB) Close() {
	if err := m.db.Close(); err != nil {
		m.Log.With().Error("error closing database", log.Err(err))
	}
}

// todo: for now, these methods are used to export dbs to sync, think about merging the two packages

// Ballots exports the ballot database.
func (m *DB) Ballots() database.Getter {
	return newBallotFetcherDB(m)
}

// Blocks exports the block database.
func (m *DB) Blocks() database.Getter {
	return newBlockFetcherDB(m)
}

// Transactions exports the transactions DB.
func (m *DB) Transactions() database.Getter {
	return &txFetcher{mdb: m}
}

// Beacons exports the beacon database.
func (m *DB) Beacons() database.Store {
	return &beaconDB{mdb: m}
}

// AddBallot adds a ballot to the database.
func (m *DB) AddBallot(b *types.Ballot) error {
	mal, err := identities.IsMalicious(m.db, b.SmesherID().Bytes())
	if err != nil {
		return err
	}
	if mal {
		b.SetMalicious()
	}

	// it is important to run add ballot and set identity to malicious atomically
	tx, err := m.db.Tx(context.Background())
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
			m.Log.With().Warning("smesher produced more than one ballot in the same layer",
				log.Stringer("smesher", b.SmesherID()),
				log.Inline(b),
			)
		}
	}
	return tx.Commit()
}

// SetMalicious updates smesher as malicious.
func (m *DB) SetMalicious(smesher *signing.PublicKey) error {
	return identities.SetMalicious(m.db, smesher.Bytes())
}

// HasBallot returns true if the ballot is stored in a database.
func (m *DB) HasBallot(ballot types.BallotID) bool {
	exists, _ := ballots.Has(m.db, ballot)
	return exists
}

// GetBallot returns true if the database has Ballot specified by the BallotID and false otherwise.
func (m *DB) GetBallot(id types.BallotID) (*types.Ballot, error) {
	return ballots.Get(m.db, id)
}

// LayerBallots retrieves all ballots from a layer by layer ID.
func (m *DB) LayerBallots(lid types.LayerID) ([]*types.Ballot, error) {
	return ballots.Layer(m.db, lid)
}

// LayerBallotIDs returns list of the ballot ids in the layer.
func (m *DB) LayerBallotIDs(lid types.LayerID) ([]types.BallotID, error) {
	return ballots.IDsInLayer(m.db, lid)
}

// AddBlock adds a block to the database.
func (m *DB) AddBlock(b *types.Block) error {
	err := m.writeBlock(b)
	if err != nil {
		if errors.Is(err, sql.ErrObjectExists) {
			return nil
		}
		return err
	}
	events.ReportNewBlock(b)
	return nil
}

// HasBlock returns true if block exists in the database.
func (m *DB) HasBlock(bid types.BlockID) bool {
	exists, _ := blocks.Has(m.db, bid)
	return exists
}

// GetBlock returns the block specified by the block ID.
func (m *DB) GetBlock(bid types.BlockID) (*types.Block, error) {
	if b := m.blockCache.Get(bid); b != nil {
		return b, nil
	}
	b, err := m.getBlock(bid)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// LayerBlockIds retrieves all block IDs from the layer specified by layer ID.
func (m *DB) LayerBlockIds(lid types.LayerID) ([]types.BlockID, error) {
	return blocks.IDsInLayer(m.db, lid)
}

// LayerBlocks retrieves all blocks from the layer specified by layer ID.
func (m *DB) LayerBlocks(lid types.LayerID) ([]*types.Block, error) {
	ids, err := m.LayerBlockIds(lid)
	if err != nil {
		return nil, err
	}
	rst := make([]*types.Block, 0, len(ids))
	for _, bid := range ids {
		block, err := m.GetBlock(bid)
		if err != nil {
			return nil, err
		}
		rst = append(rst, block)
	}
	return rst, nil
}

func (m *DB) getBlock(id types.BlockID) (*types.Block, error) {
	return blocks.Get(m.db, id)
}

// AddZeroBlockLayer tags lyr as a layer without blocks.
func (m *DB) AddZeroBlockLayer(lid types.LayerID) error {
	return layers.SetHareOutput(m.db, lid, types.BlockID{})
}

// ContextualValidity retrieves opinion on block from the database.
func (m *DB) ContextualValidity(id types.BlockID) (bool, error) {
	return blocks.IsValid(m.db, id)
}

// SaveContextualValidity persists opinion on block to the database.
func (m *DB) SaveContextualValidity(id types.BlockID, lid types.LayerID, valid bool) error {
	m.With().Debug("save block contextual validity", id, lid, log.Bool("validity", valid))
	if valid {
		return blocks.SetValid(m.db, id)
	}
	return blocks.SetInvalid(m.db, id)
}

// RecordCoinflip saves the weak coinflip result to memory for the given layer.
func (m *DB) RecordCoinflip(ctx context.Context, layerID types.LayerID, coinflip bool) {
	m.WithContext(ctx).With().Info("recording coinflip result for layer in mesh",
		layerID,
		log.Bool("coinflip", coinflip))

	m.coinflipMu.Lock()
	defer m.coinflipMu.Unlock()

	m.coinflips[layerID] = coinflip
}

// GetCoinflip returns the weak coinflip result for the given layer.
func (m *DB) GetCoinflip(_ context.Context, layerID types.LayerID) (bool, bool) {
	m.coinflipMu.Lock()
	defer m.coinflipMu.Unlock()
	coin, exists := m.coinflips[layerID]
	return coin, exists
}

// GetHareConsensusOutput gets the input vote vector for a layer (hare results).
func (m *DB) GetHareConsensusOutput(layerID types.LayerID) (types.BlockID, error) {
	return layers.GetHareOutput(m.db, layerID)
}

// SaveHareConsensusOutput gets the input vote vector for a layer (hare results).
func (m *DB) SaveHareConsensusOutput(ctx context.Context, id types.LayerID, blockID types.BlockID) error {
	m.WithContext(ctx).With().Debug("saving hare output", id, blockID)
	return layers.SetHareOutput(m.db, id, blockID)
}

func (m *DB) persistProcessedLayer(layerID types.LayerID) error {
	return layers.SetStatus(m.db, layerID, layers.Processed)
}

// GetProcessedLayer loads processed layer from database.
func (m *DB) GetProcessedLayer() (types.LayerID, error) {
	return layers.GetByStatus(m.db, layers.Processed)
}

// GetVerifiedLayer loads verified layer from database.
func (m *DB) GetVerifiedLayer() (types.LayerID, error) {
	return layers.GetByStatus(m.db, layers.Applied)
}

// GetLatestLayer loads latest layer from database.
func (m *DB) GetLatestLayer() (types.LayerID, error) {
	return layers.GetByStatus(m.db, layers.Latest)
}

func (m *DB) writeBlock(b *types.Block) error {
	if err := blocks.Add(m.db, b); err != nil {
		return fmt.Errorf("could not add block %v to database: %w", b.ID(), err)
	}
	m.blockCache.put(b)
	return nil
}

func (m *DB) recoverLayerHash(layerID types.LayerID) (types.Hash32, error) {
	return layers.GetHash(m.db, layerID)
}

func (m *DB) persistLayerHash(layerID types.LayerID, hash types.Hash32) error {
	return layers.SetHash(m.db, layerID, hash)
}

func (m *DB) persistAggregatedLayerHash(layerID types.LayerID, hash types.Hash32) error {
	return layers.SetAggregatedHash(m.db, layerID, hash)
}

func (m *DB) updateDBTXWithBlockID(b *types.Block, txs ...*types.Transaction) error {
	return m.writeTransactions(b.LayerIndex, b.ID(), txs...)
}

// writeTransactions writes all transactions associated with a block atomically.
func (m *DB) writeTransactions(layerID types.LayerID, bid types.BlockID, txs ...*types.Transaction) error {
	dbtx, err := m.db.Tx(context.Background())
	if err != nil {
		return err
	}
	defer dbtx.Release()
	for _, tx := range txs {
		if err := transactions.Add(dbtx, layerID, bid, tx); err != nil {
			return err
		}
		m.Debug("wrote tx %v to db", tx.ID().ShortString())
	}
	return dbtx.Commit()
}

func (m *DB) writeTransactionRewards(l types.LayerID, applied []types.AnyReward) error {
	tx, err := m.db.Tx(context.Background())
	if err != nil {
		return err
	}
	defer tx.Release()
	for i := range applied {
		if err := rewards.Add(tx, l, &applied[i]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetRewards retrieves account's rewards by the coinbase address.
func (m *DB) GetRewards(coinbase types.Address) ([]types.Reward, error) {
	return rewards.FilterByCoinbase(m.db, coinbase)
}

// GetRewardsBySmesherID retrieves rewards by smesherID.
func (m *DB) GetRewardsBySmesherID(smesherID types.NodeID) ([]types.Reward, error) {
	return rewards.FilterBySmesher(m.db, smesherID.ToBytes())
}

func (m *DB) markTransactionsDeleted(txs ...*types.Transaction) error {
	for _, tx := range txs {
		if err := transactions.MarkDeleted(m.db, tx.ID()); err != nil {
			return err
		}
	}
	return nil
}

func (m *DB) getAccountPendingTxs(addr types.Address) (*pendingtxs.AccountPendingTxs, error) {
	pending := pendingtxs.NewAccountPendingTxs()
	txs, err := transactions.FilterPending(m.db, addr)
	if err != nil {
		return nil, err
	}
	for _, tx := range txs {
		pending.Add(tx.LayerID, &tx.Transaction)
	}
	return pending, nil
}

func groupByOrigin(txs []*types.Transaction) map[types.Address][]*types.Transaction {
	grouped := make(map[types.Address][]*types.Transaction)
	for _, tx := range txs {
		grouped[tx.Origin()] = append(grouped[tx.Origin()], tx)
	}
	return grouped
}

// GetProjection returns projection of address.
func (m *DB) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (uint64, uint64, error) {
	pending, err := m.getAccountPendingTxs(addr)
	if err != nil {
		return 0, 0, err
	}
	nonce, balance := pending.GetProjection(prevNonce, prevBalance)
	return nonce, balance, nil
}

// GetTransactions retrieves a list of txs by their id's.
func (m *DB) GetTransactions(transactions []types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
	missing := make(map[types.TransactionID]struct{})
	txs := make([]*types.Transaction, 0, len(transactions))
	for _, id := range transactions {
		if tx, err := m.GetMeshTransaction(id); err != nil {
			m.With().Warning("could not fetch tx", id, log.Err(err))
			missing[id] = struct{}{}
		} else {
			txs = append(txs, &tx.Transaction)
		}
	}
	return txs, missing
}

// GetMeshTransactions retrieves list of txs with information in what blocks and layers they are included.
func (m *DB) GetMeshTransactions(transactions []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
	var (
		missing = map[types.TransactionID]struct{}{}
		txs     = make([]*types.MeshTransaction, 0, len(transactions))
	)
	for _, id := range transactions {
		tx, err := m.GetMeshTransaction(id)
		if err != nil {
			m.With().Warning("could not fetch tx", id, log.Err(err))
			missing[id] = struct{}{}
		} else {
			txs = append(txs, tx)
		}
	}
	return txs, missing
}

// GetMeshTransaction retrieves a tx by its id.
func (m *DB) GetMeshTransaction(id types.TransactionID) (*types.MeshTransaction, error) {
	return transactions.Get(m.db, id)
}

// GetTransactionsByDestination retrieves txs by destination in between layers [from, to].
func (m *DB) GetTransactionsByDestination(from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return transactions.FilterByDestination(m.db, from, to, address)
}

// GetTransactionsByOrigin retrieves txs by origin in beetween layers [from, to].
func (m *DB) GetTransactionsByOrigin(from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return transactions.FilterByOrigin(m.db, from, to, address)
}

// GetTransactionsByAddress retrieves txs for a single address in beetween layers [from, to].
// Guarantees that transaction will appear exactly once, even if origin and recipient is the same, and in insertion order.
func (m *DB) GetTransactionsByAddress(from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return transactions.FilterByAddress(m.db, from, to, address)
}

// BlocksByValidity classifies a slice of blocks by validity.
func (m *DB) BlocksByValidity(blocks []*types.Block) ([]*types.Block, []*types.Block) {
	var validBlocks, invalidBlocks []*types.Block
	for _, b := range blocks {
		valid, err := m.ContextualValidity(b.ID())
		if err != nil {
			m.With().Warning("could not get contextual validity for block in list", b.ID(), log.Err(err))
		}
		if valid {
			validBlocks = append(validBlocks, b)
		} else {
			invalidBlocks = append(invalidBlocks, b)
		}
	}
	return validBlocks, invalidBlocks
}

// LayerContextuallyValidBlocks returns the set of contextually valid block IDs for the provided layer.
func (m *DB) LayerContextuallyValidBlocks(ctx context.Context, layer types.LayerID) (map[types.BlockID]struct{}, error) {
	logger := m.WithContext(ctx)
	blockIds, err := m.LayerBlockIds(layer)
	if err != nil {
		return nil, err
	}

	validBlks := make(map[types.BlockID]struct{})

	cvErrors := make(map[string][]types.BlockID)
	cvErrorCount := 0

	for _, b := range blockIds {
		valid, err := m.ContextualValidity(b)
		if err != nil {
			logger.With().Warning("could not get contextual validity for block in layer", b, layer, log.Err(err))
			cvErrors[err.Error()] = append(cvErrors[err.Error()], b)
			cvErrorCount++
		}

		if !valid {
			continue
		}

		validBlks[b] = struct{}{}
	}

	logger.With().Debug("count of contextually valid blocks in layer",
		layer,
		log.Int("count_valid", len(validBlks)),
		log.Int("count_error", cvErrorCount),
		log.Int("count_total", len(blockIds)))
	if cvErrorCount != 0 {
		logger.With().Error("errors occurred getting contextual validity of blocks in layer",
			layer,
			log.Int("count", cvErrorCount),
			log.String("errors", fmt.Sprint(cvErrors)))
	}
	return validBlks, nil
}

func (m *DB) cacheWarmUpFromTo(from types.LayerID, to types.LayerID) error {
	m.Info("warming up cache with layers %v to %v", from, to)
	for i := from; i.Before(to); i = i.Add(1) {
		layer, err := m.LayerBlockIds(i)
		if err != nil {
			return fmt.Errorf("could not get layer %v from database %v", layer, err)
		}

		for _, b := range layer {
			block, blockErr := m.GetBlock(b)
			if blockErr != nil {
				return fmt.Errorf("could not get bl %v from database %v", b, blockErr)
			}
			m.blockCache.put(block)
		}
	}
	m.Info("done warming up cache")

	return nil
}

// newBlockFetcherDB returns reference to a BlockFetcherDB instance.
func newBlockFetcherDB(mdb *DB) *BlockFetcherDB {
	return &BlockFetcherDB{mdb: mdb}
}

// BlockFetcherDB implements API that allows fetcher to get a block from a remote database.
type BlockFetcherDB struct {
	mdb *DB
}

// Get types.Block encoded in byte using hash.
func (db *BlockFetcherDB) Get(hash []byte) ([]byte, error) {
	id := types.BlockID(types.BytesToHash(hash).ToHash20())
	blk, err := db.mdb.GetBlock(id)
	if err != nil {
		return nil, fmt.Errorf("get block: %w", err)
	}

	data, err := codec.Encode(blk)
	if err != nil {
		return data, fmt.Errorf("serialize: %w", err)
	}

	return data, nil
}

// newBallotFetcherDB returns reference to a BallotFetcherDB instance.
func newBallotFetcherDB(mdb *DB) *BallotFetcherDB {
	return &BallotFetcherDB{mdb: mdb}
}

// BallotFetcherDB implements API that allows fetcher to get a ballot from a remote database.
type BallotFetcherDB struct {
	mdb *DB
}

// Get types.Block encoded in byte using hash.
func (db *BallotFetcherDB) Get(hash []byte) ([]byte, error) {
	id := types.BallotID(types.BytesToHash(hash).ToHash20())
	blk, err := db.mdb.GetBallot(id)
	if err != nil {
		return nil, fmt.Errorf("get ballot: %w", err)
	}

	data, err := codec.Encode(blk)
	if err != nil {
		return data, fmt.Errorf("serialize: %w", err)
	}

	return data, nil
}

type txFetcher struct {
	mdb *DB
}

// Get transaction blob, by transaction id.
func (db *txFetcher) Get(hash []byte) ([]byte, error) {
	id := types.TransactionID{}
	copy(id[:], hash)
	return transactions.GetBlob(db.mdb.db, id)
}

// LayerIDs is a utility function that finds namespaced IDs saved in a database as a key.
func LayerIDs(db database.Database, namespace string, lid types.LayerID, f func(id []byte) error) error {
	var (
		zero = false
		n    int
		buf  = new(bytes.Buffer)
	)
	buf.WriteString(namespace)
	buf.Write(lid.Bytes())

	it := db.Find(buf.Bytes())
	defer it.Release()
	for it.Next() {
		if len(it.Key()) == buf.Len() {
			zero = true
			continue
		}
		if err := f(it.Key()[buf.Len():]); err != nil {
			return err
		}
		n++
	}
	if it.Error() != nil {
		return fmt.Errorf("iterator: %w", it.Error())
	}
	if zero || n > 0 {
		return nil
	}
	return database.ErrNotFound
}

type beaconDB struct {
	mdb *DB
}

// Get gets beacon by epoch ID.
func (db *beaconDB) Get(epoch []byte) ([]byte, error) {
	beacon, err := beacons.Get(db.mdb.db, types.EpochID(util.BytesToUint32(epoch)))
	if err != nil {
		return nil, err
	}

	return beacon.Bytes(), err
}

// Put puts beacon by epoch ID.
func (db *beaconDB) Put(epoch []byte, beacon []byte) error {
	return beacons.Add(db.mdb.db, types.EpochID(util.BytesToUint32(epoch)), types.BytesToBeacon(beacon))
}
