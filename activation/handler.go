package activation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errKnownAtx      = errors.New("known atx")
	errMalformedData = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	errWrongHash     = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	errMaliciousATX  = errors.New("malicious atx")
)

// Handler processes the atxs received from all nodes and their validity status.
type Handler struct {
	local           p2p.Peer
	cdb             *datastore.CachedDB
	atxsdata        *atxsdata.Data
	edVerifier      *signing.EdVerifier
	clock           layerClock
	publisher       pubsub.Publisher
	tickSize        uint64
	goldenATXID     types.ATXID
	nipostValidator nipostValidator
	beacon          AtxReceiver
	tortoise        system.Tortoise
	log             log.Log
	mu              sync.Mutex
	fetcher         system.Fetcher

	signerMtx sync.Mutex
	signers   map[types.NodeID]*signing.EdSigner

	// inProgress map gathers ATXs that are currently being processed.
	// It's used to avoid processing the same ATX twice.
	inProgress   map[types.ATXID][]chan error
	inProgressMu sync.Mutex
}

// NewHandler returns a data handler for ATX.
func NewHandler(
	local p2p.Peer,
	cdb *datastore.CachedDB,
	atxsdata *atxsdata.Data,
	edVerifier *signing.EdVerifier,
	c layerClock,
	pub pubsub.Publisher,
	fetcher system.Fetcher,
	tickSize uint64,
	goldenATXID types.ATXID,
	nipostValidator nipostValidator,
	beacon AtxReceiver,
	tortoise system.Tortoise,
	log log.Log,
) *Handler {
	return &Handler{
		local:           local,
		cdb:             cdb,
		atxsdata:        atxsdata,
		edVerifier:      edVerifier,
		clock:           c,
		publisher:       pub,
		tickSize:        tickSize,
		goldenATXID:     goldenATXID,
		nipostValidator: nipostValidator,
		log:             log,
		fetcher:         fetcher,
		beacon:          beacon,
		tortoise:        tortoise,

		signers:    make(map[types.NodeID]*signing.EdSigner),
		inProgress: make(map[types.ATXID][]chan error),
	}
}

func (h *Handler) Register(sig *signing.EdSigner) {
	h.signerMtx.Lock()
	defer h.signerMtx.Unlock()
	if _, exists := h.signers[sig.NodeID()]; exists {
		h.log.With().Error("signing key already registered", log.ShortStringer("id", sig.NodeID()))
		return
	}

	h.log.With().Info("registered signing key", log.ShortStringer("id", sig.NodeID()))
	h.signers[sig.NodeID()] = sig
}

// processVerifiedATX validates the active set size declared in the ATX, and contextually
// validates the ATX according to ATX validation rules. It then stores the ATX with flag
// set to validity of the ATX.
//
// ATXs received as input must be already syntactically valid. Only contextual validation
// is performed.
func (h *Handler) processVerifiedATX(
	ctx context.Context,
	atx *types.VerifiedActivationTx,
) (*types.MalfeasanceProof, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	existingATX, _ := h.cdb.GetAtxHeader(atx.ID())
	if existingATX != nil { // Already processed
		return nil, nil
	}
	h.log.WithContext(ctx).With().Debug("processing atx",
		atx.ID(),
		atx.PublishEpoch,
		log.Stringer("smesher", atx.SmesherID),
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

	proof, err := h.storeAtx(ctx, atx)
	if err != nil {
		return nil, fmt.Errorf("cannot store atx %s: %w", atx.ShortString(), err)
	}
	return proof, err
}

func (h *Handler) SyntacticallyValidate(ctx context.Context, atx *types.ActivationTx) error {
	if atx.NIPost == nil {
		return fmt.Errorf("nil nipst for atx %s", atx.ShortString())
	}
	current := h.clock.CurrentLayer().GetEpoch()
	if atx.PublishEpoch > current+1 {
		return fmt.Errorf("atx publish epoch is too far in the future: %d > %d", atx.PublishEpoch, current+1)
	}
	if atx.PositioningATX == types.EmptyATXID {
		return errors.New("empty positioning atx")
	}

	switch {
	case atx.PrevATXID == types.EmptyATXID:
		if atx.InitialPost == nil {
			return errors.New("no prev atx declared, but initial post is not included")
		}
		if atx.InnerActivationTx.NodeID == nil {
			return errors.New("no prev atx declared, but node id is missing")
		}
		if atx.VRFNonce == nil {
			return errors.New("no prev atx declared, but vrf nonce is missing")
		}
		if atx.CommitmentATX == nil {
			return errors.New("no prev atx declared, but commitment atx is missing")
		}
		if *atx.CommitmentATX == types.EmptyATXID {
			return errors.New("empty commitment atx")
		}
		if atx.Sequence != 0 {
			return errors.New("no prev atx declared, but sequence number not zero")
		}

		// Use the NIPost's Post metadata, while overriding the challenge to a zero challenge,
		// as expected from the initial Post.
		initialPostMetadata := *atx.NIPost.PostMetadata
		initialPostMetadata.Challenge = shared.ZeroChallenge
		if err := h.nipostValidator.VRFNonce(
			atx.SmesherID, *atx.CommitmentATX, atx.VRFNonce, &initialPostMetadata, atx.NumUnits,
		); err != nil {
			return fmt.Errorf("invalid vrf nonce: %w", err)
		}
		if err := h.nipostValidator.Post(
			ctx, atx.SmesherID, *atx.CommitmentATX, atx.InitialPost, &initialPostMetadata, atx.NumUnits,
		); err != nil {
			return fmt.Errorf("invalid initial post: %w", err)
		}
	default:
		if atx.InnerActivationTx.NodeID != nil {
			return errors.New("prev atx declared, but node id is included")
		}
		if atx.InitialPost != nil {
			return errors.New("prev atx declared, but initial post is included")
		}
		if atx.CommitmentATX != nil {
			return errors.New("prev atx declared, but commitment atx is included")
		}
	}
	return nil
}

func (h *Handler) SyntacticallyValidateDeps(
	ctx context.Context,
	atx *types.ActivationTx,
) (*types.VerifiedActivationTx, *types.MalfeasanceProof, error) {
	var (
		commitmentATX *types.ATXID
		err           error
	)
	if atx.PrevATXID == types.EmptyATXID {
		if err := h.validateInitialAtx(ctx, atx); err != nil {
			return nil, nil, err
		}
		commitmentATX = atx.CommitmentATX
	} else {
		commitmentATX, err = h.getCommitmentAtx(atx)
		if err != nil {
			return nil, nil, fmt.Errorf("commitment atx for %s not found: %w", atx.SmesherID, err)
		}
		if err := h.validateNonInitialAtx(ctx, atx, *commitmentATX); err != nil {
			return nil, nil, err
		}
	}

	if err := h.nipostValidator.PositioningAtx(atx.PositioningATX, h.cdb, h.goldenATXID, atx.PublishEpoch); err != nil {
		return nil, nil, err
	}

	var baseTickHeight uint64
	if atx.PositioningATX != h.goldenATXID {
		posAtx, _ := h.cdb.GetAtxHeader(atx.PositioningATX) // cannot fail as pos atx is already verified
		baseTickHeight = posAtx.TickHeight()
	}

	expectedChallengeHash := atx.NIPostChallenge.Hash()
	h.log.WithContext(ctx).
		With().
		Info("validating nipost", log.String("expected_challenge_hash", expectedChallengeHash.String()), atx.ID())

	leaves, err := h.nipostValidator.NIPost(
		ctx,
		atx.SmesherID,
		*commitmentATX,
		atx.NIPost,
		expectedChallengeHash,
		atx.NumUnits,
		PostSubset([]byte(h.local)), // use the local peer ID as seed for random subset
	)
	var invalidIdx *verifying.ErrInvalidIndex
	if errors.As(err, &invalidIdx) {
		h.log.WithContext(ctx).With().Info("ATX with invalid post index", atx.ID(), log.Int("index", invalidIdx.Index))
		proof := &types.MalfeasanceProof{
			Layer: atx.PublishEpoch.FirstLayer(),
			Proof: types.Proof{
				Type: types.InvalidPostIndex,
				Data: &types.InvalidPostIndexProof{
					Atx:        *atx,
					InvalidIdx: uint32(invalidIdx.Index),
				},
			},
		}
		encodedProof := codec.MustEncode(proof)
		if err := identities.SetMalicious(h.cdb, atx.SmesherID, encodedProof, time.Now()); err != nil {
			return nil, nil, fmt.Errorf("adding malfeasance proof: %w", err)
		}
		h.cdb.CacheMalfeasanceProof(atx.SmesherID, proof)
		h.tortoise.OnMalfeasance(atx.SmesherID)
		return nil, proof, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("invalid nipost: %w", err)
	}
	if h.nipostValidator.IsVerifyingFullPost() {
		atx.SetValidity(types.Valid)
	}
	vAtx, err := atx.Verify(baseTickHeight, leaves/h.tickSize)
	return vAtx, nil, err
}

func (h *Handler) validateInitialAtx(ctx context.Context, atx *types.ActivationTx) error {
	if err := h.nipostValidator.InitialNIPostChallenge(&atx.NIPostChallenge, h.cdb, h.goldenATXID); err != nil {
		return err
	}
	atx.SetEffectiveNumUnits(atx.NumUnits)
	return nil
}

func (h *Handler) validateNonInitialAtx(ctx context.Context, atx *types.ActivationTx, commitmentATX types.ATXID) error {
	if err := h.nipostValidator.NIPostChallenge(&atx.NIPostChallenge, h.cdb, atx.SmesherID); err != nil {
		return err
	}

	prevAtx, err := h.cdb.GetAtxHeader(atx.PrevATXID)
	if err != nil {
		return err
	}

	nonce := atx.VRFNonce
	if atx.NumUnits > prevAtx.NumUnits && nonce == nil {
		h.log.WithContext(ctx).With().Info("post size increased without new vrf Nonce, re-validating current nonce",
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
			return fmt.Errorf("invalid vrf nonce: %w", err)
		}
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
		return fmt.Errorf(
			"no prev atx reported, but other atx with same node id (%v) found: %v",
			atx.SmesherID,
			lastAtx.ShortString(),
		)
	}

	if err == nil && atx.PrevATXID != lastAtx {
		// last atx referenced does not equal last ATX seen from node
		return errors.New("last atx is not the one referenced")
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

// cacheAtx caches the atx in the atxsdata cache.
// Returns true if the atx was cached, false otherwise.
func (h *Handler) cacheAtx(ctx context.Context, atx *types.ActivationTxHeader) *atxsdata.ATX {
	if !h.atxsdata.IsEvicted(atx.TargetEpoch()) {
		nonce, err := h.cdb.VRFNonce(atx.NodeID, atx.TargetEpoch())
		if err != nil {
			h.log.With().Error("failed vrf nonce read", log.Err(err), log.Context(ctx))
			return nil
		}
		malicious, err := h.cdb.IsMalicious(atx.NodeID)
		if err != nil {
			h.log.With().Error("failed is malicious read", log.Err(err), log.Context(ctx))
			return nil
		}
		return h.atxsdata.AddFromHeader(atx, nonce, malicious)
	}
	return nil
}

// storeAtx stores an ATX and notifies subscribers of the ATXID.
func (h *Handler) storeAtx(ctx context.Context, atx *types.VerifiedActivationTx) (*types.MalfeasanceProof, error) {
	malicious, err := h.cdb.IsMalicious(atx.SmesherID)
	if err != nil {
		return nil, fmt.Errorf("checking if node is malicious: %w", err)
	}
	var proof *types.MalfeasanceProof
	if err := h.cdb.WithTx(ctx, func(tx *sql.Tx) error {
		if malicious {
			if err := atxs.Add(tx, atx); err != nil && !errors.Is(err, sql.ErrObjectExists) {
				return fmt.Errorf("add atx to db: %w", err)
			}
			return nil
		}

		prev, err := atxs.GetByEpochAndNodeID(tx, atx.PublishEpoch, atx.SmesherID)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return err
		}

		// do ID check to be absolutely sure.
		if prev != nil && prev.ID() != atx.ID() {
			if _, ok := h.signers[atx.SmesherID]; ok {
				// if we land here we tried to publish 2 ATXs in the same epoch
				// don't punish ourselves but fail validation and thereby the handling of the incoming ATX
				return fmt.Errorf("%s already published an ATX in epoch %d", atx.SmesherID.ShortString(),
					atx.PublishEpoch,
				)
			}

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
			encoded, err := codec.Encode(proof)
			if err != nil {
				h.log.With().Panic("failed to encode malfeasance proof", log.Err(err))
			}
			if err := identities.SetMalicious(tx, atx.SmesherID, encoded, time.Now()); err != nil {
				return fmt.Errorf("add malfeasance proof: %w", err)
			}

			h.log.WithContext(ctx).With().Warning("smesher produced more than one atx in the same epoch",
				log.Stringer("smesher", atx.SmesherID),
				log.Object("prev", prev),
				log.Object("curr", atx),
			)
		}

		if err := atxs.Add(tx, atx); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("add atx to db: %w", err)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("store atx: %w", err)
	}
	atxs.AtxAdded(h.cdb, atx)
	if proof != nil {
		h.cdb.CacheMalfeasanceProof(atx.SmesherID, proof)
		h.tortoise.OnMalfeasance(atx.SmesherID)
	}
	header := atx.ToHeader()
	added := h.cacheAtx(ctx, header)
	h.beacon.OnAtx(header)
	if added != nil {
		h.tortoise.OnAtx(atx.TargetEpoch(), atx.ID(), added)
	}

	h.log.WithContext(ctx).With().Debug("finished storing atx in epoch", atx.ID(), atx.PublishEpoch)

	return proof, nil
}

// GetEpochAtxs returns all valid ATXs received in the epoch epochID.
func (h *Handler) GetEpochAtxs(ctx context.Context, epochID types.EpochID) (ids []types.ATXID, err error) {
	ids, err = atxs.GetIDsByEpoch(ctx, h.cdb, epochID)
	h.log.With().Debug("returned epoch atxs", epochID,
		log.Int("count", len(ids)),
		log.String("atxs", fmt.Sprint(ids)))
	return
}

// HandleSyncedAtx handles atxs received by sync.
func (h *Handler) HandleSyncedAtx(ctx context.Context, expHash types.Hash32, peer p2p.Peer, data []byte) error {
	_, err := h.handleAtx(ctx, expHash, peer, data)
	if err != nil && !errors.Is(err, errMalformedData) && !errors.Is(err, errKnownAtx) {
		h.log.WithContext(ctx).With().Warning("failed to process synced atx",
			log.Stringer("sender", peer),
			log.Err(err),
		)
	}
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
	proof, err := h.handleAtx(ctx, types.Hash32{}, peer, msg)
	if err != nil && !errors.Is(err, errMalformedData) && !errors.Is(err, errKnownAtx) {
		h.log.WithContext(ctx).With().Warning("failed to process atx gossip",
			log.Stringer("sender", peer),
			log.Err(err),
		)
	}
	if errors.Is(err, errKnownAtx) && peer == h.local {
		return nil
	}

	// broadcast malfeasance proof last as the verification of the proof will take place
	// in the same goroutine
	if proof != nil {
		gossip := types.MalfeasanceGossip{
			MalfeasanceProof: *proof,
		}
		encodedProof := codec.MustEncode(&gossip)
		if err = h.publisher.Publish(ctx, pubsub.MalfeasanceProof, encodedProof); err != nil {
			h.log.With().Error("failed to broadcast malfeasance proof", log.Err(err))
			return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
		}
		return errMaliciousATX
	}
	return err
}

func (h *Handler) handleAtx(
	ctx context.Context,
	expHash types.Hash32,
	peer p2p.Peer,
	msg []byte,
) (*types.MalfeasanceProof, error) {
	receivedTime := time.Now()
	var atx types.ActivationTx
	if err := codec.Decode(msg, &atx); err != nil {
		return nil, fmt.Errorf("%w: %w", errMalformedData, err)
	}
	atx.SetReceived(receivedTime.Local())
	if err := atx.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to derive ID from atx: %w", err)
	}

	// Check if processing is already in progress
	h.inProgressMu.Lock()
	if sub, ok := h.inProgress[atx.ID()]; ok {
		ch := make(chan error, 1)
		h.inProgress[atx.ID()] = append(sub, ch)
		h.inProgressMu.Unlock()
		h.log.WithContext(ctx).With().Debug("atx is already being processed. waiting for result", atx.ID())
		select {
		case err := <-ch:
			h.log.WithContext(ctx).With().Debug("atx processed in other task", atx.ID(), log.Err(err))
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	h.inProgress[atx.ID()] = []chan error{}
	h.inProgressMu.Unlock()
	h.log.WithContext(ctx).With().Info("handling incoming atx", atx.ID(), log.Int("size", len(msg)))

	proof, err := h.processATX(ctx, expHash, peer, atx)
	h.inProgressMu.Lock()
	defer h.inProgressMu.Unlock()
	for _, ch := range h.inProgress[atx.ID()] {
		ch <- err
		close(ch)
	}
	delete(h.inProgress, atx.ID())
	return proof, err
}

func (h *Handler) processATX(
	ctx context.Context,
	expHash types.Hash32,
	peer p2p.Peer,
	atx types.ActivationTx,
) (*types.MalfeasanceProof, error) {
	if !h.edVerifier.Verify(signing.ATX, atx.SmesherID, atx.SignedBytes(), atx.Signature) {
		return nil, fmt.Errorf("failed to verify atx signature: %w", errMalformedData)
	}

	existing, _ := h.cdb.GetAtxHeader(atx.ID())
	if existing != nil {
		return nil, fmt.Errorf("%w atx %s", errKnownAtx, atx.ID())
	}

	if err := h.SyntacticallyValidate(ctx, &atx); err != nil {
		return nil, fmt.Errorf("atx %v syntactically invalid: %w", atx.ShortString(), err)
	}

	h.registerHashes(&atx, peer)
	if err := h.FetchReferences(ctx, &atx); err != nil {
		return nil, err
	}

	vAtx, proof, err := h.SyntacticallyValidateDeps(ctx, &atx)
	if err != nil {
		return nil, fmt.Errorf("atx %v syntactically invalid based on deps: %w", atx.ShortString(), err)
	}

	if proof != nil {
		return proof, err
	}

	if expHash != (types.Hash32{}) && vAtx.ID().Hash32() != expHash {
		return nil, fmt.Errorf(
			"%w: atx want %s, got %s",
			errWrongHash,
			expHash.ShortString(),
			vAtx.ID().Hash32().ShortString(),
		)
	}

	proof, err = h.processVerifiedATX(ctx, vAtx)
	if err != nil {
		return nil, fmt.Errorf("cannot process atx %v: %w", atx.ShortString(), err)
	}
	events.ReportNewActivation(vAtx)
	h.log.WithContext(ctx).With().Info(
		"new atx", log.Inline(vAtx),
		log.Bool("malicious", proof != nil))
	return proof, err
}

// FetchReferences fetches referenced ATXs from peers if they are not found in db.
func (h *Handler) FetchReferences(ctx context.Context, atx *types.ActivationTx) error {
	if err := h.fetcher.GetPoetProof(ctx, atx.GetPoetProofRef()); err != nil {
		return fmt.Errorf("atx (%s) missing poet proof (%s): %w",
			atx.ShortString(), atx.GetPoetProofRef().ShortString(), err,
		)
	}

	atxIDs := make(map[types.ATXID]struct{}, 3)
	if atx.PositioningATX != types.EmptyATXID && atx.PositioningATX != h.goldenATXID {
		atxIDs[atx.PositioningATX] = struct{}{}
	}

	if atx.PrevATXID != types.EmptyATXID {
		atxIDs[atx.PrevATXID] = struct{}{}
	}
	if atx.CommitmentATX != nil && *atx.CommitmentATX != h.goldenATXID {
		atxIDs[*atx.CommitmentATX] = struct{}{}
	}

	if len(atxIDs) == 0 {
		return nil
	}

	if err := h.fetcher.GetAtxs(ctx, maps.Keys(atxIDs), system.WithoutLimiting()); err != nil {
		dbg := fmt.Sprintf("prev %v pos %v commit %v", atx.PrevATXID, atx.PositioningATX, atx.CommitmentATX)
		return fmt.Errorf("fetch referenced atxs (%s): %w", dbg, err)
	}

	h.log.WithContext(ctx).With().Debug("done fetching references for atx",
		atx.ID(),
		log.Int("num fetched", len(atxIDs)),
	)
	return nil
}
