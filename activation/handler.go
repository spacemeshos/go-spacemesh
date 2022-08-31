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
	"github.com/spacemeshos/go-spacemesh/datastore"
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
	errKnownAtx      = errors.New("known atx")
	errMalformedData = errors.New("malformed data")
)

type atxChan struct {
	ch        chan struct{}
	listeners int
}

// Handler processes the atxs received from all nodes and their validity status.
type Handler struct {
	sync.RWMutex

	cdb             *datastore.CachedDB
	layersPerEpoch  uint32
	tickSize        uint64
	goldenATXID     types.ATXID
	nipostValidator nipostValidator
	log             log.Log
	processAtxMutex sync.Mutex
	atxChannels     map[types.ATXID]*atxChan
	fetcher         system.Fetcher
}

// NewHandler returns a data handler for ATX.
func NewHandler(cdb *datastore.CachedDB, fetcher system.Fetcher, layersPerEpoch uint32, tickSize uint64, goldenATXID types.ATXID, nipostValidator nipostValidator, log log.Log) *Handler {
	db := &Handler{
		cdb:             cdb,
		layersPerEpoch:  layersPerEpoch,
		tickSize:        tickSize,
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
func (h *Handler) AwaitAtx(id types.ATXID) chan struct{} {
	h.Lock()
	defer h.Unlock()

	if has, err := atxs.Has(h.cdb, id); err == nil && has {
		return closedChan
	}

	ch, found := h.atxChannels[id]
	if !found {
		ch = &atxChan{
			ch:        make(chan struct{}),
			listeners: 0,
		}
		h.atxChannels[id] = ch
	}
	ch.listeners++
	return ch.ch
}

// UnsubscribeAtx un subscribes the waiting for a specific atx with atx id id to arrive via gossip.
func (h *Handler) UnsubscribeAtx(id types.ATXID) {
	h.Lock()
	defer h.Unlock()

	ch, found := h.atxChannels[id]
	if !found {
		return
	}
	ch.listeners--
	if ch.listeners < 1 {
		delete(h.atxChannels, id)
	}
}

// ProcessAtx validates the active set size declared in the atx, and contextually validates the atx according to atx
// validation rules it then stores the atx with flag set to validity of the atx.
//
// ATXs received as input must be already syntactically valid. Only contextual validation is performed.
func (h *Handler) ProcessAtx(ctx context.Context, atx *types.ActivationTx) error {
	h.processAtxMutex.Lock()
	defer h.processAtxMutex.Unlock()

	existingATX, _ := h.cdb.AtxByID(atx.ID())
	if existingATX != nil { // Already processed
		return nil
	}
	epoch := atx.PubLayerID.GetEpoch()
	h.log.WithContext(ctx).With().Info("processing atx",
		atx.ID(),
		epoch,
		log.FieldNamed("atx_node_id", atx.NodeID()),
		atx.PubLayerID)
	if err := h.ContextuallyValidateAtx(atx); err != nil {
		h.log.WithContext(ctx).With().Warning("atx failed contextual validation",
			atx.ID(),
			log.FieldNamed("atx_node_id", atx.NodeID()),
			log.Err(err))
	} else {
		h.log.WithContext(ctx).With().Info("atx is valid", atx.ID())
	}
	if err := h.StoreAtx(ctx, epoch, atx); err != nil {
		return fmt.Errorf("cannot store atx %s: %w", atx.ShortString(), err)
	}
	return nil
}

// SyntacticallyValidateAtx ensures the following conditions apply, otherwise it returns an error.
//
//   - If the sequence number is non-zero: PrevATX points to a syntactically valid ATX whose sequence number is one less
//     than the current ATXs sequence number.
//   - If the sequence number is zero: PrevATX is empty.
//   - Positioning ATX points to a syntactically valid ATX.
//   - NIPost challenge is a hash of the serialization of the following fields:
//     NodeID, SequenceNumber, PrevATXID, LayerID, StartTick, PositioningATX.
//   - The NIPost is valid.
//   - ATX LayerID is NIPostLayerTime or less after the PositioningATX LayerID.
//   - The ATX view of the previous epoch contains ActiveSetSize activations.
func (h *Handler) SyntacticallyValidateAtx(ctx context.Context, atx *types.ActivationTx) error {
	events.ReportNewActivation(atx)

	if atx.PositioningATX == *types.EmptyATXID {
		return fmt.Errorf("empty positioning atx")
	}

	if atx.PrevATXID != *types.EmptyATXID {
		prevATX, err := h.cdb.AtxByID(atx.PrevATXID)
		if err != nil {
			return fmt.Errorf("validation failed: prevATX not found: %v", err)
		}

		if prevATX.NodeID() != atx.NodeID() {
			return fmt.Errorf("previous atx belongs to different miner. atx.ID: %v, atx.NodeID: %v, prevAtx.NodeID: %v",
				atx.ShortString(), atx.NodeID(), prevATX.NodeID())
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
		if err := h.nipostValidator.ValidatePost(atx.NodeID().ToBytes(), atx.InitialPost, &initialPostMetadata, uint(atx.NumUnits)); err != nil {
			return fmt.Errorf("invalid initial Post: %v", err)
		}
	}
	var baseTickHeight uint64
	if atx.PositioningATX != h.goldenATXID {
		posAtx, err := h.cdb.AtxByID(atx.PositioningATX)
		if err != nil {
			return fmt.Errorf("positioning atx not found")
		}
		if !atx.PubLayerID.After(posAtx.PubLayerID) {
			return fmt.Errorf("atx layer (%v) must be after positioning atx layer (%v)",
				atx.PubLayerID, posAtx.PubLayerID)
		}
		if d := atx.PubLayerID.Difference(posAtx.PubLayerID); d > h.layersPerEpoch {
			return fmt.Errorf("expected distance of one epoch (%v layers) from pos atx but found %v",
				h.layersPerEpoch, d)
		}
		baseTickHeight = posAtx.TickHeight()
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

	h.log.WithContext(ctx).With().Info("validating nipost", log.String("expected_challenge_hash", expectedChallengeHash.String()), atx.ID())

	pubKey := signing.NewPublicKey(atx.NodeID().ToBytes())
	leaves, err := h.nipostValidator.Validate(*pubKey, atx.NIPost, *expectedChallengeHash, uint(atx.NumUnits))
	if err != nil {
		return fmt.Errorf("invalid nipost: %v", err)
	}
	atx.Verify(baseTickHeight, leaves/h.tickSize)
	return nil
}

// ContextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (h *Handler) ContextuallyValidateAtx(atx *types.ActivationTx) error {
	if atx.PrevATXID != *types.EmptyATXID {
		lastAtx, err := atxs.LastIDByNodeID(h.cdb, atx.NodeID())
		if err != nil {
			h.log.With().Error("could not fetch node last atx", atx.ID(),
				log.FieldNamed("atx_node_id", atx.NodeID()),
				log.Err(err))
			return fmt.Errorf("could not fetch node last atx: %w", err)
		}
		// last atx is not the one referenced
		if lastAtx != atx.PrevATXID {
			return fmt.Errorf("last atx is not the one referenced")
		}
	} else {
		lastAtx, err := atxs.LastIDByNodeID(h.cdb, atx.NodeID())
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			h.log.With().Error("fetching atx ids failed", log.Err(err))
			return err
		}

		if err == nil {
			// we found an ATX for this node ID, although it reported no prevATX -- this is invalid
			return fmt.Errorf("no prevATX reported, but other atx with same nodeID (%v) found: %v",
				atx.NodeID().ShortString(), lastAtx.ShortString())
		}
	}

	return nil
}

// StoreAtx stores an atx for epoch ech, it stores atx for the current epoch and adds the atx for the nodeID that
// created it in a sorted manner by the sequence id. This function does not validate the atx and assumes all data is
// correct and that all associated atx exist in the db. Will return error if writing to db failed.
func (h *Handler) StoreAtx(ctx context.Context, ech types.EpochID, atx *types.ActivationTx) error {
	h.Lock()
	defer h.Unlock()

	if err := atxs.Add(h.cdb, atx, time.Now()); err != nil {
		if errors.Is(err, sql.ErrObjectExists) {
			// exists - how should we handle this?
			return nil
		}
		return fmt.Errorf("add atx to db: %w", err)
	}

	// notify subscribers
	if ch, found := h.atxChannels[atx.ID()]; found {
		close(ch.ch)
		delete(h.atxChannels, atx.ID())
	}

	h.log.WithContext(ctx).With().Info("finished storing atx in epoch", atx.ID(), ech)
	return nil
}

// GetEpochAtxs returns all valid ATXs received in the epoch epochID.
func (h *Handler) GetEpochAtxs(epochID types.EpochID) (ids []types.ATXID, err error) {
	ids, err = atxs.IDsByEpoch(h.cdb, epochID)
	h.log.With().Debug("returned epoch atxs", epochID,
		log.Int("count", len(ids)),
		log.String("atxs", fmt.Sprint(ids)))
	return
}

// GetPosAtxID returns the best (highest layer id), currently known to this node, pos atx id.
func (h *Handler) GetPosAtxID() (types.ATXID, error) {
	id, err := atxs.PositioningID(h.cdb)
	if err != nil {
		return *types.EmptyATXID, fmt.Errorf("failed to get positioning atx: %w", err)
	}
	return id, nil
}

// HandleGossipAtx handles the atx gossip data channel.
func (h *Handler) HandleGossipAtx(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
	err := h.handleAtxData(ctx, msg)
	switch {
	case err == nil:
		return pubsub.ValidationAccept
	case errors.Is(err, errMalformedData):
		return pubsub.ValidationReject
	case errors.Is(err, errKnownAtx):
		return pubsub.ValidationIgnore
	default:
		h.log.WithContext(ctx).With().Warning("failed to process atx gossip", log.Err(err))
		return pubsub.ValidationIgnore
	}
}

// HandleAtxData handles atxs received either by gossip or sync.
func (h *Handler) HandleAtxData(ctx context.Context, data []byte) error {
	err := h.handleAtxData(ctx, data)
	if errors.Is(err, errKnownAtx) {
		return nil
	}
	return err
}

func (h *Handler) handleAtxData(ctx context.Context, data []byte) error {
	atx, err := types.BytesToAtx(data)
	if err != nil {
		return errMalformedData
	}
	atx.CalcAndSetID()
	if err := atx.CalcAndSetNodeID(); err != nil {
		return fmt.Errorf("failed to derive Node ID from ATX with sig %v: %w", atx.Sig, err)
	}
	logger := h.log.WithContext(ctx).WithFields(atx.ID())
	existing, _ := h.cdb.AtxByID(atx.ID())
	if existing != nil {
		logger.With().Debug("received known atx")
		return fmt.Errorf("%w atx %s", errKnownAtx, atx.ID())
	}

	if atx.NIPost == nil {
		return fmt.Errorf("nil nipst in gossip for atx %s", atx.ShortString())
	}

	if err := h.fetcher.GetPoetProof(ctx, atx.GetPoetProofRef()); err != nil {
		return fmt.Errorf("received atx (%v) with syntactically invalid or missing PoET proof (%x): %v",
			atx.ShortString(), atx.GetPoetProofRef().ShortString(), err)
	}

	if err := h.FetchAtxReferences(ctx, atx); err != nil {
		return fmt.Errorf("received atx with missing references of prev or pos id %v, %v, %v, %v",
			atx.ID().ShortString(), atx.PrevATXID.ShortString(), atx.PositioningATX.ShortString(), log.Err(err))
	}

	err = h.SyntacticallyValidateAtx(ctx, atx)
	if err != nil {
		return fmt.Errorf("received syntactically invalid atx %v: %v", atx.ShortString(), err)
	}
	err = h.ProcessAtx(ctx, atx)
	if err != nil {
		return fmt.Errorf("cannot process atx %v: %v", atx.ShortString(), err)
		// TODO: blacklist peer
	}

	logger.With().Info("got new atx", log.Inline(atx), log.Int("size", len(data)))
	return nil
}

// FetchAtxReferences fetches positioning and prev atxs from peers if they are not found in db.
func (h *Handler) FetchAtxReferences(ctx context.Context, atx *types.ActivationTx) error {
	logger := h.log.WithContext(ctx)
	var atxIDs []types.ATXID
	if atx.PositioningATX != *types.EmptyATXID && atx.PositioningATX != h.goldenATXID {
		logger.With().Debug("going to fetch pos atx", atx.PositioningATX, atx.ID())
		atxIDs = append(atxIDs, atx.PositioningATX)
	}

	if atx.PrevATXID != *types.EmptyATXID {
		logger.With().Debug("going to fetch prev atx", atx.PrevATXID, atx.ID())
		if len(atxIDs) < 1 || atx.PrevATXID != atxIDs[0] {
			atxIDs = append(atxIDs, atx.PrevATXID)
		}
	}
	if len(atxIDs) == 0 {
		return nil
	}

	if err := h.fetcher.GetAtxs(ctx, atxIDs); err != nil {
		return fmt.Errorf("fetch referenced atxs: %w", err)
	}
	logger.With().Debug("done fetching references for atx", atx.ID())
	return nil
}
