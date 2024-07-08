package activation

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"sync"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"go.uber.org/zap"
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
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/system"
)

type nipostValidatorV1 interface {
	InitialNIPostChallengeV1(challenge *wire.NIPostChallengeV1, atxs atxProvider, goldenATXID types.ATXID) error
	NIPostChallengeV1(challenge *wire.NIPostChallengeV1, previous *types.ActivationTx, nodeID types.NodeID) error
	NIPost(
		ctx context.Context,
		nodeId types.NodeID,
		commitmentAtxId types.ATXID,
		NIPost *types.NIPost,
		expectedChallenge types.Hash32,
		numUnits uint32,
		opts ...validatorOption,
	) (uint64, error)

	NumUnits(cfg *PostConfig, numUnits uint32) error

	IsVerifyingFullPost() bool

	Post(
		ctx context.Context,
		nodeId types.NodeID,
		commitmentAtxId types.ATXID,
		post *types.Post,
		metadata *types.PostMetadata,
		numUnits uint32,
		opts ...validatorOption,
	) error

	VRFNonce(
		nodeId types.NodeID,
		commitmentAtxId types.ATXID,
		vrfNonce, labelsPerUnit uint64,
		numUnits uint32,
	) error
	PositioningAtx(id types.ATXID, atxs atxProvider, goldenATXID types.ATXID, pubepoch types.EpochID) error
}

// HandlerV1 processes ATXs version 1.
type HandlerV1 struct {
	local           p2p.Peer
	cdb             *datastore.CachedDB
	atxsdata        *atxsdata.Data
	edVerifier      *signing.EdVerifier
	clock           layerClock
	tickSize        uint64
	goldenATXID     types.ATXID
	nipostValidator nipostValidatorV1
	beacon          AtxReceiver
	tortoise        system.Tortoise
	logger          *zap.Logger
	fetcher         system.Fetcher

	signerMtx sync.Mutex
	signers   map[types.NodeID]*signing.EdSigner
}

func (h *HandlerV1) Register(sig *signing.EdSigner) {
	h.signerMtx.Lock()
	defer h.signerMtx.Unlock()
	if _, exists := h.signers[sig.NodeID()]; exists {
		h.logger.Error("signing key already registered", log.ZShortStringer("id", sig.NodeID()))
		return
	}

	h.logger.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
	h.signers[sig.NodeID()] = sig
}

func (h *HandlerV1) syntacticallyValidate(ctx context.Context, atx *wire.ActivationTxV1) error {
	if atx.NIPost == nil {
		return fmt.Errorf("nil nipost for atx %s", atx.ID())
	}
	current := h.clock.CurrentLayer().GetEpoch()
	if atx.PublishEpoch > current+1 {
		return fmt.Errorf("atx publish epoch is too far in the future: %d > %d", atx.PublishEpoch, current+1)
	}
	if atx.PositioningATXID == types.EmptyATXID {
		return errors.New("empty positioning atx")
	}

	switch {
	case atx.PrevATXID == types.EmptyATXID:
		if atx.InitialPost == nil {
			return errors.New("no prev atx declared, but initial post is not included")
		}
		if atx.NodeID == nil {
			return errors.New("no prev atx declared, but node id is missing")
		}
		if atx.VRFNonce == nil {
			return errors.New("no prev atx declared, but vrf nonce is missing")
		}
		if atx.CommitmentATXID == nil {
			return errors.New("no prev atx declared, but commitment atx is missing")
		}
		if *atx.CommitmentATXID == types.EmptyATXID {
			return errors.New("empty commitment atx")
		}
		if atx.Sequence != 0 {
			return errors.New("no prev atx declared, but sequence number not zero")
		}

		// Use the NIPost's Post metadata, while overriding the challenge to a zero challenge,
		// as expected from the initial Post.
		initialPostMetadata := types.PostMetadata{
			Challenge:     shared.ZeroChallenge,
			LabelsPerUnit: atx.NIPost.PostMetadata.LabelsPerUnit,
		}
		if err := h.nipostValidator.VRFNonce(
			atx.SmesherID, *atx.CommitmentATXID, *atx.VRFNonce, initialPostMetadata.LabelsPerUnit, atx.NumUnits,
		); err != nil {
			return fmt.Errorf("invalid vrf nonce: %w", err)
		}
		post := wire.PostFromWireV1(atx.InitialPost)
		if err := h.nipostValidator.Post(
			ctx, atx.SmesherID, *atx.CommitmentATXID, post, &initialPostMetadata, atx.NumUnits,
		); err != nil {
			return fmt.Errorf("invalid initial post: %w", err)
		}
	default:
		if atx.NodeID != nil {
			return errors.New("prev atx declared, but node id is included")
		}
		if atx.InitialPost != nil {
			return errors.New("prev atx declared, but initial post is included")
		}
		if atx.CommitmentATXID != nil {
			return errors.New("prev atx declared, but commitment atx is included")
		}
	}
	return nil
}

// Obtain the commitment ATX ID for the given ATX.
func (h *HandlerV1) commitment(atx *wire.ActivationTxV1) (types.ATXID, error) {
	if atx.PrevATXID == types.EmptyATXID {
		return *atx.CommitmentATXID, nil
	}
	return atxs.CommitmentATX(h.cdb, atx.SmesherID)
}

func (h *HandlerV1) syntacticallyValidateDeps(
	ctx context.Context,
	atx *wire.ActivationTxV1,
) (leaves uint64, effectiveNumUnits uint32, proof *mwire.MalfeasanceProof, err error) {
	commitmentATX, err := h.commitment(atx)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("commitment atx for %s not found: %w", atx.SmesherID, err)
	}

	if atx.PrevATXID == types.EmptyATXID {
		if err := h.nipostValidator.InitialNIPostChallengeV1(&atx.NIPostChallengeV1, h.cdb, h.goldenATXID); err != nil {
			return 0, 0, nil, err
		}
		effectiveNumUnits = atx.NumUnits
	} else {
		previous, err := atxs.Get(h.cdb, atx.PrevATXID)
		if err != nil {
			return 0, 0, nil, fmt.Errorf("fetching previous atx %s: %w", atx.PrevATXID, err)
		}
		if err := h.validateNonInitialAtx(ctx, atx, previous, commitmentATX); err != nil {
			return 0, 0, nil, err
		}
		prevUnits, err := atxs.Units(h.cdb, atx.PrevATXID, atx.SmesherID)
		if err != nil {
			return 0, 0, nil, fmt.Errorf("fetching previous atx units: %w", err)
		}
		effectiveNumUnits = min(prevUnits, atx.NumUnits)
	}

	err = h.nipostValidator.PositioningAtx(atx.PositioningATXID, h.cdb, h.goldenATXID, atx.PublishEpoch)
	if err != nil {
		return 0, 0, nil, err
	}

	expectedChallengeHash := atx.NIPostChallengeV1.Hash()
	h.logger.Info("validating nipost",
		log.ZContext(ctx),
		zap.Stringer("expected_challenge_hash", expectedChallengeHash),
		zap.Stringer("atx_id", atx.ID()),
	)

	leaves, err = h.nipostValidator.NIPost(
		ctx,
		atx.SmesherID,
		commitmentATX,
		wire.NiPostFromWireV1(atx.NIPost),
		expectedChallengeHash,
		atx.NumUnits,
		PostSubset([]byte(h.local)), // use the local peer ID as seed for random subset
	)
	var invalidIdx *verifying.ErrInvalidIndex
	if errors.As(err, &invalidIdx) {
		h.logger.Info("ATX with invalid post index",
			log.ZContext(ctx),
			zap.Stringer("atx_id", atx.ID()),
			zap.Int("index", invalidIdx.Index),
		)
		proof := &mwire.MalfeasanceProof{
			Layer: atx.PublishEpoch.FirstLayer(),
			Proof: mwire.Proof{
				Type: mwire.InvalidPostIndex,
				Data: &mwire.InvalidPostIndexProof{
					Atx:        *atx,
					InvalidIdx: uint32(invalidIdx.Index),
				},
			},
		}
		encodedProof := codec.MustEncode(proof)
		if err := identities.SetMalicious(h.cdb, atx.SmesherID, encodedProof, time.Now()); err != nil {
			return 0, 0, nil, fmt.Errorf("adding malfeasance proof: %w", err)
		}
		h.cdb.CacheMalfeasanceProof(atx.SmesherID, proof)
		h.tortoise.OnMalfeasance(atx.SmesherID)
		return 0, 0, proof, nil
	}
	if err != nil {
		return 0, 0, nil, fmt.Errorf("invalid nipost: %w", err)
	}

	return leaves, effectiveNumUnits, nil, err
}

func (h *HandlerV1) validateNonInitialAtx(
	ctx context.Context,
	atx *wire.ActivationTxV1,
	previous *types.ActivationTx,
	commitment types.ATXID,
) error {
	if err := h.nipostValidator.NIPostChallengeV1(&atx.NIPostChallengeV1, previous, atx.SmesherID); err != nil {
		return err
	}

	needRecheck := atx.VRFNonce != nil || atx.NumUnits > previous.NumUnits
	if atx.VRFNonce == nil {
		atx.VRFNonce = new(uint64)
		*atx.VRFNonce = uint64(previous.VRFNonce)
	}

	if needRecheck {
		h.logger.Info("validating VRF nonce",
			log.ZContext(ctx),
			zap.Stringer("atx_id", atx.ID()),
			zap.Bool("post increased", atx.NumUnits > previous.NumUnits),
			zap.Stringer("smesher", atx.SmesherID),
		)
		err := h.nipostValidator.
			VRFNonce(atx.SmesherID, commitment, *atx.VRFNonce, atx.NIPost.PostMetadata.LabelsPerUnit, atx.NumUnits)
		if err != nil {
			return fmt.Errorf("invalid vrf nonce: %w", err)
		}
	}

	return nil
}

// contextuallyValidateAtx ensures that the previous ATX referenced is the last known ATX for the referenced miner ID.
// If a previous ATX is not referenced, it validates that indeed there's no previous known ATX for that miner ID.
func (h *HandlerV1) contextuallyValidateAtx(atx *wire.ActivationTxV1) error {
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
		h.logger.Error("could not fetch node last atx",
			zap.Stringer("atx_id", atx.ID()),
			zap.Stringer("smesher", atx.SmesherID),
			zap.Error(err),
		)
		return fmt.Errorf("could not fetch node last atx: %w", err)
	}

	return err
}

// cacheAtx caches the atx in the atxsdata cache.
// Returns true if the atx was cached, false otherwise.
func (h *HandlerV1) cacheAtx(ctx context.Context, atx *types.ActivationTx) *atxsdata.ATX {
	if !h.atxsdata.IsEvicted(atx.TargetEpoch()) {
		malicious, err := h.cdb.IsMalicious(atx.SmesherID)
		if err != nil {
			h.logger.Error("failed is malicious read", zap.Error(err), log.ZContext(ctx))
			return nil
		}
		return h.atxsdata.AddFromAtx(atx, malicious)
	}
	return nil
}

// checkDoublePublish verifies if a node has already published an ATX in the same epoch.
func (h *HandlerV1) checkDoublePublish(
	ctx context.Context,
	tx sql.Executor,
	atx *wire.ActivationTxV1,
) (*mwire.MalfeasanceProof, error) {
	prev, err := atxs.GetByEpochAndNodeID(tx, atx.PublishEpoch, atx.SmesherID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, err
	}
	if prev == types.EmptyATXID || prev == atx.ID() {
		// no ATX previously published for this epoch, or we are handling the same ATX again
		return nil, nil
	}

	if _, ok := h.signers[atx.SmesherID]; ok {
		// if we land here we tried to publish 2 ATXs in the same epoch
		// don't punish ourselves but fail validation and thereby the handling of the incoming ATX
		return nil, fmt.Errorf("%s already published an ATX in epoch %d", atx.SmesherID.ShortString(), atx.PublishEpoch)
	}

	prevSignature, err := atxSignature(ctx, tx, prev)
	if err != nil {
		return nil, fmt.Errorf("extracting signature for malfeasance proof: %w", err)
	}

	atxProof := mwire.AtxProof{
		Messages: [2]mwire.AtxProofMsg{{
			InnerMsg: types.ATXMetadata{
				PublishEpoch: atx.PublishEpoch,
				MsgHash:      prev.Hash32(),
			},
			SmesherID: atx.SmesherID,
			Signature: prevSignature,
		}, {
			InnerMsg: types.ATXMetadata{
				PublishEpoch: atx.PublishEpoch,
				MsgHash:      atx.ID().Hash32(),
			},
			SmesherID: atx.SmesherID,
			Signature: atx.Signature,
		}},
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
		h.logger.Panic("failed to encode malfeasance proof", zap.Error(err))
	}
	if err := identities.SetMalicious(tx, atx.SmesherID, encoded, time.Now()); err != nil {
		return nil, fmt.Errorf("add malfeasance proof: %w", err)
	}

	h.logger.Warn("smesher produced more than one atx in the same epoch",
		log.ZContext(ctx),
		zap.Stringer("smesher", atx.SmesherID),
		zap.Stringer("previous", prev),
		zap.Stringer("current", atx.ID()),
	)

	return proof, nil
}

// checkWrongPrevAtx verifies if the previous ATX referenced in the ATX is correct.
func (h *HandlerV1) checkWrongPrevAtx(
	ctx context.Context,
	tx sql.Executor,
	atx *wire.ActivationTxV1,
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
		h.logger.Warn(
			"Node produced an ATX with a wrong prevATX. This can happened when the node wasn't synced when "+
				"registering at PoET",
			log.ZContext(ctx),
			zap.Stringer("smesher", atx.SmesherID),
			log.ZShortStringer("expected", prevID),
			log.ZShortStringer("actual", atx.PrevATXID),
		)
		return nil, fmt.Errorf("%s referenced incorrect previous ATX", atx.SmesherID.ShortString())
	}

	var atx2ID types.ATXID
	if atx.PrevATXID == types.EmptyATXID {
		// if the ATX references an empty previous ATX, we can just take the initial ATX and create a proof
		// that the node referenced the wrong previous ATX
		id, err := atxs.GetFirstIDByNodeID(tx, atx.SmesherID)
		if err != nil {
			return nil, fmt.Errorf("get initial atx id: %w", err)
		}

		atx2ID = id
	} else {
		prev, err := atxs.Get(tx, atx.PrevATXID)
		if err != nil {
			return nil, fmt.Errorf("get prev atx: %w", err)
		}

		// if atx references a previous ATX that is not the last ATX by the same node, there must be at least one
		// atx published between prevATX and the current epoch
		pubEpoch := h.clock.CurrentLayer().GetEpoch()
		for pubEpoch > prev.PublishEpoch {
			id, err := atxs.PrevIDByNodeID(tx, atx.SmesherID, pubEpoch)
			if err != nil {
				return nil, fmt.Errorf("get prev atx id by node id: %w", err)
			}

			atx2, err := atxs.Get(tx, id)
			if err != nil {
				return nil, fmt.Errorf("get prev atx: %w", err)
			}
			if atx.ID() != atx2.ID() && atx.PrevATXID == atx2.PrevATXID {
				// found an ATX that points to the same previous ATX
				atx2ID = id
				break
			}
			pubEpoch = atx2.PublishEpoch
		}
	}

	if atx2ID == types.EmptyATXID {
		// something went wrong, we couldn't find an ATX that points to the same previous ATX
		// this should never happen since we are checking in other places that all ATXs from the same node
		// form a chain
		return nil, errors.New("failed double previous check: could not find an ATX with same previous ATX")
	}

	var blob sql.Blob
	v, err := atxs.LoadBlob(ctx, tx, atx2ID.Bytes(), &blob)
	if err != nil {
		return nil, err
	}
	if v != types.AtxV1 {
		// TODO(mafa): update when V2 is introduced
		return nil, fmt.Errorf("ATX %s with same prev ATX as %s is not version 1", atx2ID, atx.PrevATXID)
	}

	var watx2 wire.ActivationTxV1
	if err := codec.Decode(blob.Bytes, &watx2); err != nil {
		return nil, fmt.Errorf("decoding previous atx: %w", err)
	}

	proof := &mwire.MalfeasanceProof{
		Layer: atx.PublishEpoch.FirstLayer(),
		Proof: mwire.Proof{
			Type: mwire.InvalidPrevATX,
			Data: &mwire.InvalidPrevATXProof{
				Atx1: *atx,
				Atx2: watx2,
			},
		},
	}

	if err := identities.SetMalicious(tx, atx.SmesherID, codec.MustEncode(proof), time.Now()); err != nil {
		return nil, fmt.Errorf("add malfeasance proof: %w", err)
	}

	h.logger.Warn("smesher referenced the wrong previous in published ATX",
		log.ZContext(ctx),
		zap.Stringer("smesher", atx.SmesherID),
		log.ZShortStringer("expected", prevID),
		log.ZShortStringer("actual", atx.PrevATXID),
	)
	return proof, nil
}

func (h *HandlerV1) checkMalicious(
	ctx context.Context,
	tx sql.Transaction,
	watx *wire.ActivationTxV1,
) (*mwire.MalfeasanceProof, error) {
	malicious, err := identities.IsMalicious(tx, watx.SmesherID)
	if err != nil {
		return nil, fmt.Errorf("checking if node is malicious: %w", err)
	}
	if malicious {
		return nil, nil
	}
	proof, err := h.checkDoublePublish(ctx, tx, watx)
	if proof != nil || err != nil {
		return proof, err
	}
	return h.checkWrongPrevAtx(ctx, tx, watx)
}

// storeAtx stores an ATX and notifies subscribers of the ATXID.
func (h *HandlerV1) storeAtx(
	ctx context.Context,
	atx *types.ActivationTx,
	watx *wire.ActivationTxV1,
) (*mwire.MalfeasanceProof, error) {
	var proof *mwire.MalfeasanceProof
	if err := h.cdb.WithTx(ctx, func(tx sql.Transaction) error {
		var err error
		proof, err = h.checkMalicious(ctx, tx, watx)
		if err != nil {
			return fmt.Errorf("check malicious: %w", err)
		}

		err = atxs.Add(tx, atx)
		if err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("add atx to db: %w", err)
		}
		err = atxs.SetUnits(tx, atx.ID(), atx.SmesherID, watx.NumUnits)
		if err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("set atx units: %w", err)
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

	added := h.cacheAtx(ctx, atx)
	h.beacon.OnAtx(atx)
	if added != nil {
		h.tortoise.OnAtx(atx.TargetEpoch(), atx.ID(), added)
	}

	h.logger.Debug("finished storing atx in epoch",
		zap.Stringer("atx_id", atx.ID()),
		zap.Uint32("epoch_id", atx.PublishEpoch.Uint32()),
	)
	return proof, nil
}

func (h *HandlerV1) processATX(
	ctx context.Context,
	peer p2p.Peer,
	watx *wire.ActivationTxV1,
	blob []byte,
	received time.Time,
) (*mwire.MalfeasanceProof, error) {
	if !h.edVerifier.Verify(signing.ATX, watx.SmesherID, watx.SignedBytes(), watx.Signature) {
		return nil, fmt.Errorf("invalid atx signature: %w", errMalformedData)
	}

	existing, _ := h.cdb.GetAtx(watx.ID())
	if existing != nil {
		return nil, fmt.Errorf("%w atx %s", errKnownAtx, watx.ID())
	}

	h.logger.Debug("processing atx",
		log.ZContext(ctx),
		zap.Stringer("atx_id", watx.ID()),
		zap.Uint32("epoch_id", watx.PublishEpoch.Uint32()),
		zap.Stringer("smesherID", watx.SmesherID),
	)

	if err := h.syntacticallyValidate(ctx, watx); err != nil {
		return nil, fmt.Errorf("atx %s syntactically invalid: %w", watx.ID(), err)
	}

	poetRef, atxIDs := collectAtxDeps(h.goldenATXID, watx)
	h.registerHashes(peer, poetRef, atxIDs)
	if err := h.fetchReferences(ctx, poetRef, atxIDs); err != nil {
		return nil, fmt.Errorf("fetching references for atx %s: %w", watx.ID(), err)
	}

	leaves, effectiveNumUnits, proof, err := h.syntacticallyValidateDeps(ctx, watx)
	if err != nil {
		return nil, fmt.Errorf("atx %s syntactically invalid based on deps: %w", watx.ID(), err)
	}
	if proof != nil {
		return proof, nil
	}

	if err := h.contextuallyValidateAtx(watx); err != nil {
		h.logger.Warn("atx is contextually invalid ",
			log.ZContext(ctx),
			zap.Stringer("atx_id", watx.ID()),
			zap.Stringer("smesherID", watx.SmesherID),
			zap.Error(err),
		)
	} else {
		h.logger.Debug("atx is valid", zap.Stringer("atx_id", watx.ID()))
	}

	var baseTickHeight uint64
	if watx.PositioningATXID != h.goldenATXID {
		posAtx, err := h.cdb.GetAtx(watx.PositioningATXID)
		if err != nil {
			return nil, fmt.Errorf("failed to get positioning atx %s: %w", watx.PositioningATXID, err)
		}
		baseTickHeight = posAtx.TickHeight()
	}

	atx := wire.ActivationTxFromWireV1(watx, blob...)
	if h.nipostValidator.IsVerifyingFullPost() {
		atx.SetValidity(types.Valid)
	}
	atx.SetReceived(received)
	atx.NumUnits = effectiveNumUnits
	atx.BaseTickHeight = baseTickHeight
	atx.TickCount = leaves / h.tickSize
	hi, weight := bits.Mul64(uint64(atx.NumUnits), atx.TickCount)
	if hi != 0 {
		return nil, errors.New("atx weight would overflow uint64")
	}
	atx.Weight = weight

	proof, err = h.storeAtx(ctx, atx, watx)
	if err != nil {
		return nil, fmt.Errorf("cannot store atx %s: %w", atx.ShortString(), err)
	}

	events.ReportNewActivation(atx)
	h.logger.Info("new atx",
		log.ZContext(ctx),
		zap.Inline(atx),
		zap.Bool("malicious", proof != nil),
	)
	return proof, err
}

// registerHashes registers that the given peer should be asked for
// the hashes of the poet proof and ATXs.
func (h *HandlerV1) registerHashes(peer p2p.Peer, poetRef types.Hash32, atxIDs []types.ATXID) {
	hashes := make([]types.Hash32, 0, len(atxIDs)+1)
	for _, id := range atxIDs {
		hashes = append(hashes, id.Hash32())
	}
	hashes = append(hashes, types.Hash32(poetRef))
	h.fetcher.RegisterPeerHashes(peer, hashes)
}

// fetchReferences makes sure that the referenced poet proof and ATXs are available.
func (h *HandlerV1) fetchReferences(ctx context.Context, poetRef types.Hash32, atxIDs []types.ATXID) error {
	if err := h.fetcher.GetPoetProof(ctx, poetRef); err != nil {
		return fmt.Errorf("missing poet proof (%s): %w", poetRef.ShortString(), err)
	}

	if len(atxIDs) == 0 {
		return nil
	}

	if err := h.fetcher.GetAtxs(ctx, atxIDs, system.WithoutLimiting()); err != nil {
		return fmt.Errorf("missing atxs %x: %w", atxIDs, err)
	}

	h.logger.Debug("done fetching references",
		log.ZContext(ctx),
		zap.Int("fetched", len(atxIDs)),
	)
	return nil
}

// Collect unique dependencies of an ATX.
// Filters out EmptyATXID and the golden ATX.
func collectAtxDeps(goldenAtxId types.ATXID, atx *wire.ActivationTxV1) (types.Hash32, []types.ATXID) {
	ids := []types.ATXID{atx.PrevATXID, atx.PositioningATXID}
	if atx.CommitmentATXID != nil {
		ids = append(ids, *atx.CommitmentATXID)
	}

	filtered := make(map[types.ATXID]struct{})
	for _, id := range ids {
		if id != types.EmptyATXID && id != goldenAtxId {
			filtered[id] = struct{}{}
		}
	}

	return types.BytesToHash(atx.NIPost.PostMetadata.Challenge), maps.Keys(filtered)
}
