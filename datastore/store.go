package datastore

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

const (
	atxHdrCacheSize = 600
)

// CachedDB is simply a database injected with cache.
type CachedDB struct {
	*sql.Database
	logger      log.Log
	atxHdrCache AtxCache
}

// NewCachedDB create an instance of a CachedDB.
func NewCachedDB(db *sql.Database, lg log.Log) *CachedDB {
	return &CachedDB{
		Database:    db,
		logger:      lg,
		atxHdrCache: NewAtxCache(atxHdrCacheSize),
	}
}

// GetAtxHeader returns the ATX header by the given ID. This function is thread safe and will return an error if the ID
// is not found in the ATX DB.
func (db *CachedDB) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if id == *types.EmptyATXID {
		return nil, errors.New("trying to fetch empty atx id")
	}

	if atxHeader, gotIt := db.atxHdrCache.Get(id); gotIt {
		return atxHeader, nil
	}

	return db.getAndCacheHeader(id)
}

// GetFullAtx returns the full atx struct of the given atxId id, it returns an error if the full atx cannot be found
// in all databases.
func (db *CachedDB) GetFullAtx(id types.ATXID) (*types.VerifiedActivationTx, error) {
	if id == *types.EmptyATXID {
		return nil, errors.New("trying to fetch empty atx id")
	}

	atx, err := atxs.Get(db, id)
	if err != nil {
		return nil, fmt.Errorf("get ATXs from DB: %w", err)
	}

	db.atxHdrCache.Add(id, getHeader(atx))
	return atx, nil
}

// getAndCacheHeader fetches the full atx struct from the database, caches it and returns the cached header.
func (db *CachedDB) getAndCacheHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	_, err := db.GetFullAtx(id)
	if err != nil {
		return nil, err
	}

	atxHeader, gotIt := db.atxHdrCache.Get(id)
	if !gotIt {
		return nil, fmt.Errorf("inconsistent state: failed to get atx header: %v", err)
	}

	return atxHeader, nil
}

// GetEpochWeight returns the total weight of ATXs targeting the given epochID.
func (db *CachedDB) GetEpochWeight(epoch types.EpochID) (uint64, []types.ATXID, error) {
	var (
		weight uint64
		ids    []types.ATXID
	)
	if err := db.IterateEpochATXHeaders(epoch, func(header *types.ActivationTxHeader) bool {
		weight += header.GetWeight()
		ids = append(ids, header.ID)
		return true
	}); err != nil {
		return 0, nil, err
	}
	return weight, ids, nil
}

// IterateEpochATXHeaders iterates over ActivationTxs that target an epoch.
func (db *CachedDB) IterateEpochATXHeaders(epoch types.EpochID, iter func(*types.ActivationTxHeader) bool) error {
	ids, err := atxs.GetIDsByEpoch(db, epoch-1)
	if err != nil {
		return err
	}
	for _, id := range ids {
		header, err := db.GetAtxHeader(id)
		if err != nil {
			return err
		}
		if !iter(header) {
			return nil
		}
	}
	return nil
}

// GetPrevAtx gets the last atx header of specified node Id, it returns error if no previous atx found or if no
// AtxHeader struct in db.
func (db *CachedDB) GetPrevAtx(nodeID types.NodeID) (*types.ActivationTxHeader, error) {
	if atxid, err := atxs.GetLastIDByNodeID(db, nodeID); err != nil {
		return nil, fmt.Errorf("no prev atx found: %v", err)
	} else if atx, err := db.GetAtxHeader(atxid); err != nil {
		return nil, fmt.Errorf("inconsistent state: failed to get atx header: %v", err)
	} else {
		return atx, nil
	}
}

// Hint marks which DB should be queried for a certain provided hash.
type Hint string

// DB hints per DB.
const (
	BallotDB   Hint = "ballotDB"
	BlockDB    Hint = "blocksDB"
	ProposalDB Hint = "proposalDB"
	ATXDB      Hint = "ATXDB"
	TXDB       Hint = "TXDB"
	POETDB     Hint = "POETDB"
)

// NewBlobStore returns a BlobStore.
func NewBlobStore(db *sql.Database) *BlobStore {
	return &BlobStore{DB: db}
}

// BlobStore gets data as a blob to serve direct fetch requests.
type BlobStore struct {
	DB *sql.Database
}

// Get gets an ATX as bytes by an ATX ID as bytes.
func (bs *BlobStore) Get(hint Hint, key []byte) ([]byte, error) {
	switch hint {
	case ATXDB:
		return atxs.GetBlob(bs.DB, key)
	case ProposalDB:
		return proposals.GetBlob(bs.DB, key)
	case BallotDB:
		id := types.BallotID(types.BytesToHash(key).ToHash20())
		blt, err := ballots.Get(bs.DB, id)
		if err != nil {
			return nil, fmt.Errorf("get ballot blob: %w", err)
		}
		data, err := codec.Encode(blt)
		if err != nil {
			return data, fmt.Errorf("serialize: %w", err)
		}
		return data, nil
	case BlockDB:
		id := types.BlockID(types.BytesToHash(key).ToHash20())
		blk, err := blocks.Get(bs.DB, id)
		if err != nil {
			return nil, fmt.Errorf("get block: %w", err)
		}
		data, err := codec.Encode(blk)
		if err != nil {
			return data, fmt.Errorf("serialize: %w", err)
		}
		return data, nil
	case TXDB:
		return transactions.GetBlob(bs.DB, key)
	case POETDB:
		return poets.Get(bs.DB, key)
	}
	return nil, fmt.Errorf("blob store not found %s", hint)
}

func getHeader(vatx *types.VerifiedActivationTx) *types.ActivationTxHeader {
	return &types.ActivationTxHeader{
		NIPostChallenge: vatx.NIPostChallenge,
		Coinbase:        vatx.Coinbase,
		NumUnits:        vatx.NumUnits,
		VRFNonce:        vatx.VRFNonce,

		ID:     vatx.ID(),
		NodeID: vatx.NodeID(),

		BaseTickHeight: vatx.BaseTickHeight(),
		TickCount:      vatx.TickCount(),
	}
}
