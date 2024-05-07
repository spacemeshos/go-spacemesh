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

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
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
) (*mwire.MalfeasanceProof, error) {
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
) (*types.VerifiedActivationTx, *mwire.MalfeasanceProof, error) {
	var commitmentATX *types.ATXID
	if atx.PrevATXID == types.EmptyATXID {
		if err := h.validateInitialAtx(ctx, atx); err != nil {
			return nil, nil, err
		}
		commitmentATX = atx.CommitmentATX // checked to be non-nil in syntactic validation
	} else {
		var err error
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

	expectedChallengeHash := wire.NIPostChallengeToWireV1(&atx.NIPostChallenge).Hash()
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
		proof := &mwire.MalfeasanceProof{
			Layer: atx.PublishEpoch.FirstLayer(),
			Proof: mwire.Proof{
				Type: mwire.InvalidPostIndex,
				Data: &mwire.InvalidPostIndexProof{
					Atx:        *wire.ActivationTxToWireV1(atx),
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

		// This is not expected to happen very often, so we query the database
		// directly here without using the cache.
		current, err := atxs.NonceByID(h.cdb, prevAtx.ID)
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
func (h *Handler) cacheAtx(ctx context.Context, atx *types.ActivationTxHeader, nonce types.VRFPostIndex) *atxsdata.ATX {
	if !h.atxsdata.IsEvicted(atx.TargetEpoch()) {
		malicious, err := h.cdb.IsMalicious(atx.NodeID)
		if err != nil {
			h.log.With().Error("failed is malicious read", log.Err(err), log.Context(ctx))
			return nil
		}
		return h.atxsdata.AddFromHeader(atx, nonce, malicious)
	}
	return nil
}

// checkDoublePublish verifies if a node has already published an ATX in the same epoch.
func (h *Handler) checkDoublePublish(
	ctx context.Context,
	tx sql.Executor,
	atx *types.VerifiedActivationTx,
) (*mwire.MalfeasanceProof, error) {
	prev, err := atxs.GetByEpochAndNodeID(tx, atx.PublishEpoch, atx.SmesherID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, err
	}

	// do ID check to be absolutely sure.
	if prev == nil || prev.ID() == atx.ID() {
		return nil, nil
	}
	if _, ok := h.signers[atx.SmesherID]; ok {
		// if we land here we tried to publish 2 ATXs in the same epoch
		// don't punish ourselves but fail validation and thereby the handling of the incoming ATX
		return nil, fmt.Errorf("%s already published an ATX in epoch %d", atx.SmesherID.ShortString(), atx.PublishEpoch)
	}

	var atxProof mwire.AtxProof
	for i, a := range []*types.VerifiedActivationTx{prev, atx} {
		atxProof.Messages[i] = mwire.AtxProofMsg{
			InnerMsg: types.ATXMetadata{
				PublishEpoch: a.PublishEpoch,
				MsgHash:      wire.ActivationTxToWireV1(a.ActivationTx).HashInnerBytes(),
			},
			SmesherID: a.SmesherID,
			Signature: a.Signature,
		}
	}
	proof := &mwire.MalfeasanceProof{
		Layer: atx.PublishEpoch.FirstLayer(),
		Proof: mwire.Proof{
			Type: mwire.MultipleATXs,
			Data: &atxProof,
		},
	}
	encoded, err := codec.Encode(proof)
	if err != nil {
		h.log.With().Panic("failed to encode malfeasance proof", log.Err(err))
	}
	if err := identities.SetMalicious(tx, atx.SmesherID, encoded, time.Now()); err != nil {
		return nil, fmt.Errorf("add malfeasance proof: %w", err)
	}

	h.log.WithContext(ctx).With().Warning("smesher produced more than one atx in the same epoch",
		log.Stringer("smesher", atx.SmesherID),
		log.Object("prev", prev),
		log.Object("curr", atx),
	)

	return proof, nil
}

// checkWrongPrevAtx verifies if the previous ATX referenced in the ATX is correct.
func (h *Handler) checkWrongPrevAtx(
	ctx context.Context,
	tx sql.Executor,
	atx *types.VerifiedActivationTx,
) (*mwire.MalfeasanceProof, error) {
	prevID, err := atxs.PrevIDByNodeID(tx, atx.SmesherID, atx.PublishEpoch)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("get last atx by node id: %w", err)
	}
	if prevID == atx.PrevATXID {
		return nil, nil
	}

	if _, ok := h.signers[atx.SmesherID]; ok {
		// if we land here we tried to publish an ATX with a wrong prevATX
		h.log.WithContext(ctx).With().Warning(
			"Node produced an ATX with a wrong prevATX. This can happened when the node wasn't synced when "+
				"registering at PoET",
			log.Stringer("smesher", atx.SmesherID),
			log.ShortStringer("expected", prevID),
			log.ShortStringer("actual", atx.PrevATXID),
		)
		return nil, fmt.Errorf("%s referenced incorrect previous ATX", atx.SmesherID.ShortString())
	}

	// check if atx.PrevATXID is actually the last published ATX by the same node
	prev, err := atxs.Get(tx, prevID)
	if err != nil {
		return nil, fmt.Errorf("get prev atx: %w", err)
	}

	// if atx references a previous ATX that is not the last ATX by the same node, there must be at least one
	// atx published between prevATX and the current epoch
	var atx2 *types.VerifiedActivationTx
	pubEpoch := h.clock.CurrentLayer().GetEpoch()
	for pubEpoch > prev.PublishEpoch {
		id, err := atxs.PrevIDByNodeID(tx, atx.SmesherID, pubEpoch)
		if err != nil {
			return nil, fmt.Errorf("get prev atx id by node id: %w", err)
		}

		atx2, err = atxs.Get(tx, id)
		if err != nil {
			return nil, fmt.Errorf("get prev atx: %w", err)
		}

		if atx.ID() != atx2.ID() && atx.PrevATXID == atx2.PrevATXID {
			// found an ATX that points to the same previous ATX
			break
		}
		pubEpoch = atx2.PublishEpoch
	}

	if atx2 == nil || atx2.PrevATXID != atx.PrevATXID {
		// something went wrong, we couldn't find an ATX that points to the same previous ATX
		// this should never happen since we are checking in other places that all ATXs from the same node
		// form a chain
		return nil, errors.New("failed double previous check: could not find an ATX with same previous ATX")
	}

	proof := &mwire.MalfeasanceProof{
		Layer: atx.PublishEpoch.FirstLayer(),
		Proof: mwire.Proof{
			Type: mwire.InvalidPrevATX,
			Data: &mwire.InvalidPrevATXProof{
				Atx1: *wire.ActivationTxToWireV1(atx.ActivationTx),
				Atx2: *wire.ActivationTxToWireV1(atx2.ActivationTx),
			},
		},
	}

	if err := identities.SetMalicious(tx, atx.SmesherID, codec.MustEncode(proof), time.Now()); err != nil {
		return nil, fmt.Errorf("add malfeasance proof: %w", err)
	}

	h.log.WithContext(ctx).With().Warning("smesher referenced the wrong previous in published ATX",
		log.Stringer("smesher", atx.SmesherID),
		log.ShortStringer("expected", prevID),
		log.ShortStringer("actual", atx.PrevATXID),
	)
	return proof, nil
}

func (h *Handler) checkMalicious(
	ctx context.Context,
	tx *sql.Tx,
	atx *types.VerifiedActivationTx,
) (*mwire.MalfeasanceProof, error) {
	malicious, err := identities.IsMalicious(tx, atx.SmesherID)
	if err != nil {
		return nil, fmt.Errorf("checking if node is malicious: %w", err)
	}
	if malicious {
		return nil, nil
	}
	proof, err := h.checkDoublePublish(ctx, tx, atx)
	if proof != nil || err != nil {
		return proof, err
	}
	return h.checkWrongPrevAtx(ctx, tx, atx)
}

// storeAtx stores an ATX and notifies subscribers of the ATXID.
func (h *Handler) storeAtx(ctx context.Context, atx *types.VerifiedActivationTx) (*mwire.MalfeasanceProof, error) {
	var nonce *types.VRFPostIndex
	var proof *mwire.MalfeasanceProof
	err := h.cdb.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		proof, err = h.checkMalicious(ctx, tx, atx)
		if err != nil {
			return fmt.Errorf("check malicious: %w", err)
		}
		nonce, err = atxs.AddGettingNonce(tx, atx)
		if err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("add atx to db: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store atx: %w", err)
	}
	if nonce == nil {
		return nil, errors.New("no nonce")
	}
	atxs.AtxAdded(h.cdb, atx)
	if proof != nil {
		h.cdb.CacheMalfeasanceProof(atx.SmesherID, proof)
		h.tortoise.OnMalfeasance(atx.SmesherID)
	}
	header := atx.ToHeader()
	added := h.cacheAtx(ctx, header, *nonce)
	h.beacon.OnAtx(header)
	if added != nil {
		h.tortoise.OnAtx(atx.TargetEpoch(), atx.ID(), added)
	}

	h.log.WithContext(ctx).With().Debug("finished storing atx in epoch", atx.ID(), atx.PublishEpoch)

	return proof, nil
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

// HandleGossipAtx handles the atx gossip data channel.
func (h *Handler) HandleGossipAtx(ctx context.Context, peer p2p.Peer, msg []byte) error {
	proof, err := h.handleAtx(ctx, types.EmptyHash32, peer, msg)
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
		gossip := mwire.MalfeasanceGossip{
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
) (*mwire.MalfeasanceProof, error) {
	receivedTime := time.Now()

	var atxOnWire wire.ActivationTxV1
	if err := codec.Decode(msg, &atxOnWire); err != nil {
		return nil, fmt.Errorf("%w: %w", errMalformedData, err)
	}

	atx := wire.ActivationTxFromWireV1(&atxOnWire)

	atx.SetReceived(receivedTime.Local())

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

	proof, err := h.processATX(ctx, expHash, peer, *atx)
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
) (*mwire.MalfeasanceProof, error) {
	if !h.edVerifier.Verify(signing.ATX, atx.SmesherID, wire.ActivationTxToWireV1(&atx).SignedBytes(), atx.Signature) {
		return nil, fmt.Errorf("failed to verify atx signature: %w", errMalformedData)
	}

	existing, _ := h.cdb.GetAtxHeader(atx.ID())
	if existing != nil {
		return nil, fmt.Errorf("%w atx %s", errKnownAtx, atx.ID())
	}

	if err := h.SyntacticallyValidate(ctx, &atx); err != nil {
		return nil, fmt.Errorf("atx %v syntactically invalid: %w", atx.ShortString(), err)
	}

	poetRef, atxIDs := collectAtxDeps(h.goldenATXID, &atx)
	h.registerHashes(peer, poetRef, atxIDs)
	if err := h.fetchReferences(ctx, poetRef, atxIDs); err != nil {
		return nil, fmt.Errorf("fetching references for atx %x: %w", atx.ID(), err)
	}

	vAtx, proof, err := h.SyntacticallyValidateDeps(ctx, &atx)
	if err != nil {
		return nil, fmt.Errorf("atx %v syntactically invalid based on deps: %w", atx.ShortString(), err)
	}

	if proof != nil {
		return proof, err
	}

	if expHash != types.EmptyHash32 && vAtx.ID().Hash32() != expHash {
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
		log.Bool("malicious", proof != nil),
	)
	return proof, err
}

// registerHashes registers that the given peer should be asked for
// the hashes of the poet proof and ATXs.
func (h *Handler) registerHashes(peer p2p.Peer, poetRef types.Hash32, atxIDs []types.ATXID) {
	hashes := make([]types.Hash32, 0, len(atxIDs)+1)
	for _, id := range atxIDs {
		hashes = append(hashes, id.Hash32())
	}
	hashes = append(hashes, types.Hash32(poetRef))
	h.fetcher.RegisterPeerHashes(peer, hashes)
}

// fetchReferences makes sure that the referenced poet proof and ATXs are available.
func (h *Handler) fetchReferences(ctx context.Context, poetRef types.Hash32, atxIDs []types.ATXID) error {
	if err := h.fetcher.GetPoetProof(ctx, poetRef); err != nil {
		return fmt.Errorf("missing poet proof (%s): %w", poetRef.ShortString(), err)
	}

	if len(atxIDs) == 0 {
		return nil
	}

	if err := h.fetcher.GetAtxs(ctx, atxIDs, system.WithoutLimiting()); err != nil {
		return fmt.Errorf("missing atxs %x: %w", atxIDs, err)
	}

	h.log.WithContext(ctx).With().Debug("done fetching references", log.Int("fetched", len(atxIDs)))
	return nil
}

// Collect unique dependencies of an ATX.
// Filters out EmptyATXID and the golden ATX.
func collectAtxDeps(goldenAtxId types.ATXID, atx *types.ActivationTx) (types.Hash32, []types.ATXID) {
	ids := []types.ATXID{atx.PrevATXID, atx.PositioningATX}
	if atx.CommitmentATX != nil {
		ids = append(ids, *atx.CommitmentATX)
	}

	filtered := make(map[types.ATXID]struct{})
	for _, id := range ids {
		if id != types.EmptyATXID && id != goldenAtxId {
			filtered[id] = struct{}{}
		}
	}

	return atx.GetPoetProofRef(), maps.Keys(filtered)
}
