package datastore

import (
	"context"
	"errors"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
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
	logger *zap.Logger

	// cache is optional in tests. It MUST be set for the 'App'
	// for properly checking malfeasance.
	atxsdata *atxsdata.Data

	atxCache      *lru.Cache[types.ATXID, *types.ActivationTx]
	vrfNonceCache *lru.Cache[VrfNonceKey, types.VRFPostIndex]

	// used to coordinate db update and cache
	mu               sync.Mutex
	malfeasanceCache *lru.Cache[types.NodeID, *wire.MalfeasanceProof]
}

type Config struct {
	// ATXSize must be larger than the sum of all ATXs in last 2 epochs to be effective
	ATXSize         int `mapstructure:"atx-size"`
	MalfeasanceSize int `mapstructure:"malfeasance-size"`
}

func DefaultConfig() Config {
	return Config{
		// NOTE(dshulyak) there are several places where this cache is used, but none of them require to hold
		// all atxs in memory. those places should eventually be refactored to load necessary data from db.
		ATXSize:         1_000,
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
func NewCachedDB(db Executor, lg *zap.Logger, opts ...Opt) *CachedDB {
	o := cacheOpts{cfg: DefaultConfig()}
	for _, opt := range opts {
		opt(&o)
	}
	lg.Info("initialized datastore", zap.Any("config", o.cfg))

	atxHdrCache, err := lru.New[types.ATXID, *types.ActivationTx](o.cfg.ATXSize)
	if err != nil {
		lg.Fatal("failed to create atx cache", zap.Error(err))
	}

	malfeasanceCache, err := lru.New[types.NodeID, *wire.MalfeasanceProof](o.cfg.MalfeasanceSize)
	if err != nil {
		lg.Fatal("failed to create malfeasance cache", zap.Error(err))
	}

	vrfNonceCache, err := lru.New[VrfNonceKey, types.VRFPostIndex](o.cfg.ATXSize)
	if err != nil {
		lg.Fatal("failed to create vrf nonce cache", zap.Error(err))
	}

	return &CachedDB{
		Executor:         db,
		QueryCache:       db.QueryCache(),
		logger:           lg,
		atxsdata:         o.atxsdata,
		atxCache:         atxHdrCache,
		malfeasanceCache: malfeasanceCache,
		vrfNonceCache:    vrfNonceCache,
	}
}

func (db *CachedDB) MalfeasanceCacheSize() int {
	return db.malfeasanceCache.Len()
}

// GetMalfeasanceProof gets the malfeasance proof associated with the NodeID.
func (db *CachedDB) GetMalfeasanceProof(id types.NodeID) (*wire.MalfeasanceProof, error) {
	if id == types.EmptyNodeID {
		panic("invalid argument to GetMalfeasanceProof")
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

func (db *CachedDB) CacheMalfeasanceProof(id types.NodeID, proof *wire.MalfeasanceProof) {
	if id == types.EmptyNodeID {
		panic("invalid argument to CacheMalfeasanceProof")
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
		return nonce, nil
	}

	nonce, err := atxs.VRFNonce(db, id, epoch)
	if err != nil {
		return types.VRFPostIndex(0), err
	}

	db.vrfNonceCache.Add(key, nonce)
	return nonce, nil
}

// GetAtx returns the ATX by the given ID. This function is thread safe and will return an error if the ID
// is not found in the ATX DB.
func (db *CachedDB) GetAtx(id types.ATXID) (*types.ActivationTx, error) {
	if id == types.EmptyATXID {
		return nil, errors.New("trying to fetch empty atx id")
	}

	if atx, gotIt := db.atxCache.Get(id); gotIt {
		return atx, nil
	}

	atx, err := atxs.Get(db, id)
	if err != nil {
		return nil, fmt.Errorf("get ATX from DB: %w", err)
	}

	db.atxCache.Add(id, atx)
	return atx, nil
}

// Previous retrieves the list of previous ATXs for the given ATX ID.
func (db *CachedDB) Previous(id types.ATXID) ([]types.ATXID, error) {
	return atxs.Previous(db, id)
}

func (db *CachedDB) IterateMalfeasanceProofs(
	iter func(types.NodeID, *wire.MalfeasanceProof) error,
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
	ATXDB: func(ctx context.Context, db sql.Executor, key []byte, blob *sql.Blob) error {
		_, err := atxs.LoadBlob(ctx, db, key, blob)
		return err
	},
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
