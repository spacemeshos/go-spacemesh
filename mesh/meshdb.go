package mesh

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/pendingtxs"
)

const (
	layerSize = 200

	layerBlocks  = "b"
	layerBallots = "bL"
)

// DB represents a mesh database instance.
type DB struct {
	log.Log
	blockCache         blockCache
	layers             database.Database
	blocks             database.Database
	ballots            database.Database
	transactions       database.Database
	contextualValidity database.Database
	general            database.Database
	unappliedTxs       database.Database
	hareOutput         database.Database
	blockMutex         sync.RWMutex
	coinflipMu         sync.RWMutex
	coinflips          map[types.LayerID]bool // weak coinflip results from Hare
	lhMutex            sync.Mutex
	exit               chan struct{}
}

// NewPersistentMeshDB creates an instance of a mesh database.
func NewPersistentMeshDB(path string, blockCacheSize int, logger log.Log) (*DB, error) {
	bldb, err := database.NewLDBDatabase(filepath.Join(path, "blocks"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize blocks db: %v", err)
	}
	bdb, err := database.NewLDBDatabase(filepath.Join(path, "ballots"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ballots db: %v", err)
	}
	ldb, err := database.NewLDBDatabase(filepath.Join(path, "layers"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize layers db: %v", err)
	}
	vdb, err := database.NewLDBDatabase(filepath.Join(path, "validity"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize validity db: %v", err)
	}
	tdb, err := database.NewLDBDatabase(filepath.Join(path, "transactions"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize transactions db: %v", err)
	}
	gdb, err := database.NewLDBDatabase(filepath.Join(path, "general"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize general db: %v", err)
	}
	utx, err := database.NewLDBDatabase(filepath.Join(path, "unappliedTxs"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize mesh unappliedTxs db: %v", err)
	}
	ho, err := database.NewLDBDatabase(filepath.Join(path, "hareoutput"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize mesh unappliedTxs db: %v", err)
	}

	mdb := &DB{
		Log:                logger,
		blockCache:         newBlockCache(blockCacheSize * layerSize),
		blocks:             bldb,
		ballots:            bdb,
		layers:             ldb,
		transactions:       tdb,
		general:            gdb,
		contextualValidity: vdb,
		unappliedTxs:       utx,
		hareOutput:         ho,
		coinflips:          make(map[types.LayerID]bool),
		exit:               make(chan struct{}),
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
	if _, err := m.general.Get(constLATEST); err == nil {
		m.Info("found data to recover on disk")
		return true
	}
	m.Info("did not find data to recover on disk")
	return false
}

// NewMemMeshDB is a mock used for testing.
func NewMemMeshDB(logger log.Log) *DB {
	mdb := &DB{
		Log:                logger,
		blockCache:         newBlockCache(100 * layerSize),
		blocks:             database.NewMemDatabase(),
		ballots:            database.NewMemDatabase(),
		layers:             database.NewMemDatabase(),
		general:            database.NewMemDatabase(),
		contextualValidity: database.NewMemDatabase(),
		transactions:       database.NewMemDatabase(),
		unappliedTxs:       database.NewMemDatabase(),
		hareOutput:         database.NewMemDatabase(),
		coinflips:          make(map[types.LayerID]bool),
		exit:               make(chan struct{}),
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
	close(m.exit)
	m.blocks.Close()
	m.ballots.Close()
	m.layers.Close()
	m.transactions.Close()
	m.unappliedTxs.Close()
	m.hareOutput.Close()
	m.general.Close()
	m.contextualValidity.Close()
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
	return m.transactions
}

// HasBallot returns true if the database has Ballot specified by the BallotID and false otherwise.
func (m *DB) HasBallot(id types.BallotID) bool {
	has, err := m.ballots.Has(id.Bytes())
	return err == nil && has
}

// AddBallot adds a ballot to the database.
func (m *DB) AddBallot(b *types.Ballot) error {
	m.blockMutex.Lock()
	defer m.blockMutex.Unlock()
	return m.addBallot(b)
}

func (m *DB) addBallot(b *types.Ballot) error {
	if m.HasBallot(b.ID()) {
		m.With().Warning("ballot already exists", b.ID())
		return nil
	}
	return m.writeBallot(b)
}

// GetBallot returns true if the database has Ballot specified by the BallotID and false otherwise.
func (m *DB) GetBallot(id types.BallotID) (*types.Ballot, error) {
	data, err := m.getBallotBytes(id)
	if err != nil {
		return nil, err
	}
	dbb := &types.DBBallot{}
	if err := codec.Decode(data, dbb); err != nil {
		return nil, fmt.Errorf("parse ballot: %w", err)
	}
	return dbb.ToBallot(), nil
}

// LayerBallotIDs retrieves all BallotID from the layer specified by layer ID.
func (m *DB) LayerBallotIDs(lid types.LayerID) ([]types.BallotID, error) {
	var ids []types.BallotID
	err := LayerIDs(m.layers, layerBallots, lid, func(id []byte) error {
		var bid types.BallotID
		copy(bid[:], id)
		ids = append(ids, bid)
		return nil
	})
	return ids, err
}

// LayerBallots retrieves all ballots from a layer by layer ID.
func (m *DB) LayerBallots(lid types.LayerID) ([]*types.Ballot, error) {
	var ballots []*types.Ballot
	err := LayerIDs(m.layers, layerBallots, lid, func(id []byte) error {
		var bid types.BallotID
		copy(bid[:], id)
		ballot, err := m.GetBallot(bid)
		if err != nil {
			return err
		}
		ballots = append(ballots, ballot)
		return nil
	})
	return ballots, err
}

// HasBlock returns true if the database has Block specified by the BlockID and false otherwise.
func (m *DB) HasBlock(bid types.BlockID) bool {
	has, err := m.blocks.Has(bid.Bytes())
	return err == nil && has
}

// AddBlock adds a block to the database.
func (m *DB) AddBlock(b *types.Block) error {
	m.blockMutex.Lock()
	defer m.blockMutex.Unlock()
	if m.HasBlock(b.ID()) {
		m.With().Warning("block already exists", b.ID())
		return nil
	}
	err := m.writeBlock(b)
	if err != nil {
		return err
	}
	events.ReportNewBlock(b)
	return nil
}

// GetBlock returns the block specified by the block ID.
func (m *DB) GetBlock(bid types.BlockID) (*types.Block, error) {
	if blkh := m.blockCache.Get(bid); blkh != nil {
		return blkh, nil
	}

	b, err := m.getBlock(bid)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// LayerBlockIds retrieves all block IDs from the layer specified by layer ID.
func (m *DB) LayerBlockIds(lid types.LayerID) ([]types.BlockID, error) {
	var ids []types.BlockID
	err := LayerIDs(m.layers, layerBlocks, lid, func(id []byte) error {
		var bid types.BlockID
		copy(bid[:], id)
		ids = append(ids, bid)
		return nil
	})
	return ids, err
}

// LayerBlocks retrieves all blocks from the layer specified by layer ID.
func (m *DB) LayerBlocks(lid types.LayerID) ([]*types.Block, error) {
	var blocks []*types.Block
	err := LayerIDs(m.layers, layerBlocks, lid, func(id []byte) error {
		var bid types.BlockID
		copy(bid[:], id)
		block, err := m.GetBlock(bid)
		if err != nil {
			return err
		}
		blocks = append(blocks, block)
		return nil
	})
	return blocks, err
}

func (m *DB) getBlock(id types.BlockID) (*types.Block, error) {
	data, err := m.getBlockBytes(id)
	if err != nil {
		return nil, err
	}
	dbb := &types.DBBlock{}
	if err := codec.Decode(data, dbb); err != nil {
		return nil, fmt.Errorf("parse block: %w", err)
	}
	return dbb.ToBlock(), nil
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

// AddZeroBallotLayer tags lyr as a layer without ballots.
func (m *DB) AddZeroBallotLayer(lid types.LayerID) error {
	buf := new(bytes.Buffer)
	buf.WriteString(layerBallots)
	buf.Write(lid.Bytes())
	if err := m.layers.Put(buf.Bytes(), nil); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}
	return nil
}

// AddZeroBlockLayer tags lyr as a layer without blocks.
func (m *DB) AddZeroBlockLayer(lid types.LayerID) error {
	buf := new(bytes.Buffer)
	buf.WriteString(layerBlocks)
	buf.Write(lid.Bytes())
	if err := m.layers.Put(buf.Bytes(), nil); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}
	return nil
}

func (m *DB) getBallotBytes(id types.BallotID) ([]byte, error) {
	data, err := m.ballots.Get(id.Bytes())
	if err != nil {
		return data, fmt.Errorf("ballot get from DB: %w", err)
	}

	return data, nil
}

func (m *DB) getBlockBytes(id types.BlockID) ([]byte, error) {
	data, err := m.blocks.Get(id.Bytes())
	if err != nil {
		return data, fmt.Errorf("get from DB: %w", err)
	}

	return data, nil
}

// ContextualValidity retrieves opinion on block from the database.
func (m *DB) ContextualValidity(id types.BlockID) (bool, error) {
	b, err := m.contextualValidity.Get(id.Bytes())
	if err != nil {
		return false, fmt.Errorf("get from DB: %w", err)
	}
	return b[0] == 1, nil // bytes to bool
}

// SaveContextualValidity persists opinion on block to the database.
func (m *DB) SaveContextualValidity(id types.BlockID, lid types.LayerID, valid bool) error {
	var v []byte
	if valid {
		v = constTrue
	} else {
		v = constFalse
	}
	m.With().Debug("save block contextual validity", id, lid, log.Bool("validity", valid))

	if err := m.contextualValidity.Put(id.Bytes(), v); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}

	return nil
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
	coin, exists := m.coinflips[layerID]
	m.coinflipMu.Unlock()

	return coin, exists
}

// GetHareConsensusOutput gets the input vote vector for a layer (hare results).
func (m *DB) GetHareConsensusOutput(layerID types.LayerID) (types.BlockID, error) {
	buf, err := m.hareOutput.Get(layerID.Bytes())
	if err != nil {
		return types.EmptyBlockID, fmt.Errorf("get from DB: %w", err)
	}
	var id types.BlockID
	if err := codec.Decode(buf, &id); err != nil {
		return types.EmptyBlockID, fmt.Errorf("decode with codec: %w", err)
	}
	return id, nil
}

// SaveHareConsensusOutput gets the input vote vector for a layer (hare results).
func (m *DB) SaveHareConsensusOutput(ctx context.Context, id types.LayerID, blockID types.BlockID) error {
	m.WithContext(ctx).With().Debug("saving hare output", id, blockID)
	// NOTE(dshulyak) there is an implicit dependency in fetcher
	if err := m.hareOutput.Put(id.Bytes(), blockID.Bytes()); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}

	return nil
}

func (m *DB) persistProcessedLayer(layerID types.LayerID) error {
	if err := m.general.Put(constPROCESSED, layerID.Bytes()); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}

	return nil
}

// GetProcessedLayer loads processed layer from database.
func (m *DB) GetProcessedLayer() (types.LayerID, error) {
	data, err := m.general.Get(constPROCESSED)
	if err != nil {
		return types.NewLayerID(0), err
	}
	return types.BytesToLayerID(data), nil
}

// GetVerifiedLayer loads verified layer from database.
func (m *DB) GetVerifiedLayer() (types.LayerID, error) {
	data, err := m.general.Get(VERIFIED)
	if err != nil {
		return types.NewLayerID(0), err
	}
	return types.BytesToLayerID(data), nil
}

func (m *DB) writeBallot(b *types.Ballot) error {
	dbb := &types.DBBallot{
		InnerBallot: b.InnerBallot,
		ID:          b.ID(),
		Signature:   b.Signature,
		SmesherID:   b.SmesherID().Bytes(),
	}
	if data, err := codec.Encode(dbb); err != nil {
		return fmt.Errorf("could not encode ballot: %w", err)
	} else if err := m.ballots.Put(b.ID().Bytes(), data); err != nil {
		return fmt.Errorf("could not add ballot %v to database: %w", b.ID(), err)
	} else if err := m.updateLayer(layerBallots, b.LayerIndex, b.ID().Bytes()); err != nil {
		return fmt.Errorf("could not update layer %v with a new ballot %v: %w", b.LayerIndex, b.ID(), err)
	}

	return nil
}

func (m *DB) writeBlock(b *types.Block) error {
	dbb := &types.DBBlock{
		InnerBlock: b.InnerBlock,
		ID:         b.ID(),
	}
	if data, err := codec.Encode(dbb); err != nil {
		return fmt.Errorf("could not encode block: %w", err)
	} else if err := m.blocks.Put(b.ID().Bytes(), data); err != nil {
		return fmt.Errorf("could not add block %v to database: %w", b.ID(), err)
	} else if err := m.updateLayer(layerBlocks, b.LayerIndex, b.ID().Bytes()); err != nil {
		return fmt.Errorf("could not update layer %v with a new block %v: %w", b.LayerIndex, b.ID(), err)
	}

	m.blockCache.put(b)
	return nil
}

func (m *DB) updateLayer(namespace string, lid types.LayerID, id []byte) error {
	var b bytes.Buffer
	b.WriteString(namespace)
	b.Write(lid.Bytes())
	b.Write(id)

	if err := m.layers.Put(b.Bytes(), nil); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}

	return nil
}

func (m *DB) recoverLayerHash(layerID types.LayerID) (types.Hash32, error) {
	h := types.EmptyLayerHash
	bts, err := m.general.Get(getLayerHashKey(layerID))
	if err != nil {
		return types.EmptyLayerHash, fmt.Errorf("get from DB: %w", err)
	}
	h.SetBytes(bts)
	return h, nil
}

func (m *DB) persistLayerHash(layerID types.LayerID, hash types.Hash32) error {
	if err := m.general.Put(getLayerHashKey(layerID), hash.Bytes()); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}

	return nil
}

func (m *DB) persistAggregatedLayerHash(layerID types.LayerID, hash types.Hash32) error {
	if err := m.general.Put(getAggregatedLayerHashKey(layerID), hash.Bytes()); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}

	return nil
}

func getRewardKey(l types.LayerID, account types.Address, smesherID types.NodeID) []byte {
	return new(keyBuilder).
		WithAccountRewardsPrefix().
		WithAddress(account).
		WithNodeID(smesherID).
		WithLayerID(l).
		Bytes()
}

func getRewardKeyPrefix(account types.Address) []byte {
	return new(keyBuilder).
		WithAccountRewardsPrefix().
		WithAddress(account).
		Bytes()
}

// This function gets the reward key for a particular smesherID
// format for the index "s_<smesherid>_<accountid>_<layerid> -> r_<accountid>_<smesherid>_<layerid> -> the actual reward".
func getSmesherRewardKey(l types.LayerID, smesherID types.NodeID, account types.Address) []byte {
	return new(keyBuilder).
		WithSmesherRewardsPrefix().
		WithNodeID(smesherID).
		WithAddress(account).
		WithLayerID(l).
		Bytes()
}

// use r_ for one and s_ for the other so the namespaces can't collide.
func getSmesherRewardKeyPrefix(smesherID types.NodeID) []byte {
	return new(keyBuilder).
		WithSmesherRewardsPrefix().
		WithNodeID(smesherID).Bytes()
}

func getLayerHashKey(layerID types.LayerID) []byte {
	return new(keyBuilder).
		WithLayerHashPrefix().
		WithLayerID(layerID).Bytes()
}

type keyBuilder struct {
	buf bytes.Buffer
}

func parseLayerIDFromRewardsKey(buf []byte) types.LayerID {
	if len(buf) < 4 {
		panic("key that contains layer id must be atleast 4 bytes")
	}
	return types.NewLayerID(binary.BigEndian.Uint32(buf[len(buf)-4:]))
}

func (k *keyBuilder) WithLayerID(l types.LayerID) *keyBuilder {
	buf := make([]byte, 4)
	// NOTE(dshulyak) big endian produces lexicographically ordered bytes.
	// some queries can be optimizaed by using range queries instead of point queries.
	binary.BigEndian.PutUint32(buf, l.Uint32())
	k.buf.Write(buf)
	return k
}

func (k *keyBuilder) WithOriginPrefix() *keyBuilder {
	k.buf.WriteString("to")
	return k
}

func (k *keyBuilder) WithDestPrefix() *keyBuilder {
	k.buf.WriteString("td")
	return k
}

func (k *keyBuilder) WithLayerHashPrefix() *keyBuilder {
	k.buf.WriteString("lh")
	return k
}

func (k *keyBuilder) WithAccountRewardsPrefix() *keyBuilder {
	k.buf.WriteString("ar")
	return k
}

func (k *keyBuilder) WithSmesherRewardsPrefix() *keyBuilder {
	k.buf.WriteString("sr")
	return k
}

func (k *keyBuilder) WithAddress(addr types.Address) *keyBuilder {
	k.buf.Write(addr.Bytes())
	return k
}

func (k *keyBuilder) WithNodeID(id types.NodeID) *keyBuilder {
	k.buf.WriteString(id.Key)
	k.buf.Write(id.VRFPublicKey)
	return k
}

func (k *keyBuilder) WithBytes(buf []byte) *keyBuilder {
	k.buf.Write(buf)
	return k
}

func (k *keyBuilder) Bytes() []byte {
	return k.buf.Bytes()
}

func getTransactionOriginKey(l types.LayerID, t *types.Transaction) []byte {
	return new(keyBuilder).
		WithOriginPrefix().
		WithBytes(t.Origin().Bytes()).
		WithLayerID(l).
		WithBytes(t.ID().Bytes()).
		Bytes()
}

func getTransactionDestKey(l types.LayerID, t *types.Transaction) []byte {
	return new(keyBuilder).
		WithDestPrefix().
		WithBytes(t.GetRecipient().Bytes()).
		WithLayerID(l).
		WithBytes(t.ID().Bytes()).
		Bytes()
}

func getTransactionOriginKeyPrefix(l types.LayerID, account types.Address) []byte {
	return new(keyBuilder).
		WithOriginPrefix().
		WithBytes(account.Bytes()).
		WithLayerID(l).
		Bytes()
}

func getTransactionDestKeyPrefix(l types.LayerID, account types.Address) []byte {
	return new(keyBuilder).
		WithDestPrefix().
		WithBytes(account.Bytes()).
		WithLayerID(l).
		Bytes()
}

// DbTransaction is the transaction type stored in DB.
type DbTransaction struct {
	*types.Transaction
	Origin  types.Address
	BlockID types.BlockID
	LayerID types.LayerID
}

func newDbTransaction(tx *types.Transaction, bid types.BlockID, layerID types.LayerID) *DbTransaction {
	return &DbTransaction{
		Transaction: tx,
		Origin:      tx.Origin(),
		BlockID:     bid,
		LayerID:     layerID,
	}
}

func (t DbTransaction) getTransaction() *types.MeshTransaction {
	t.Transaction.SetOrigin(t.Origin)

	return &types.MeshTransaction{
		Transaction: *t.Transaction,
		LayerID:     t.LayerID,
		BlockID:     t.BlockID,
	}
}

func (m *DB) updateDBTXWithBlockID(b *types.Block, txs ...*types.Transaction) error {
	batch := m.transactions.NewBatch()
	for _, tx := range txs {
		if data, err := codec.Encode(newDbTransaction(tx, b.ID(), b.LayerIndex)); err != nil {
			return fmt.Errorf("could not serialize tx %v: %v", tx.ID().ShortString(), err)
		} else if err := batch.Put(tx.ID().Bytes(), data); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", tx.ID().ShortString(), err)
		}
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to update transactions: %v", err)
	}
	return nil
}

// writeTransactions writes all transactions associated with a block atomically.
func (m *DB) writeTransactions(layerID types.LayerID, bid types.BlockID, txs ...*types.Transaction) error {
	batch := m.transactions.NewBatch()
	for _, t := range txs {
		data, err := codec.Encode(newDbTransaction(t, bid, layerID))
		if err != nil {
			return fmt.Errorf("could not marshall tx %v to bytes: %v", t.ID().ShortString(), err)
		}
		m.Log.With().Debug("storing transaction", t.ID(), log.Int("tx_length", len(data)), log.String("origin", t.Origin().String()))
		if err := batch.Put(t.ID().Bytes(), data); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
		}
		// write extra index for querying txs by account
		if err := batch.Put(getTransactionOriginKey(layerID, t), t.ID().Bytes()); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
		}
		if err := batch.Put(getTransactionDestKey(layerID, t), t.ID().Bytes()); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
		}
		m.Debug("wrote tx %v to db", t.ID().ShortString())
	}
	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transactions: %v", err)
	}
	return nil
}

// We're not using the existing reward type because the layer is implicit in the key.
type dbReward struct {
	TotalReward         uint64
	LayerRewardEstimate uint64
	SmesherID           types.NodeID
	Coinbase            types.Address
	// Amount - LayerReward = FeesEstimate
}

func (m *DB) writeTransactionRewards(l types.LayerID, rewards []types.AnyReward) error {
	type pair struct {
		total uint64
		layer uint64
	}
	smesherIDs := make(map[string]types.NodeID)
	smesherByAddresses := make(map[types.Address]map[string]pair)
	for _, r := range rewards {
		if _, exists := smesherByAddresses[r.Address]; !exists {
			smesherByAddresses[r.Address] = make(map[string]pair)
		}
		p := smesherByAddresses[r.Address][r.SmesherID.String()]
		p.total += r.Amount
		p.layer += r.LayerReward
		smesherByAddresses[r.Address][r.SmesherID.String()] = p
		smesherIDs[r.SmesherID.String()] = r.SmesherID
	}
	batch := m.transactions.NewBatch()
	for addr, smeshers := range smesherByAddresses {
		for smesher, v := range smeshers {
			smesherID := smesherIDs[smesher]
			reward := dbReward{TotalReward: v.total, LayerRewardEstimate: v.layer, SmesherID: smesherID, Coinbase: addr}
			if b, err := codec.Encode(&reward); err != nil {
				return fmt.Errorf("could not marshal reward for %v: %v", addr.Short(), err)
			} else if err := batch.Put(getRewardKey(l, addr, smesherID), b); err != nil {
				return fmt.Errorf("could not write reward to %v to database: %v", addr.Short(), err)
			} else if err := batch.Put(getSmesherRewardKey(l, smesherID, addr), getRewardKey(l, addr, smesherID)); err != nil {
				return fmt.Errorf("could not write reward key for smesherID %v to database: %v", smesherID.ShortString(), err)
			}
		}
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}
	return nil
}

// GetRewards retrieves account's rewards by address.
func (m *DB) GetRewards(account types.Address) (rewards []types.Reward, err error) {
	it := m.transactions.Find(getRewardKeyPrefix(account))
	defer it.Release()
	for it.Next() {
		if it.Key() == nil {
			break
		}
		layer := parseLayerIDFromRewardsKey(it.Key())
		var reward dbReward
		err = codec.Decode(it.Value(), &reward)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal reward: %v", err)
		}
		rewards = append(rewards, types.Reward{
			Layer:               layer,
			TotalReward:         reward.TotalReward,
			LayerRewardEstimate: reward.LayerRewardEstimate,
			SmesherID:           reward.SmesherID,
			Coinbase:            reward.Coinbase,
		})
	}
	err = it.Error()
	return
}

// GetRewardsBySmesherID retrieves rewards by smesherID.
func (m *DB) GetRewardsBySmesherID(smesherID types.NodeID) (rewards []types.Reward, err error) {
	it := m.transactions.Find(getSmesherRewardKeyPrefix(smesherID))
	defer it.Release()
	for it.Next() {
		if it.Key() == nil {
			break
		}
		layer := parseLayerIDFromRewardsKey(it.Key())
		if err != nil {
			return nil, fmt.Errorf("error parsing db key %s: %v", it.Key(), err)
		}
		// find the key to the actual reward struct, which is in it.Value()
		var reward dbReward
		rewardBytes, err := m.transactions.Get(it.Value())
		if err != nil {
			return nil, fmt.Errorf("wrong key in db %s: %v", it.Value(), err)
		}
		if err = codec.Decode(rewardBytes, &reward); err != nil {
			return nil, fmt.Errorf("failed to unmarshal reward: %v", err)
		}
		rewards = append(rewards, types.Reward{
			Layer:               layer,
			TotalReward:         reward.TotalReward,
			LayerRewardEstimate: reward.LayerRewardEstimate,
			SmesherID:           reward.SmesherID,
			Coinbase:            reward.Coinbase,
		})
	}
	err = it.Error()
	return
}

func (m *DB) addToUnappliedTxs(txs []*types.Transaction, layer types.LayerID) error {
	groupedTxs := groupByOrigin(txs)
	for addr, accountTxs := range groupedTxs {
		if err := m.addUnapplied(addr, accountTxs, layer); err != nil {
			return fmt.Errorf("add unapplied: %w", err)
		}
	}
	return nil
}

func (m *DB) addUnapplied(addr types.Address, txs []*types.Transaction, layer types.LayerID) error {
	var (
		batch = m.unappliedTxs.NewBatch()
		b     bytes.Buffer
	)
	for _, tx := range txs {
		mtx := types.MeshTransaction{Transaction: *tx, LayerID: layer}
		buf, err := codec.Encode(mtx)
		if err != nil {
			m.Log.With().Panic("can't encode tx", log.Err(err))
		}

		b.Write(addr.Bytes())
		b.Write(tx.ID().Bytes())
		if err := batch.Put(b.Bytes(), buf); err != nil {
			return fmt.Errorf("put into batch: %w", err)
		}
		b.Reset()
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}

	return nil
}

func (m *DB) removeFromUnappliedTxs(accepted []*types.Transaction) (map[types.Address][]*types.Transaction, map[types.Address]struct{}, error) {
	grouped := groupByOrigin(accepted)
	accounts := make(map[types.Address]struct{})
	for account := range grouped {
		accounts[account] = struct{}{}
	}
	for account := range accounts {
		if err := m.removeFromAccountTxs(account, grouped); err != nil {
			return nil, nil, err
		}
	}
	return grouped, accounts, nil
}

func (m *DB) removeFromAccountTxs(account types.Address, accepted map[types.Address][]*types.Transaction) error {
	return m.removePending(account, accepted[account])
}

func (m *DB) removeRejectedFromAccountTxs(account types.Address, rejected map[types.Address][]*types.Transaction) error {
	return m.removePending(account, rejected[account])
}

func (m *DB) removePending(addr types.Address, txs []*types.Transaction) error {
	var (
		batch = m.unappliedTxs.NewBatch()
		b     bytes.Buffer
	)
	for _, tx := range txs {
		b.Write(addr.Bytes())
		b.Write(tx.ID().Bytes())
		if err := batch.Delete(b.Bytes()); err != nil {
			return fmt.Errorf("delete batch: %w", err)
		}
		b.Reset()
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}

	return nil
}

func (m *DB) getAccountPendingTxs(addr types.Address) (*pendingtxs.AccountPendingTxs, error) {
	var (
		it      = m.unappliedTxs.Find(addr[:])
		pending = pendingtxs.NewAccountPendingTxs()
	)
	defer it.Release()
	for it.Next() {
		var mtx types.MeshTransaction
		if err := codec.Decode(it.Value(), &mtx); err != nil {
			return nil, fmt.Errorf("decode with codec: %w", err)
		}
		pending.Add(mtx.LayerID, &mtx.Transaction)
	}
	if it.Error() != nil {
		return nil, fmt.Errorf("iterator: %w", it.Error())
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
	tBytes, err := m.transactions.Get(id[:])
	if err != nil {
		return nil, fmt.Errorf("could not find transaction in database %v err=%v", hex.EncodeToString(id[:]), err)
	}
	var dbTx DbTransaction
	err = codec.Decode(tBytes, &dbTx)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %v", err)
	}
	return dbTx.getTransaction(), nil
}

// GetTransactionsByDestination retrieves txs by destination and layer.
func (m *DB) GetTransactionsByDestination(l types.LayerID, account types.Address) ([]types.TransactionID, error) {
	var txs []types.TransactionID
	it := m.transactions.Find(getTransactionDestKeyPrefix(l, account))
	defer it.Release()
	for it.Next() {
		if it.Key() == nil {
			break
		}
		var id types.TransactionID
		copy(id[:], it.Value())
		txs = append(txs, id)
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return txs, nil
}

// GetTransactionsByOrigin retrieves txs by origin and layer.
func (m *DB) GetTransactionsByOrigin(l types.LayerID, account types.Address) ([]types.TransactionID, error) {
	var txs []types.TransactionID
	it := m.transactions.Find(getTransactionOriginKeyPrefix(l, account))
	defer it.Release()
	for it.Next() {
		if it.Key() == nil {
			break
		}
		var id types.TransactionID
		copy(id[:], it.Value())
		txs = append(txs, id)
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return txs, nil
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
		select {
		case <-m.exit:
			m.Info("shutdown during cache warm up")
			return nil
		default:
		}

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
