package datastore

import (
	"context"
	"errors"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

var ErrNotFound = errors.New("not found")

type VrfNonceKey struct {
	ID    types.NodeID
	Epoch types.EpochID
}

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./store.go

type Executor interface {
	sql.Executor
	WithTx(context.Context, func(*sql.Tx) error) error
	QueryCache() sql.QueryCache
}

// CachedDB is simply a database injected with cache.
type CachedDB struct {
	Executor
	sql.QueryCache
	logger log.Log

	// cache is optional
	atxsdata *atxsdata.Data

	atxHdrCache   *lru.Cache[types.ATXID, *types.ActivationTxHeader]
	vrfNonceCache *lru.Cache[VrfNonceKey, *types.VRFPostIndex]

	// used to coordinate db update and cache
	mu               sync.Mutex
	malfeasanceCache *lru.Cache[types.NodeID, *types.MalfeasanceProof]
}

type Config struct {
	// ATXSize must be larger than the sum of all ATXs in last 2 epochs to be effective
	ATXSize         int `mapstructure:"atx-size"`
	MalfeasanceSize int `mapstructure:"malfeasance-size"`
}

func DefaultConfig() Config {
	return Config{
		ATXSize:         4_400_000, // to be in line with 2*`EpochData` size (see fetch/wire_types.go) - see comment above
		MalfeasanceSize: 1_000,
	}
}

type cacheOpts struct {
	cfg      Config
	atxsdata *atxsdata.Data
}

type Opt func(*cacheOpts)

func WithConfig(cfg Config) Opt {
	return func(o *cacheOpts) {
		o.cfg = cfg
	}
}

func WithConsensusCache(c *atxsdata.Data) Opt {
	return func(o *cacheOpts) {
		o.atxsdata = c
	}
}

// NewCachedDB create an instance of a CachedDB.
func NewCachedDB(db Executor, lg log.Log, opts ...Opt) *CachedDB {
	o := cacheOpts{cfg: DefaultConfig()}
	for _, opt := range opts {
		opt(&o)
	}
	lg.With().Info("initialized datastore", log.Any("config", o.cfg))

	atxHdrCache, err := lru.New[types.ATXID, *types.ActivationTxHeader](o.cfg.ATXSize)
	if err != nil {
		lg.Fatal("failed to create atx cache", err)
	}

	malfeasanceCache, err := lru.New[types.NodeID, *types.MalfeasanceProof](o.cfg.MalfeasanceSize)
	if err != nil {
		lg.Fatal("failed to create malfeasance cache", err)
	}

	vrfNonceCache, err := lru.New[VrfNonceKey, *types.VRFPostIndex](o.cfg.ATXSize)
	if err != nil {
		lg.Fatal("failed to create vrf nonce cache", err)
	}

	return &CachedDB{
		Executor:         db,
		QueryCache:       db.QueryCache(),
		atxsdata:         o.atxsdata,
		logger:           lg,
		atxHdrCache:      atxHdrCache,
		malfeasanceCache: malfeasanceCache,
		vrfNonceCache:    vrfNonceCache,
	}
}

func (db *CachedDB) MalfeasanceCacheSize() int {
	return db.malfeasanceCache.Len()
}

// IsMalicious returns true if the NodeID is malicious.
func (db *CachedDB) IsMalicious(id types.NodeID) (bool, error) {
	if id == types.EmptyNodeID {
		db.logger.Fatal("invalid argument to IsMalicious")
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if proof, ok := db.malfeasanceCache.Get(id); ok {
		if proof == nil {
			return false, nil
		} else {
			return true, nil
		}
	}

	bad, err := identities.IsMalicious(db, id)
	if err != nil {
		return false, err
	}
	if !bad {
		db.malfeasanceCache.Add(id, nil)
	}
	return bad, nil
}

// GetMalfeasanceProof gets the malfeasance proof associated with the NodeID.
func (db *CachedDB) GetMalfeasanceProof(id types.NodeID) (*types.MalfeasanceProof, error) {
	if id == types.EmptyNodeID {
		db.logger.Fatal("invalid argument to GetMalfeasanceProof")
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if proof, ok := db.malfeasanceCache.Get(id); ok {
		if proof == nil {
			return nil, sql.ErrNotFound
		}
		return proof, nil
	}

	proof, err := identities.GetMalfeasanceProof(db.Executor, id)
	if err != nil && err != sql.ErrNotFound {
		return nil, err
	}
	db.malfeasanceCache.Add(id, proof)
	return proof, err
}

func (db *CachedDB) CacheMalfeasanceProof(id types.NodeID, proof *types.MalfeasanceProof) {
	if id == types.EmptyNodeID {
		db.logger.Fatal("invalid argument to CacheMalfeasanceProof")
	}
	if db.atxsdata != nil {
		db.atxsdata.SetMalicious(id)
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.malfeasanceCache.Add(id, proof)
}

// VRFNonce returns the VRF nonce of for the given node in the given epoch. This function is thread safe and will
// return an error if the nonce is not found in the ATX DB.
func (db *CachedDB) VRFNonce(id types.NodeID, epoch types.EpochID) (types.VRFPostIndex, error) {
	key := VrfNonceKey{id, epoch}
	if nonce, ok := db.vrfNonceCache.Get(key); ok {
		return *nonce, nil
	}

	nonce, err := atxs.VRFNonce(db, id, epoch)
	if err != nil {
		return types.VRFPostIndex(0), err
	}

	db.vrfNonceCache.Add(key, &nonce)
	return nonce, nil
}

// GetAtxHeader returns the ATX header by the given ID. This function is thread safe and will return an error if the ID
// is not found in the ATX DB.
func (db *CachedDB) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if id == types.EmptyATXID {
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
	if id == types.EmptyATXID {
		return nil, errors.New("trying to fetch empty atx id")
	}

	atx, err := atxs.Get(db, id)
	if err != nil {
		return nil, fmt.Errorf("get ATXs from DB: %w", err)
	}

	db.atxHdrCache.Add(id, atx.ToHeader())
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
		return nil, fmt.Errorf("inconsistent state: failed to get atx header: %w", err)
	}

	return atxHeader, nil
}

// GetEpochWeight returns the total weight of ATXs targeting the given epochID.
func (db *CachedDB) GetEpochWeight(epoch types.EpochID) (uint64, []types.ATXID, error) {
	var (
		weight uint64
		ids    []types.ATXID
	)
	if err := db.IterateEpochATXHeaders(epoch, func(header *types.ActivationTxHeader) error {
		weight += header.GetWeight()
		ids = append(ids, header.ID)
		return nil
	}); err != nil {
		return 0, nil, err
	}
	return weight, ids, nil
}

// IterateEpochATXHeaders iterates over ActivationTxs that target an epoch.
func (db *CachedDB) IterateEpochATXHeaders(
	epoch types.EpochID,
	iter func(*types.ActivationTxHeader) error,
) error {
	ids, err := atxs.GetIDsByEpoch(context.Background(), db, epoch-1)
	if err != nil {
		return err
	}
	for _, id := range ids {
		header, err := db.GetAtxHeader(id)
		if err != nil {
			return err
		}
		if err := iter(header); err != nil {
			return err
		}
	}
	return nil
}

func (db *CachedDB) IterateMalfeasanceProofs(
	iter func(types.NodeID, *types.MalfeasanceProof) error,
) error {
	ids, err := identities.GetMalicious(db)
	if err != nil {
		return err
	}
	for _, id := range ids {
		proof, err := db.GetMalfeasanceProof(id)
		if err != nil {
			return err
		}
		if err := iter(id, proof); err != nil {
			return err
		}
	}
	return nil
}

// GetLastAtx gets the last atx header of specified node ID.
func (db *CachedDB) GetLastAtx(nodeID types.NodeID) (*types.ActivationTxHeader, error) {
	if atxid, err := atxs.GetLastIDByNodeID(db, nodeID); err != nil {
		return nil, fmt.Errorf("no prev atx found: %w", err)
	} else if atx, err := db.GetAtxHeader(atxid); err != nil {
		return nil, fmt.Errorf("inconsistent state: failed to get atx header: %w", err)
	} else {
		return atx, nil
	}
}

// GetEpochAtx gets the atx header of specified node ID published in the specified epoch.
func (db *CachedDB) GetEpochAtx(
	epoch types.EpochID,
	nodeID types.NodeID,
) (*types.ActivationTxHeader, error) {
	vatx, err := atxs.GetByEpochAndNodeID(db, epoch, nodeID)
	if err != nil {
		return nil, fmt.Errorf("no epoch atx found: %w", err)
	}
	header := vatx.ToHeader()
	db.atxHdrCache.Add(vatx.ID(), header)
	return header, nil
}

// IdentityExists returns true if this NodeID has published any ATX.
func (db *CachedDB) IdentityExists(nodeID types.NodeID) (bool, error) {
	_, err := atxs.GetLastIDByNodeID(db, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (db *CachedDB) MaxHeightAtx() (types.ATXID, error) {
	return atxs.GetIDWithMaxHeight(db, types.EmptyNodeID, atxs.FilterAll)
}

// Hint marks which DB should be queried for a certain provided hash.
type Hint string

// DB hints per DB.
const (
	NoHint      Hint = ""
	BallotDB    Hint = "ballotDB"
	BlockDB     Hint = "blocksDB"
	ProposalDB  Hint = "proposalDB"
	ATXDB       Hint = "ATXDB"
	TXDB        Hint = "TXDB"
	POETDB      Hint = "POETDB"
	Malfeasance Hint = "malfeasance"
	ActiveSet   Hint = "activeset"
)

// NewBlobStore returns a BlobStore.
func NewBlobStore(db sql.Executor, proposals *store.Store) *BlobStore {
	return &BlobStore{DB: db, proposals: proposals}
}

// BlobStore gets data as a blob to serve direct fetch requests.
type BlobStore struct {
	DB        sql.Executor
	proposals *store.Store
}

type (
	loadBlobFunc func(ctx context.Context, db sql.Executor, key []byte, blob *sql.Blob) error
	blobSizeFunc func(db sql.Executor, ids [][]byte) (sizes []int, err error)
)

var loadBlobDispatch = map[Hint]loadBlobFunc{
	ATXDB:       atxs.LoadBlob,
	BallotDB:    ballots.LoadBlob,
	BlockDB:     blocks.LoadBlob,
	TXDB:        transactions.LoadBlob,
	POETDB:      poets.LoadBlob,
	Malfeasance: identities.LoadMalfeasanceBlob,
	ActiveSet:   activesets.LoadBlob,
}

var blobSizeDispatch = map[Hint]blobSizeFunc{
	ATXDB:       atxs.GetBlobSizes,
	BallotDB:    ballots.GetBlobSizes,
	BlockDB:     blocks.GetBlobSizes,
	TXDB:        transactions.GetBlobSizes,
	POETDB:      poets.GetBlobSizes,
	Malfeasance: identities.GetBlobSizes,
	ActiveSet:   activesets.GetBlobSizes,
}

func (bs *BlobStore) loadProposal(key []byte, blob *sql.Blob) error {
	id := types.ProposalID(types.BytesToHash(key).ToHash20())
	b, err := bs.proposals.GetBlob(id)
	switch {
	case err == nil:
		blob.Bytes = b
		return nil
	case errors.Is(err, store.ErrNotFound):
		return ErrNotFound
	default:
		return err
	}
}

func (bs *BlobStore) getProposalSizes(keys [][]byte) (sizes []int, err error) {
	sizes = make([]int, len(keys))
	for n, k := range keys {
		id := types.ProposalID(types.BytesToHash(k).ToHash20())
		size, err := bs.proposals.GetBlobSize(id)
		switch {
		case err == nil:
			sizes[n] = size
		case errors.Is(err, store.ErrNotFound):
			sizes[n] = -1
		default:
			return nil, err
		}
	}
	return sizes, err
}

// LoadBlob gets an blob as bytes by an object ID as bytes.
func (bs *BlobStore) LoadBlob(ctx context.Context, hint Hint, key []byte, blob *sql.Blob) error {
	if hint == ProposalDB {
		return bs.loadProposal(key, blob)
	}
	loader, found := loadBlobDispatch[hint]
	if !found {
		return fmt.Errorf("blob store not found %s", hint)
	}
	err := loader(ctx, bs.DB, key, blob)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNotFound):
		return ErrNotFound
	default:
		return fmt.Errorf("get %s blob: %w", hint, err)
	}
}

// GetBlobSizes returns the sizes of the blobs corresponding to the specified ids. For
// non-existent objects, the corresponding items are set to -1.
func (bs *BlobStore) GetBlobSizes(hint Hint, ids [][]byte) (sizes []int, err error) {
	if hint == ProposalDB {
		return bs.getProposalSizes(ids)
	}
	getSizes, found := blobSizeDispatch[hint]
	if !found {
		return nil, fmt.Errorf("blob store not found %s", hint)
	}
	sizes, err = getSizes(bs.DB, ids)
	if err != nil {
		return nil, fmt.Errorf("get %s blob sizes: %w", hint, err)
	}
	return sizes, nil
}

func (bs *BlobStore) Has(hint Hint, key []byte) (bool, error) {
	switch hint {
	case ATXDB:
		return atxs.Has(bs.DB, types.BytesToATXID(key))
	case ProposalDB:
		return bs.proposals.Has(types.ProposalID(types.BytesToHash(key).ToHash20())), nil
	case BallotDB:
		id := types.BallotID(types.BytesToHash(key).ToHash20())
		return ballots.Has(bs.DB, id)
	case BlockDB:
		id := types.BlockID(types.BytesToHash(key).ToHash20())
		return blocks.Has(bs.DB, id)
	case TXDB:
		return transactions.Has(bs.DB, types.TransactionID(types.BytesToHash(key)))
	case POETDB:
		var ref types.PoetProofRef
		copy(ref[:], key)
		return poets.Has(bs.DB, ref)
	case Malfeasance:
		return identities.IsMalicious(bs.DB, types.BytesToNodeID(key))
	case ActiveSet:
		return activesets.Has(bs.DB, key)
	}
	return false, fmt.Errorf("blob store not found %s", hint)
}
