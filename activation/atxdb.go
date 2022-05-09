package activation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/post/shared"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errInvalidSig = errors.New("identity not found when validating signature, invalid atx")
	errKnownAtx   = errors.New("known atx")
)

type atxChan struct {
	ch        chan struct{}
	listeners int
}

// DB hold the atxs received from all nodes and their validity status
// it also stores identifications for all nodes e.g the coupling between ed id and bls id.
type DB struct {
	sync.RWMutex
	sqlDB           *sql.Database
	atxHeaderCache  AtxCache
	LayersPerEpoch  uint32
	goldenATXID     types.ATXID
	nipostValidator nipostValidator
	log             log.Log
	processAtxMutex sync.Mutex
	atxChannels     map[types.ATXID]*atxChan

	// FIXME(dshulyak) this should not be defined in database
	fetcher system.Fetcher
}

// NewDB creates a new struct of type DB, this struct will hold the atxs received from all nodes and
// their validity.
func NewDB(sqlDB *sql.Database, fetcher system.Fetcher, layersPerEpoch uint32, goldenATXID types.ATXID, nipostValidator nipostValidator, log log.Log) *DB {
	db := &DB{
		sqlDB:           sqlDB,
		atxHeaderCache:  NewAtxCache(600),
		LayersPerEpoch:  layersPerEpoch,
		goldenATXID:     goldenATXID,
		nipostValidator: nipostValidator,
		log:             log,
		atxChannels:     make(map[types.ATXID]*atxChan),
		fetcher:         fetcher,
	}
	return db
}

var closedChan = make(chan struct{})

func init() {
	close(closedChan)
}

// AwaitAtx returns a channel that will receive notification when the specified atx with id id is received via gossip.
func (db *DB) AwaitAtx(id types.ATXID) chan struct{} {
	db.Lock()
	defer db.Unlock()

	if has, err := atxs.Has(db.sqlDB, id); err == nil && has {
		return closedChan
	}

	ch, found := db.atxChannels[id]
	if !found {
		ch = &atxChan{
			ch:        make(chan struct{}),
			listeners: 0,
		}
		db.atxChannels[id] = ch
	}
	ch.listeners++
	return ch.ch
}

// UnsubscribeAtx un subscribes the waiting for a specific atx with atx id id to arrive via gossip.
func (db *DB) UnsubscribeAtx(id types.ATXID) {
	db.Lock()
	defer db.Unlock()

	ch, found := db.atxChannels[id]
	if !found {
		return
	}
	ch.listeners--
	if ch.listeners < 1 {
		delete(db.atxChannels, id)
	}
}

// ProcessAtx validates the active set size declared in the atx, and contextually validates the atx according to atx
// validation rules it then stores the atx with flag set to validity of the atx.
//
// ATXs received as input must be already syntactically valid. Only contextual validation is performed.
func (db *DB) ProcessAtx(ctx context.Context, atx *types.ActivationTx) error {
	db.processAtxMutex.Lock()
	defer db.processAtxMutex.Unlock()

	existingATX, _ := db.GetAtxHeader(atx.ID())
	if existingATX != nil { // Already processed
		return nil
	}
	epoch := atx.PubLayerID.GetEpoch()
	db.log.WithContext(ctx).With().Info("processing atx",
		atx.ID(),
		epoch,
		log.FieldNamed("atx_node_id", atx.NodeID),
		atx.PubLayerID)
	if err := db.ContextuallyValidateAtx(atx.ActivationTxHeader); err != nil {
		db.log.WithContext(ctx).With().Warning("atx failed contextual validation",
			atx.ID(),
			log.FieldNamed("atx_node_id", atx.NodeID),
			log.Err(err))
	} else {
		db.log.WithContext(ctx).With().Info("atx is valid", atx.ID())
	}
	if err := db.StoreAtx(ctx, epoch, atx); err != nil {
		return fmt.Errorf("cannot store atx %s: %w", atx.ShortString(), err)
	}
	return nil
}

// SyntacticallyValidateAtx ensures the following conditions apply, otherwise it returns an error.
//
// - If the sequence number is non-zero: PrevATX points to a syntactically valid ATX whose sequence number is one less
//   than the current ATX's sequence number.
// - If the sequence number is zero: PrevATX is empty.
// - Positioning ATX points to a syntactically valid ATX.
// - NIPost challenge is a hash of the serialization of the following fields:
//   NodeID, SequenceNumber, PrevATXID, LayerID, StartTick, PositioningATX.
// - The NIPost is valid.
// - ATX LayerID is NIPostLayerTime or less after the PositioningATX LayerID.
// - The ATX view of the previous epoch contains ActiveSetSize activations.
func (db *DB) SyntacticallyValidateAtx(ctx context.Context, atx *types.ActivationTx) error {
	events.ReportNewActivation(atx)
	pub, err := ExtractPublicKey(atx)
	if err != nil {
		return fmt.Errorf("cannot validate atx sig atx id %v err %v", atx.ShortString(), err)
	}
	if bytes.Compare(atx.NodeID[:], pub.Bytes()) != 0 {
		return fmt.Errorf("node ids don't match")
	}

	if atx.PositioningATX == *types.EmptyATXID {
		return fmt.Errorf("empty positioning atx")
	}

	if atx.PrevATXID != *types.EmptyATXID {
		err = db.ValidateSignedAtx(*pub, atx)
		if err != nil { // means there is no such identity
			return fmt.Errorf("no id found %v err %v", atx.ShortString(), err)
		}
		prevATX, err := db.GetAtxHeader(atx.PrevATXID)
		if err != nil {
			return fmt.Errorf("validation failed: prevATX not found: %v", err)
		}

		if prevATX.NodeID != atx.NodeID {
			return fmt.Errorf("previous atx belongs to different miner. atx.ID: %v, atx.NodeID: %v, prevAtx.NodeID: %v",
				atx.ShortString(), atx.NodeID, prevATX.NodeID)
		}

		prevEp := prevATX.PubLayerID.GetEpoch()
		curEp := atx.PubLayerID.GetEpoch()
		if prevEp >= curEp {
			return fmt.Errorf(
				"prevAtx epoch (%v, layer %v) isn't older than current atx epoch (%v, layer %v)",
				prevEp, prevATX.PubLayerID, curEp, atx.PubLayerID)
		}

		if prevATX.Sequence+1 != atx.Sequence {
			return fmt.Errorf("sequence number is not one more than prev sequence number")
		}

		if atx.InitialPost != nil {
			return fmt.Errorf("prevATX declared, but initial Post is included")
		}

		if atx.InitialPostIndices != nil {
			return fmt.Errorf("prevATX declared, but initial Post indices is included in challenge")
		}
	} else {
		if atx.Sequence != 0 {
			return fmt.Errorf("no prevATX declared, but sequence number not zero")
		}

		if atx.InitialPost == nil {
			return fmt.Errorf("no prevATX declared, but initial Post is not included")
		}

		if atx.InitialPostIndices == nil {
			return fmt.Errorf("no prevATX declared, but initial Post indices is not included in challenge")
		}

		if !bytes.Equal(atx.InitialPost.Indices, atx.InitialPostIndices) {
			return errors.New("initial Post indices included in challenge does not equal to the initial Post indices included in the atx")
		}

		// Use the NIPost's Post metadata, while overriding the challenge to a zero challenge,
		// as expected from the initial Post.
		initialPostMetadata := *atx.NIPost.PostMetadata
		initialPostMetadata.Challenge = shared.ZeroChallenge
		if err := db.nipostValidator.ValidatePost(pub.Bytes(), atx.InitialPost, &initialPostMetadata, atx.NumUnits); err != nil {
			return fmt.Errorf("invalid initial Post: %v", err)
		}
	}

	if atx.PositioningATX != db.goldenATXID {
		posAtx, err := db.GetAtxHeader(atx.PositioningATX)
		if err != nil {
			return fmt.Errorf("positioning atx not found")
		}
		if !atx.PubLayerID.After(posAtx.PubLayerID) {
			return fmt.Errorf("atx layer (%v) must be after positioning atx layer (%v)",
				atx.PubLayerID, posAtx.PubLayerID)
		}
		if d := atx.PubLayerID.Difference(posAtx.PubLayerID); d > db.LayersPerEpoch {
			return fmt.Errorf("expected distance of one epoch (%v layers) from pos atx but found %v",
				db.LayersPerEpoch, d)
		}
	} else {
		publicationEpoch := atx.PubLayerID.GetEpoch()
		if !publicationEpoch.NeedsGoldenPositioningATX() {
			return fmt.Errorf("golden atx used for atx in epoch %d, but is only valid in epoch 1", publicationEpoch)
		}
	}

	expectedChallengeHash, err := atx.NIPostChallenge.Hash()
	if err != nil {
		return fmt.Errorf("failed to compute NIPost's expected challenge hash: %v", err)
	}

	db.log.WithContext(ctx).With().Info("validating nipost", log.String("expected_challenge_hash", expectedChallengeHash.String()), atx.ID())

	pubKey := signing.NewPublicKey(atx.NodeID[:])
	if err = db.nipostValidator.Validate(*pubKey, atx.NIPost, *expectedChallengeHash, atx.NumUnits); err != nil {
		return fmt.Errorf("invalid nipost: %v", err)
	}

	return nil
}

// ContextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (db *DB) ContextuallyValidateAtx(atx *types.ActivationTxHeader) error {
	if atx.PrevATXID != *types.EmptyATXID {
		lastAtx, err := db.GetNodeLastAtxID(atx.NodeID)
		if err != nil {
			db.log.With().Error("could not fetch node last atx", atx.ID(),
				log.FieldNamed("atx_node_id", atx.NodeID),
				log.Err(err))
			return fmt.Errorf("could not fetch node last atx: %v", err)
		}
		// last atx is not the one referenced
		if lastAtx != atx.PrevATXID {
			return fmt.Errorf("last atx is not the one referenced")
		}
	} else {
		lastAtx, err := db.GetNodeLastAtxID(atx.NodeID)
		if _, ok := err.(ErrAtxNotFound); err != nil && !ok {
			db.log.With().Error("fetching atx ids failed", log.Err(err))
			return err
		}
		if err == nil { // we found an ATX for this node ID, although it reported no prevATX -- this is invalid
			return fmt.Errorf("no prevATX reported, but other atx with same nodeID (%v) found: %v",
				atx.NodeID.ShortString(), lastAtx.ShortString())
		}
	}

	return nil
}

// StoreAtx stores an atx for epoch ech, it stores atx for the current epoch and adds the atx for the nodeID that
// created it in a sorted manner by the sequence id. This function does not validate the atx and assumes all data is
// correct and that all associated atx exist in the db. Will return error if writing to db failed.
func (db *DB) StoreAtx(ctx context.Context, ech types.EpochID, atx *types.ActivationTx) error {
	db.Lock()
	defer db.Unlock()

	// todo: maybe cleanup DB if failed by using defer (#1921)
	if err := db.storeATXInDB(atx); err != nil {
		return fmt.Errorf("store ATX in DB: %w", err)
	}

	// notify subscribers
	if ch, found := db.atxChannels[atx.ID()]; found {
		close(ch.ch)
		delete(db.atxChannels, atx.ID())
	}

	db.log.WithContext(ctx).With().Info("finished storing atx in epoch", atx.ID(), ech)
	return nil
}

func (db *DB) storeATXInDB(atx *types.ActivationTx) error {
	dbTx, err := db.sqlDB.Tx(context.Background())
	if err != nil {
		return err
	}
	defer dbTx.Release()

	if err := atxs.Add(dbTx, atx, time.Now()); err != nil {
		if errors.Is(err, sql.ErrObjectExists) {
			// exists - how should we handle this?
			return nil
		}
		return fmt.Errorf("add ATX to DB: %w", err)
	}

	if err := atxs.UpdateTopIfNeeded(dbTx, atx); err != nil {
		return fmt.Errorf("update top: %w", err)
	}

	return dbTx.Commit()
}

// ErrAtxNotFound is a specific error returned when no atx was found in DB.
type ErrAtxNotFound error

// GetNodeLastAtxID returns the last atx id that was received for node nodeID.
func (db *DB) GetNodeLastAtxID(nodeID types.NodeID) (types.ATXID, error) {
	id, err := atxs.GetLastIDByNodeID(db.sqlDB, nodeID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			err := ErrAtxNotFound(fmt.Errorf("atx for node %v does not exist", nodeID.ShortString()))
			return *types.EmptyATXID, fmt.Errorf("find ATX in DB: %w", err)
		}
		return types.ATXID{}, fmt.Errorf("failed to get top atx: %v", err)
	}

	return id, nil
}

// GetEpochAtxs returns all valid ATXs received in the epoch epochID.
func (db *DB) GetEpochAtxs(epochID types.EpochID) (ids []types.ATXID, err error) {
	ids, err = atxs.GetIDsByEpoch(db.sqlDB, epochID)
	db.log.With().Debug("returned epoch atxs", epochID,
		log.Int("count", len(ids)),
		log.String("atxs", fmt.Sprint(ids)))
	return
}

// GetNodeAtxIDForEpoch returns an atx published by the provided nodeID for the specified publication epoch. meaning the atx
// that the requested nodeID has published. it returns an error if no atx was found for provided nodeID.
func (db *DB) GetNodeAtxIDForEpoch(nodeID types.NodeID, publicationEpoch types.EpochID) (types.ATXID, error) {
	id, err := atxs.GetIDByEpochAndNodeID(db.sqlDB, publicationEpoch, nodeID)
	if err != nil {
		return *types.EmptyATXID, fmt.Errorf("atx for node %v with publication epoch %v: %v",
			nodeID.ShortString(), publicationEpoch, err)
	}
	return id, nil
}

// GetPosAtxID returns the best (highest layer id), currently known to this node, pos atx id.
func (db *DB) GetPosAtxID() (types.ATXID, error) {
	id, err := atxs.GetTop(db.sqlDB)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return db.goldenATXID, nil
		}
		return *types.EmptyATXID, fmt.Errorf("failed to get top atx: %v", err)
	}

	return id, nil
}

// GetAtxTimestamp returns ATX timestamp.
func (db *DB) GetAtxTimestamp(id types.ATXID) (time.Time, error) {
	ts, err := atxs.GetTimestamp(db.sqlDB, id)
	if err != nil {
		return time.Time{}, err
	}

	return ts, nil
}

// GetEpochWeight returns the total weight of ATXs targeting the given epochID.
func (db *DB) GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error) {
	weight := uint64(0)
	activeSet, err := db.GetEpochAtxs(epochID - 1)
	if err != nil {
		return 0, nil, err
	}
	for _, atxID := range activeSet {
		atxHeader, err := db.GetAtxHeader(atxID)
		if err != nil {
			return 0, nil, err
		}
		weight += atxHeader.GetWeight()
	}
	return weight, activeSet, nil
}

// GetAtxHeader returns the ATX header by the given ID. This function is thread safe and will return an error if the ID
// is not found in the ATX DB.
func (db *DB) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if id == *types.EmptyATXID {
		return nil, errors.New("trying to fetch empty atx id")
	}

	if atxHeader, gotIt := db.atxHeaderCache.Get(id); gotIt {
		return atxHeader, nil
	}
	atxHeader, err := db.GetFullAtx(id)
	if err != nil {
		return nil, fmt.Errorf("get ATXs from DB: %w", err)
	}

	db.atxHeaderCache.Add(id, atxHeader.ActivationTxHeader)
	return atxHeader.ActivationTxHeader, nil
}

// GetFullAtx returns the full atx struct of the given atxId id, it returns an error if the full atx cannot be found
// in all databases.
func (db *DB) GetFullAtx(id types.ATXID) (*types.ActivationTx, error) {
	if id == *types.EmptyATXID {
		return nil, errors.New("trying to fetch empty atx id")
	}

	atx, err := atxs.Get(db.sqlDB, id)
	if err != nil {
		return nil, fmt.Errorf("get ATXs from DB: %w", err)
	}

	db.atxHeaderCache.Add(id, atx.ActivationTxHeader)

	return atx, nil
}

// ValidateSignedAtx extracts public key from message and verifies public key exists in idStore, this is how we validate
// ATX signature. If this is the first ATX it is considered valid anyways and ATX syntactic validation will determine ATX validity.
func (db *DB) ValidateSignedAtx(pubKey signing.PublicKey, signedAtx *types.ActivationTx) error {
	// this is the first occurrence of this identity, we cannot validate simply by extracting public key
	// pass it down to Atx handling so that atx can be syntactically verified and identity could be registered.
	if signedAtx.PrevATXID == *types.EmptyATXID {
		return nil
	}
	return nil
}

// HandleGossipAtx handles the atx gossip data channel.
func (db *DB) HandleGossipAtx(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
	if err := db.handleAtxData(ctx, msg); errors.Is(err, errKnownAtx) {
		return pubsub.ValidationIgnore
	} else if err != nil {
		db.log.WithContext(ctx).With().Error("error handling atx data", log.Err(err))
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

// HandleAtxData handles atxs received either by gossip or sync.
func (db *DB) HandleAtxData(ctx context.Context, data []byte) error {
	err := db.handleAtxData(ctx, data)
	if errors.Is(err, errKnownAtx) {
		return nil
	}
	return err
}

func (db *DB) handleAtxData(ctx context.Context, data []byte) error {
	atx, err := types.BytesToAtx(data)
	if err != nil {
		return fmt.Errorf("cannot parse incoming atx")
	}
	atx.CalcAndSetID()
	logger := db.log.WithContext(ctx).WithFields(atx.ID())
	existing, _ := db.GetAtxHeader(atx.ID())
	if existing != nil {
		logger.With().Debug("received known atx")
		return fmt.Errorf("%w atx %s", errKnownAtx, atx.ID())
	}

	logger.With().Info(fmt.Sprintf("got new atx %v", atx.ID().ShortString()), atx.Fields(len(data))...)

	if atx.NIPost == nil {
		return fmt.Errorf("nil nipst in gossip for atx %s", atx.ShortString())
	}

	if err := db.fetcher.GetPoetProof(ctx, atx.GetPoetProofRef()); err != nil {
		return fmt.Errorf("received atx (%v) with syntactically invalid or missing PoET proof (%x): %v",
			atx.ShortString(), atx.GetPoetProofRef().ShortString(), err)
	}

	if err := db.FetchAtxReferences(ctx, atx); err != nil {
		return fmt.Errorf("received atx with missing references of prev or pos id %v, %v, %v, %v",
			atx.ID().ShortString(), atx.PrevATXID.ShortString(), atx.PositioningATX.ShortString(), log.Err(err))
	}

	err = db.SyntacticallyValidateAtx(ctx, atx)
	if err != nil {
		return fmt.Errorf("received syntactically invalid atx %v: %v", atx.ShortString(), err)
	}

	err = db.ProcessAtx(ctx, atx)
	if err != nil {
		return fmt.Errorf("cannot process atx %v: %v", atx.ShortString(), err)
		// TODO: blacklist peer
	}

	logger.With().Info("stored and propagated new syntactically valid atx", atx.ID())
	return nil
}

// FetchAtxReferences fetches positioning and prev atxs from peers if they are not found in db.
func (db *DB) FetchAtxReferences(ctx context.Context, atx *types.ActivationTx) error {
	logger := db.log.WithContext(ctx)
	if atx.PositioningATX != *types.EmptyATXID && atx.PositioningATX != db.goldenATXID {
		logger.With().Debug("going to fetch pos atx", atx.PositioningATX, atx.ID())
		if err := db.fetcher.FetchAtx(ctx, atx.PositioningATX); err != nil {
			return fmt.Errorf("fetch positioning ATX: %w", err)
		}
	}

	if atx.PrevATXID != *types.EmptyATXID {
		logger.With().Debug("going to fetch prev atx", atx.PrevATXID, atx.ID())
		if err := db.fetcher.FetchAtx(ctx, atx.PrevATXID); err != nil {
			return fmt.Errorf("fetch previous ATX ID: %w", err)
		}
	}
	logger.With().Debug("done fetching references for atx", atx.ID())

	return nil
}

// ATXs exports the ATXs database.
func (db *DB) ATXs() database.Getter {
	return newATXFetcherDB(db)
}

func newATXFetcherDB(db *DB) *ATXFetcher {
	return &ATXFetcher{DB: db}
}

// ATXFetcher is an adapter of SQLite implementation to legacy LevelDB interfaces.
type ATXFetcher struct {
	DB *DB
}

// Get gets an ATX as bytes by an ATX ID as bytes.
func (f *ATXFetcher) Get(key []byte) ([]byte, error) {
	return atxs.GetBlob(f.DB.sqlDB, types.ATXID(types.BytesToHash(key)))
}
