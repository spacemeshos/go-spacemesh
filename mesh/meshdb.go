package mesh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
)

// DB represents a mesh database instance.
type DB struct {
	log.Log

	db *sql.Database

	coinflipMu sync.RWMutex
	coinflips  map[types.LayerID]bool // weak coinflip results from Hare
}

// NewPersistentMeshDB creates an instance of a mesh database.
func NewPersistentMeshDB(db *sql.Database, logger log.Log) (*DB, error) {
	mdb := &DB{
		Log:       logger,
		db:        db,
		coinflips: make(map[types.LayerID]bool),
	}
	gLayer := types.GenesisLayer()
	for _, b := range gLayer.Ballots() {
		mdb.Log.With().Info("adding genesis ballot", b.ID(), b.LayerIndex)
		if err := mdb.AddBallot(b); err != nil {
			mdb.Log.With().Error("error inserting genesis ballot to db", b.ID(), b.LayerIndex, log.Err(err))
		}
	}
	for _, b := range gLayer.Blocks() {
		mdb.Log.With().Info("adding genesis block", b.ID(), b.LayerIndex)
		if err := mdb.AddBlock(b); err != nil {
			mdb.Log.With().Error("error inserting genesis block to db", b.ID(), b.LayerIndex, log.Err(err))
		}
		if err := mdb.SaveContextualValidity(b.ID(), b.LayerIndex, true); err != nil {
			mdb.Log.With().Error("error inserting genesis block to db", b.ID(), b.LayerIndex, log.Err(err))
		}
	}
	if err := mdb.SaveHareConsensusOutput(context.Background(), gLayer.Index(), types.GenesisBlockID); err != nil {
		log.With().Error("error inserting genesis block as hare output to db", gLayer.Index(), log.Err(err))
	}
	return mdb, nil
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
		Log:       logger,
		db:        sql.InMemory(),
		coinflips: make(map[types.LayerID]bool),
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
	return nil
}

// HasBlock returns true if block exists in the database.
func (m *DB) HasBlock(bid types.BlockID) bool {
	exists, _ := blocks.Has(m.db, bid)
	return exists
}

// GetBlock returns the block specified by the block ID.
func (m *DB) GetBlock(bid types.BlockID) (*types.Block, error) {
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

// LayerContextualValidity returns tuples with block id and contextual validity for all blocks in the layer.
func (m *DB) LayerContextualValidity(lid types.LayerID) ([]types.BlockContextualValidity, error) {
	return blocks.ContextualValidity(m.db, lid)
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
