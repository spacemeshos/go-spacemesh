package activation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/post/shared"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errKnownAtx      = errors.New("known atx")
	errMalformedData = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	errMaliciousATX  = errors.New("malicious atx")
)

type atxChan struct {
	ch        chan struct{}
	listeners int
}

// Handler processes the atxs received from all nodes and their validity status.
type Handler struct {
	cdb             *datastore.CachedDB
	edVerifier      *signing.EdVerifier
	clock           layerClock
	publisher       pubsub.Publisher
	layersPerEpoch  uint32
	tickSize        uint64
	goldenATXID     types.ATXID
	nipostValidator nipostValidator
	atxReceivers    []AtxReceiver
	log             log.Log
	mu              sync.Mutex
	atxChannels     map[types.ATXID]*atxChan
	fetcher         system.Fetcher
	poetCfg         PoetConfig
}

// NewHandler returns a data handler for ATX.
func NewHandler(
	cdb *datastore.CachedDB,
	edVerifier *signing.EdVerifier,
	c layerClock,
	pub pubsub.Publisher,
	fetcher system.Fetcher,
	layersPerEpoch uint32,
	tickSize uint64,
	goldenATXID types.ATXID,
	nipostValidator nipostValidator,
	atxReceivers []AtxReceiver,
	log log.Log,
	poetCfg PoetConfig,
) *Handler {
	return &Handler{
		cdb:             cdb,
		edVerifier:      edVerifier,
		clock:           c,
		publisher:       pub,
		layersPerEpoch:  layersPerEpoch,
		tickSize:        tickSize,
		goldenATXID:     goldenATXID,
		nipostValidator: nipostValidator,
		atxReceivers:    atxReceivers,
		log:             log,
		atxChannels:     make(map[types.ATXID]*atxChan),
		fetcher:         fetcher,
		poetCfg:         poetCfg,
	}
}

var closedChan = make(chan struct{})

func init() {
	close(closedChan)
}

// AwaitAtx returns a channel that will receive notification when the specified atx with id is received via gossip.
func (h *Handler) AwaitAtx(id types.ATXID) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()

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
	h.mu.Lock()
	defer h.mu.Unlock()

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
func (h *Handler) ProcessAtx(ctx context.Context, atx *types.VerifiedActivationTx) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	existingATX, _ := h.cdb.GetAtxHeader(atx.ID())
	if existingATX != nil { // Already processed
		return nil
	}
	h.log.WithContext(ctx).With().Debug("processing atx",
		atx.ID(),
		atx.PublishEpoch,
		log.Stringer("smesher", atx.SmesherID),
		atx.PublishEpoch,
	)
	if err := h.ContextuallyValidateAtx(atx); err != nil {
		h.log.WithContext(ctx).With().Warning("atx failed contextual validation",
			atx.ID(),
			log.Stringer("smesher", atx.SmesherID),
			log.Err(err),
		)
	} else {
		h.log.WithContext(ctx).With().Debug("atx is valid", atx.ID())
	}
	if err := h.storeAtx(ctx, atx); err != nil {
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
func (h *Handler) SyntacticallyValidateAtx(ctx context.Context, atx *types.ActivationTx) (*types.VerifiedActivationTx, error) {
	var (
		commitmentATX *types.ATXID
		err           error
	)

	if atx.PrevATXID == types.EmptyATXID {
		if err := h.validateInitialAtx(ctx, atx); err != nil {
			return nil, err
		}
		commitmentATX = atx.CommitmentATX // validateInitialAtx checks that commitmentATX is not nil and references an existing valid ATX
	} else {
		commitmentATX, err = h.getCommitmentAtx(atx)
		if err != nil {
			return nil, fmt.Errorf("commitment atx for %s not found: %w", atx.SmesherID, err)
		}

		err = h.validateNonInitialAtx(ctx, atx, *commitmentATX)
		if err != nil {
			return nil, err
		}
	}

	if err := h.nipostValidator.PositioningAtx(&atx.PositioningATX, h.cdb, h.goldenATXID, atx.PublishEpoch, h.layersPerEpoch); err != nil {
		return nil, err
	}

	var baseTickHeight uint64
	if atx.PositioningATX != h.goldenATXID {
		posAtx, _ := h.cdb.GetAtxHeader(atx.PositioningATX) // cannot fail as pos atx is already verified
		baseTickHeight = posAtx.TickHeight()
	}

	expectedChallengeHash := atx.NIPostChallenge.Hash()
	h.log.WithContext(ctx).With().Info("validating nipost", log.String("expected_challenge_hash", expectedChallengeHash.String()), atx.ID())

	leaves, err := h.nipostValidator.NIPost(ctx, atx.SmesherID, *commitmentATX, atx.NIPost, expectedChallengeHash, atx.NumUnits)
	if err != nil {
		return nil, fmt.Errorf("invalid nipost: %w", err)
	}

	return atx.Verify(baseTickHeight, leaves/h.tickSize)
}

func (h *Handler) validateInitialAtx(ctx context.Context, atx *types.ActivationTx) error {
	if atx.InitialPost == nil {
		return fmt.Errorf("no prevATX declared, but initial Post is not included")
	}

	if atx.InnerActivationTx.NodeID == nil {
		return fmt.Errorf("no prevATX declared, but NodeID is missing")
	}

	if err := h.nipostValidator.InitialNIPostChallenge(&atx.NIPostChallenge, h.cdb, h.goldenATXID, atx.InitialPost.Indices); err != nil {
		return err
	}

	// Use the NIPost's Post metadata, while overriding the challenge to a zero challenge,
	// as expected from the initial Post.
	initialPostMetadata := *atx.NIPost.PostMetadata
	initialPostMetadata.Challenge = shared.ZeroChallenge

	if err := h.nipostValidator.Post(ctx, atx.SmesherID, *atx.CommitmentATX, atx.InitialPost, &initialPostMetadata, atx.NumUnits); err != nil {
		return fmt.Errorf("invalid initial Post: %w", err)
	}

	if atx.VRFNonce == nil {
		return fmt.Errorf("no prevATX declared, but VRFNonce is missing")
	}

	if err := h.nipostValidator.VRFNonce(atx.SmesherID, *atx.CommitmentATX, atx.VRFNonce, &initialPostMetadata, atx.NumUnits); err != nil {
		return fmt.Errorf("invalid VRFNonce: %w", err)
	}

	atx.SetEffectiveNumUnits(atx.NumUnits)
	return nil
}

func (h *Handler) validateNonInitialAtx(ctx context.Context, atx *types.ActivationTx, commitmentATX types.ATXID) error {
	if atx.InnerActivationTx.NodeID != nil {
		return fmt.Errorf("prevATX declared, but NodeID is included")
	}

	if err := h.nipostValidator.NIPostChallenge(&atx.NIPostChallenge, h.cdb, atx.SmesherID); err != nil {
		return err
	}

	prevAtx, err := h.cdb.GetAtxHeader(atx.PrevATXID)
	if err != nil {
		return err
	}

	nonce := atx.VRFNonce
	if atx.NumUnits > prevAtx.NumUnits && nonce == nil {
		h.log.WithContext(ctx).With().Info("PoST size increased without new VRF Nonce, re-validating current nonce",
			atx.ID(),
			log.Stringer("smesher", atx.SmesherID),
		)

		current, err := h.cdb.VRFNonce(atx.SmesherID, atx.TargetEpoch())
		if err != nil {
			return fmt.Errorf("failed to get current nonce: %w", err)
		}
		nonce = &current
	}

	if nonce != nil {
		err = h.nipostValidator.VRFNonce(atx.SmesherID, commitmentATX, nonce, atx.NIPost.PostMetadata, atx.NumUnits)
		if err != nil {
			return fmt.Errorf("invalid VRFNonce: %w", err)
		}
	}

	if atx.InitialPost != nil {
		return fmt.Errorf("prevATX declared, but initial Post is included")
	}

	if prevAtx.NumUnits < atx.NumUnits {
		atx.SetEffectiveNumUnits(prevAtx.NumUnits)
	} else {
		atx.SetEffectiveNumUnits(atx.NumUnits)
	}
	return nil
}

func (h *Handler) getCommitmentAtx(atx *types.ActivationTx) (*types.ATXID, error) {
	if atx.CommitmentATX != nil {
		return atx.CommitmentATX, nil
	}

	id, err := atxs.CommitmentATX(h.cdb, atx.SmesherID)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// ContextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (h *Handler) ContextuallyValidateAtx(atx *types.VerifiedActivationTx) error {
	lastAtx, err := atxs.GetLastIDByNodeID(h.cdb, atx.SmesherID)
	if err == nil && atx.PrevATXID == lastAtx {
		// last atx referenced equals last ATX seen from node
		return nil
	}

	if err == nil && atx.PrevATXID == types.EmptyATXID {
		// no previous atx declared, but already seen at least one atx from node
		return fmt.Errorf("no prevATX reported, but other atx with same nodeID (%v) found: %v", atx.SmesherID, lastAtx.ShortString())
	}

	if err == nil && atx.PrevATXID != lastAtx {
		// last atx referenced does not equal last ATX seen from node
		return fmt.Errorf("last atx is not the one referenced")
	}

	if errors.Is(err, sql.ErrNotFound) && atx.PrevATXID == types.EmptyATXID {
		// no previous atx found and none referenced
		return nil
	}

	if err != nil && atx.PrevATXID != types.EmptyATXID {
		// no previous atx found but previous atx referenced
		h.log.With().Error("could not fetch node last atx",
			atx.ID(),
			log.Stringer("smesher", atx.SmesherID),
			log.Err(err),
		)
		return fmt.Errorf("could not fetch node last atx: %w", err)
	}

	return err
}

// storeAtx stores an ATX and notifies subscribers of the ATXID.
func (h *Handler) storeAtx(ctx context.Context, atx *types.VerifiedActivationTx) error {
	malicious, err := h.cdb.IsMalicious(atx.SmesherID)
	if err != nil {
		return fmt.Errorf("checking if node is malicious: %w", err)
	}
	var proof *types.MalfeasanceProof
	if err = h.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		if !malicious {
			prev, err := atxs.GetByEpochAndNodeID(dbtx, atx.PublishEpoch, atx.SmesherID)
			if err != nil && !errors.Is(err, sql.ErrNotFound) {
				return err
			}
			// do ID check to be absolutely sure.
			if prev != nil && prev.ID() != atx.ID() {
				var atxProof types.AtxProof
				for i, a := range []*types.VerifiedActivationTx{prev, atx} {
					atxProof.Messages[i] = types.AtxProofMsg{
						InnerMsg: types.ATXMetadata{
							PublishEpoch: a.PublishEpoch,
							MsgHash:      types.BytesToHash(a.HashInnerBytes()),
						},
						SmesherID: a.SmesherID,
						Signature: a.Signature,
					}
				}
				proof = &types.MalfeasanceProof{
					Layer: atx.PublishEpoch.FirstLayer(),
					Proof: types.Proof{
						Type: types.MultipleATXs,
						Data: &atxProof,
					},
				}
				if err = h.cdb.AddMalfeasanceProof(atx.SmesherID, proof, dbtx); err != nil {
					return fmt.Errorf("adding malfeasance proof: %w", err)
				}
				h.log.WithContext(ctx).With().Warning("smesher produced more than one atx in the same epoch",
					log.Stringer("smesher", atx.SmesherID),
					log.Object("prev", prev),
					log.Object("curr", atx),
				)
			}
		}
		if err = atxs.Add(dbtx, atx); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("add atx to db: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("store atx: %w", err)
	}

	// notify subscribers
	if ch, found := h.atxChannels[atx.ID()]; found {
		close(ch.ch)
		delete(h.atxChannels, atx.ID())
	}

	h.log.WithContext(ctx).With().Debug("finished storing atx in epoch", atx.ID(), atx.PublishEpoch)

	// broadcast malfeasance proof last as the verification of the proof will take place
	// in the same goroutine
	if proof != nil {
		gossip := types.MalfeasanceGossip{
			MalfeasanceProof: *proof,
		}
		encodedProof, err := codec.Encode(&gossip)
		if err != nil {
			h.log.Fatal("failed to encode MalfeasanceGossip", log.Err(err))
		}
		if err = h.publisher.Publish(ctx, pubsub.MalfeasanceProof, encodedProof); err != nil {
			h.log.With().Error("failed to broadcast malfeasance proof", log.Err(err))
			return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
		}
		return errMaliciousATX
	}
	return nil
}

// GetEpochAtxs returns all valid ATXs received in the epoch epochID.
func (h *Handler) GetEpochAtxs(epochID types.EpochID) (ids []types.ATXID, err error) {
	ids, err = atxs.GetIDsByEpoch(h.cdb, epochID)
	h.log.With().Debug("returned epoch atxs", epochID,
		log.Int("count", len(ids)),
		log.String("atxs", fmt.Sprint(ids)))
	return
}

// GetPosAtxID returns the best (highest layer id), currently known to this node, pos atx id.
func (h *Handler) GetPosAtxID() (types.ATXID, error) {
	id, err := atxs.GetAtxIDWithMaxHeight(h.cdb)
	if err != nil {
		return types.EmptyATXID, fmt.Errorf("failed to get positioning atx: %w", err)
	}
	return id, nil
}

// HandleAtxData handles atxs received either by gossip or sync.
func (h *Handler) HandleAtxData(ctx context.Context, peer p2p.Peer, data []byte) error {
	err := h.HandleGossipAtx(ctx, peer, data)
	if errors.Is(err, errKnownAtx) {
		return nil
	}
	return err
}

func (h *Handler) registerHashes(atx *types.ActivationTx, peer p2p.Peer) {
	hashes := map[types.Hash32]struct{}{}
	for _, id := range []types.ATXID{atx.PositioningATX, atx.PrevATXID} {
		if id != types.EmptyATXID && id != h.goldenATXID {
			hashes[id.Hash32()] = struct{}{}
		}
	}
	hashes[atx.GetPoetProofRef()] = struct{}{}
	h.fetcher.RegisterPeerHashes(peer, maps.Keys(hashes))
}

// HandleGossipAtx handles the atx gossip data channel.
func (h *Handler) HandleGossipAtx(ctx context.Context, peer p2p.Peer, msg []byte) error {
	err := h.handleGossipAtx(ctx, peer, msg)
	if err != nil && !errors.Is(err, errMalformedData) && !errors.Is(err, errKnownAtx) {
		h.log.WithContext(ctx).With().Warning("failed to process atx gossip",
			log.Stringer("sender", peer),
			log.Err(err),
		)
	}
	return err
}

func (h *Handler) handleGossipAtx(ctx context.Context, peer p2p.Peer, msg []byte) error {
	receivedTime := time.Now()
	var atx types.ActivationTx
	if err := codec.Decode(msg, &atx); err != nil {
		return fmt.Errorf("%w: %v", errMalformedData, err)
	}

	epochStart := h.clock.LayerToTime(atx.PublishEpoch.FirstLayer())
	poetRoundEnd := epochStart.Add(h.poetCfg.PhaseShift - h.poetCfg.CycleGap)
	latency := receivedTime.Sub(poetRoundEnd)
	metrics.ReportMessageLatency(pubsub.AtxProtocol, pubsub.AtxProtocol, latency)

	atx.SetReceived(receivedTime.Local())
	if err := atx.Initialize(); err != nil {
		return fmt.Errorf("failed to derive ID from atx: %w", err)
	}

	if !h.edVerifier.Verify(signing.ATX, atx.SmesherID, atx.SignedBytes(), atx.Signature) {
		return fmt.Errorf("failed to verify atx signature: %w", errMalformedData)
	}

	logger := h.log.WithContext(ctx).WithFields(atx.ID())
	existing, _ := h.cdb.GetAtxHeader(atx.ID())
	if existing != nil {
		logger.With().Debug("received known atx")
		return fmt.Errorf("%w atx %s", errKnownAtx, atx.ID())
	}

	if atx.NIPost == nil {
		return fmt.Errorf("nil nipst in gossip for atx %s", atx.ShortString())
	}

	h.registerHashes(&atx, peer)
	if err := h.fetcher.GetPoetProof(ctx, atx.GetPoetProofRef()); err != nil {
		return fmt.Errorf("received atx (%v) with syntactically invalid or missing PoET proof (%x): %v",
			atx.ShortString(), atx.GetPoetProofRef().ShortString(), err)
	}

	if err := h.FetchAtxReferences(ctx, &atx); err != nil {
		return fmt.Errorf("received atx (%v) with missing references of prev or pos id %v, %v, %v",
			atx.ID().ShortString(), atx.PrevATXID.ShortString(), atx.PositioningATX.ShortString(), log.Err(err))
	}

	vAtx, err := h.SyntacticallyValidateAtx(ctx, &atx)
	if err != nil {
		return fmt.Errorf("received syntactically invalid atx %v: %v", atx.ShortString(), err)
	}
	err = h.ProcessAtx(ctx, vAtx)
	if err != nil {
		return fmt.Errorf("cannot process atx %v: %v", atx.ShortString(), err)
		// TODO(anton): blacklist peer
	}
	header, err := h.cdb.GetAtxHeader(vAtx.ID())
	if err != nil {
		return fmt.Errorf("get header for processed atx %s: %w", vAtx.ID(), err)
	}
	for _, r := range h.atxReceivers {
		r.OnAtx(header)
	}
	events.ReportNewActivation(vAtx)
	logger.With().Info("new atx", log.Inline(vAtx), log.Int("size", len(msg)))
	return nil
}

// FetchAtxReferences fetches positioning and prev atxs from peers if they are not found in db.
func (h *Handler) FetchAtxReferences(ctx context.Context, atx *types.ActivationTx) error {
	logger := h.log.WithContext(ctx)
	var atxIDs []types.ATXID
	if atx.PositioningATX != types.EmptyATXID && atx.PositioningATX != h.goldenATXID {
		logger.With().Debug("going to fetch pos atx", atx.PositioningATX, atx.ID())
		atxIDs = append(atxIDs, atx.PositioningATX)
	}

	if atx.PrevATXID != types.EmptyATXID {
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
