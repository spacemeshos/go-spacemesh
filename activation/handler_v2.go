package activation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

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

type nipostValidatorV2 interface {
	IsVerifyingFullPost() bool
	VRFNonceV2(smesherID types.NodeID, commitment types.ATXID, vrfNonce uint64, numUnits uint32) error
	PostV2(
		ctx context.Context,
		smesherID types.NodeID,
		commitment types.ATXID,
		post *types.Post,
		challenge []byte,
		numUnits uint32,
		opts ...validatorOption,
	) error

	PoetMembership(
		ctx context.Context,
		membership *types.MultiMerkleProof,
		postChallenge types.Hash32,
		poetChallenges [][]byte,
	) (uint64, error)
}

type HandlerV2 struct {
	local           p2p.Peer
	cdb             *datastore.CachedDB
	atxsdata        *atxsdata.Data
	edVerifier      *signing.EdVerifier
	clock           layerClock
	tickSize        uint64
	goldenATXID     types.ATXID
	nipostValidator nipostValidatorV2
	beacon          AtxReceiver
	tortoise        system.Tortoise
	logger          *zap.Logger
	fetcher         system.Fetcher
}

func (h *HandlerV2) processATX(
	ctx context.Context,
	peer p2p.Peer,
	watx *wire.ActivationTxV2,
	blob []byte,
	received time.Time,
) (*mwire.MalfeasanceProof, error) {
	exists, err := atxs.Has(h.cdb, watx.ID())
	if err != nil {
		return nil, fmt.Errorf("failed to check if atx exists: %w", err)
	}
	if exists {
		return nil, nil
	}

	h.logger.Debug(
		"processing atx",
		log.ZContext(ctx),
		zap.Stringer("atx_id", watx.ID()),
		zap.Uint32("publish", watx.PublishEpoch.Uint32()),
		zap.Stringer("smesherID", watx.SmesherID),
	)

	if err := h.syntacticallyValidate(ctx, watx); err != nil {
		return nil, fmt.Errorf("atx %s syntactically invalid: %w", watx.ID(), err)
	}

	poetRef, atxIDs := h.collectAtxDeps(watx)
	h.registerHashes(peer, poetRef, atxIDs)
	if err := h.fetchReferences(ctx, poetRef, atxIDs); err != nil {
		return nil, fmt.Errorf("fetching references for atx %s: %w", watx.ID(), err)
	}

	baseTickHeight, err := h.validatePositioningAtx(watx.PublishEpoch, h.goldenATXID, watx.PositioningATX)
	if err != nil {
		return nil, fmt.Errorf("validating positioning atx: %w", err)
	}

	parts, proof, err := h.syntacticallyValidateDeps(ctx, watx)
	if err != nil {
		return nil, fmt.Errorf("atx %s syntactically invalid based on deps: %w", watx.ID(), err)
	}

	if proof != nil {
		return proof, err
	}

	coinbase, err := h.coinbase(ctx, watx)
	if err != nil {
		return nil, fmt.Errorf("getting coinbase: %w", err)
	}

	atx := &types.ActivationTx{
		PublishEpoch:   watx.PublishEpoch,
		Coinbase:       coinbase,
		NumUnits:       parts.effectiveUnits,
		BaseTickHeight: baseTickHeight,
		TickCount:      parts.leaves / h.tickSize,
		VRFNonce:       parts.vrfNonce,
		SmesherID:      watx.SmesherID,
		AtxBlob:        types.AtxBlob{Blob: blob, Version: types.AtxV2},
	}
	if watx.Initial == nil {
		// FIXME: update to keep many previous ATXs to support merged ATXs
		atx.PrevATXID = watx.PreviousATXs[0]
	} else {
		atx.CommitmentATX = &watx.Initial.CommitmentATX
	}

	if h.nipostValidator.IsVerifyingFullPost() {
		atx.SetValidity(types.Valid)
	}
	atx.SetID(watx.ID())
	atx.SetReceived(received)

	proof, err = h.storeAtx(ctx, atx, watx)
	if err != nil {
		return nil, fmt.Errorf("cannot store atx %s: %w", atx.ShortString(), err)
	}

	events.ReportNewActivation(atx)
	h.logger.Info("new atx", log.ZContext(ctx), zap.Inline(atx), zap.Bool("malicious", proof != nil))
	return proof, err
}

// Syntactically validate an ATX.
// TODOs:
// 2. support merged ATXs.
func (h *HandlerV2) syntacticallyValidate(ctx context.Context, atx *wire.ActivationTxV2) error {
	if !h.edVerifier.Verify(signing.ATX, atx.SmesherID, atx.SignedBytes(), atx.Signature) {
		return fmt.Errorf("invalid atx signature: %w", errMalformedData)
	}
	if atx.PositioningATX == types.EmptyATXID {
		return errors.New("empty positioning atx")
	}
	for i, m := range atx.Marriages {
		if !h.edVerifier.Verify(signing.MARRIAGE, m.ID, atx.SmesherID.Bytes(), m.Signature) {
			return fmt.Errorf("invalid marriage[%d] signature", i)
		}
	}

	current := h.clock.CurrentLayer().GetEpoch()
	if atx.PublishEpoch > current+1 {
		return fmt.Errorf("atx publish epoch is too far in the future: %d > %d", atx.PublishEpoch, current+1)
	}

	if atx.MarriageATX == nil {
		if len(atx.NiPosts) != 1 {
			return errors.New("solo atx must have one nipost")
		}
		if len(atx.NiPosts[0].Posts) != 1 {
			return errors.New("solo atx must have one post")
		}
		if atx.NiPosts[0].Posts[0].PrevATXIndex != 0 {
			return errors.New("solo atx post must have prevATXIndex 0")
		}
	}

	if atx.Initial != nil {
		if atx.MarriageATX != nil {
			return errors.New("initial atx cannot reference a marriage atx")
		}
		if atx.Initial.CommitmentATX == types.EmptyATXID {
			return errors.New("initial atx missing commitment atx")
		}
		if atx.VRFNonce == nil {
			return errors.New("initial atx missing vrf nonce")
		}
		if len(atx.PreviousATXs) != 0 {
			return errors.New("initial atx must not have previous atxs")
		}

		if atx.Coinbase == nil {
			return errors.New("initial atx missing coinbase")
		}

		numUnits := atx.NiPosts[0].Posts[0].NumUnits
		if err := h.nipostValidator.VRFNonceV2(
			atx.SmesherID, atx.Initial.CommitmentATX, *atx.VRFNonce, numUnits,
		); err != nil {
			return fmt.Errorf("invalid vrf nonce: %w", err)
		}
		post := wire.PostFromWireV1(&atx.Initial.Post)
		if err := h.nipostValidator.PostV2(
			ctx, atx.SmesherID, atx.Initial.CommitmentATX, post, shared.ZeroChallenge, numUnits,
		); err != nil {
			return fmt.Errorf("invalid initial post: %w", err)
		}
		return nil
	}

	for i, prev := range atx.PreviousATXs {
		if prev == types.EmptyATXID {
			return fmt.Errorf("previous atx[%d] is empty", i)
		}
		if prev == h.goldenATXID {
			return fmt.Errorf("previous atx[%d] is the golden ATX", i)
		}
	}

	switch {
	case atx.MarriageATX != nil:
		// Merged ATX
		if len(atx.Marriages) != 0 {
			return errors.New("merged atx cannot have marriages")
		}
		// TODO: support merged ATXs
		return errors.New("atx merge is not supported")
	default:
		// Solo chained (non-initial) ATX
		if len(atx.PreviousATXs) != 1 {
			return errors.New("solo atx must have one previous atx")
		}
	}

	return nil
}

// registerHashes registers that the given peer should be asked for
// the hashes of the poet proofs and ATXs.
func (h *HandlerV2) registerHashes(peer p2p.Peer, poetRefs []types.Hash32, atxIDs []types.ATXID) {
	hashes := make([]types.Hash32, 0, len(atxIDs)+1)
	for _, id := range atxIDs {
		hashes = append(hashes, id.Hash32())
	}
	for _, poetRef := range poetRefs {
		hashes = append(hashes, types.Hash32(poetRef))
	}

	h.fetcher.RegisterPeerHashes(peer, hashes)
}

// fetchReferences makes sure that the referenced poet proof and ATXs are available.
func (h *HandlerV2) fetchReferences(ctx context.Context, poetRefs []types.Hash32, atxIDs []types.ATXID) error {
	eg, ctx := errgroup.WithContext(ctx)

	for _, poetRef := range poetRefs {
		eg.Go(func() error {
			if err := h.fetcher.GetPoetProof(ctx, poetRef); err != nil {
				return fmt.Errorf("fetching poet proof (%s): %w", poetRef.ShortString(), err)
			}
			return nil
		})
	}

	if len(atxIDs) != 0 {
		eg.Go(func() error {
			if err := h.fetcher.GetAtxs(ctx, atxIDs, system.WithoutLimiting()); err != nil {
				return fmt.Errorf("missing atxs %x: %w", atxIDs, err)
			}
			return nil
		})
	}
	return eg.Wait()
}

// Collect unique dependencies of an ATX.
// Filters out EmptyATXID and the golden ATX.
func (h *HandlerV2) collectAtxDeps(atx *wire.ActivationTxV2) ([]types.Hash32, []types.ATXID) {
	ids := []types.ATXID{atx.PositioningATX}
	ids = append(ids, atx.PreviousATXs...)

	if atx.Initial != nil {
		ids = append(ids, types.ATXID(atx.Initial.CommitmentATX))
	}
	if atx.MarriageATX != nil {
		ids = append(ids, *atx.MarriageATX)
	}

	filtered := make(map[types.ATXID]struct{})
	for _, id := range ids {
		if id != types.EmptyATXID && id != h.goldenATXID {
			filtered[id] = struct{}{}
		}
	}

	poetRefs := make(map[types.Hash32]struct{})
	for _, nipost := range atx.NiPosts {
		poetRefs[nipost.Challenge] = struct{}{}
	}

	return maps.Keys(poetRefs), maps.Keys(filtered)
}

func (h *HandlerV2) coinbase(ctx context.Context, atx *wire.ActivationTxV2) (types.Address, error) {
	if atx.Coinbase != nil {
		return *atx.Coinbase, nil
	}
	return atxs.Coinbase(h.cdb, atx.SmesherID)
}

func (h *HandlerV2) previous(ctx context.Context, id types.ATXID) (opaqueAtx, error) {
	var blob sql.Blob
	version, err := atxs.LoadBlob(ctx, h.cdb, id[:], &blob)
	if err != nil {
		return nil, err
	}

	if len(blob.Bytes) == 0 {
		// An empty blob indicates a golden ATX (after a checkpoint-recovery).
		// Fallback to fetching it from the DB to get the effective NumUnits.
		atx, err := atxs.Get(h.cdb, id)
		if err != nil {
			return nil, fmt.Errorf("fetching golden previous atx: %w", err)
		}
		return atx, nil
	}

	switch version {
	case types.AtxV1:
		var prev wire.ActivationTxV1
		if err := codec.Decode(blob.Bytes, &prev); err != nil {
			return nil, fmt.Errorf("decoding previous atx v1: %w", err)
		}
		return &prev, nil
	case types.AtxV2:
		var prev wire.ActivationTxV2
		if err := codec.Decode(blob.Bytes, &prev); err != nil {
			return nil, fmt.Errorf("decoding previous atx v2: %w", err)
		}
		return &prev, nil
	}
	return nil, fmt.Errorf("unexpected previous ATX version: %d", version)
}

// Validate the previous ATX for the given PoST and return the effective numunits.
func (h *HandlerV2) validatePreviousAtx(id types.NodeID, post *wire.SubPostV2, prevAtxs []opaqueAtx) (uint32, error) {
	if post.PrevATXIndex > uint32(len(prevAtxs)) {
		return 0, fmt.Errorf("prevATXIndex out of bounds: %d > %d", post.PrevATXIndex, len(prevAtxs))
	}
	prev := prevAtxs[post.PrevATXIndex]

	switch prev := prev.(type) {
	case *types.ActivationTx:
		// A golden ATX
		// TODO: support merged golden ATX
		if prev.SmesherID != id {
			return 0, fmt.Errorf("prev golden ATX has different owner: %s (expected %s)", prev.SmesherID, id)
		}
		return min(prev.NumUnits, post.NumUnits), nil

	case *wire.ActivationTxV1:
		if prev.SmesherID != id {
			return 0, fmt.Errorf("prev ATX V1 has different owner: %s (expected %s)", prev.SmesherID, id)
		}
		return min(prev.NumUnits, post.NumUnits), nil
	case *wire.ActivationTxV2:
		// TODO: support previous merged-ATX

		// previous is solo ATX
		if prev.SmesherID == id {
			return min(prev.NiPosts[0].Posts[0].NumUnits, post.NumUnits), nil
		}
		return 0, fmt.Errorf("previous solo ATX V2 has different owner: %s (expected %s)", prev.SmesherID, id)
	}
	return 0, fmt.Errorf("unexpected previous ATX type: %T", prev)
}

// VRF nonce must be valid for the collected space of all included IDs.
// TODO: support merged ATXs.
func (h *HandlerV2) validateVrfNonce(
	atx *wire.ActivationTxV2,
	previous opaqueAtx,
	commitment types.ATXID,
) (types.VRFPostIndex, error) {
	numUnits := atx.TotalNumUnits()
	needRecheck := numUnits > previous.TotalNumUnits() || atx.VRFNonce != nil
	var nonce types.VRFPostIndex
	if atx.VRFNonce == nil {
		// lookup the previous nonce by this ID
		// TODO: support merged ATXs
		// For a merged ATX we need to follow ATXs chain for `id`, find the last ATX by this ID
		// and check the nonce from it.
		if atx.MarriageATX != nil {
			return 0, errors.New("merged ATXs are not supported")
		}
		n, err := atxs.NonceByID(h.cdb, previous.ID())
		if err != nil {
			return 0, fmt.Errorf("getting nonce from previous ATX %s: %w", previous.ID(), err)
		}
		nonce = n
	} else {
		nonce = types.VRFPostIndex(*atx.VRFNonce)
	}

	if needRecheck {
		return nonce, h.nipostValidator.VRFNonceV2(atx.SmesherID, commitment, uint64(nonce), numUnits)
	}
	return nonce, nil
}

func (h *HandlerV2) validateCommitmentAtx(golden, commitmentAtxId types.ATXID, publish types.EpochID) error {
	if commitmentAtxId != golden {
		commitment, err := atxs.Get(h.cdb, commitmentAtxId)
		if err != nil {
			return &ErrAtxNotFound{Id: commitmentAtxId, source: err}
		}
		if publish <= commitment.PublishEpoch {
			return fmt.Errorf(
				"atx publish epoch (%v) must be after commitment atx publish epoch (%v)",
				publish,
				commitment.PublishEpoch,
			)
		}
	}
	return nil
}

// validate positioning ATX and return its tick height.
func (h *HandlerV2) validatePositioningAtx(publish types.EpochID, golden, positioning types.ATXID) (uint64, error) {
	if positioning == golden {
		return 0, nil
	}

	posAtx, err := atxs.Get(h.cdb, positioning)
	if err != nil {
		return 0, &ErrAtxNotFound{Id: positioning, source: err}
	}
	if posAtx.PublishEpoch >= publish {
		return 0, fmt.Errorf("positioning atx epoch (%v) must be before %v", posAtx.PublishEpoch, publish)
	}

	return posAtx.TickHeight(), nil
}

type atxParts struct {
	leaves         uint64
	effectiveUnits uint32
	vrfNonce       types.VRFPostIndex
}

// Syntactically validate the ATX with its dependencies.
func (h *HandlerV2) syntacticallyValidateDeps(
	ctx context.Context,
	atx *wire.ActivationTxV2,
) (*atxParts, *mwire.MalfeasanceProof, error) {
	if atx.Initial != nil {
		if err := h.validateCommitmentAtx(h.goldenATXID, atx.Initial.CommitmentATX, atx.PublishEpoch); err != nil {
			return nil, nil, fmt.Errorf("verifying commitment ATX: %w", err)
		}
	}

	previousAtxs := make([]opaqueAtx, len(atx.PreviousATXs))
	for i, prev := range atx.PreviousATXs {
		prevAtx, err := h.previous(ctx, prev)
		if err != nil {
			return nil, nil, fmt.Errorf("fetching previous atx: %w", err)
		}
		if prevAtx.Published() >= atx.PublishEpoch {
			err := fmt.Errorf("previous atx is too new (%d >= %d) (%s) ", prevAtx.Published(), atx.PublishEpoch, prev)
			return nil, nil, err
		}
		previousAtxs[i] = prevAtx
	}

	// validate all niposts
	// TODO: support merged ATXs
	// For a merged ATX we need to fetch the equivocation this smesher is part of.
	equivocationSet := []types.NodeID{atx.SmesherID}
	var totalEffectiveNumUnits uint32
	var minLeaves uint64
	var smesherCommitment *types.ATXID
	for _, niposts := range atx.NiPosts {
		// verify PoET memberships in a single go
		var poetChallenges [][]byte

		for _, post := range niposts.Posts {
			if post.MarriageIndex >= uint32(len(equivocationSet)) {
				err := fmt.Errorf("marriage index out of bounds: %d > %d", post.MarriageIndex, len(equivocationSet)-1)
				return nil, nil, err
			}
			id := equivocationSet[post.MarriageIndex]
			effectiveNumUnits := post.NumUnits
			if atx.Initial == nil {
				var err error
				effectiveNumUnits, err = h.validatePreviousAtx(id, &post, previousAtxs)
				if err != nil {
					return nil, nil, fmt.Errorf("validating previous atx for ID %s: %w", id, err)
				}
			}
			totalEffectiveNumUnits += effectiveNumUnits

			var commitment types.ATXID
			if atx.Initial != nil {
				commitment = atx.Initial.CommitmentATX
			} else {
				var err error
				commitment, err = atxs.CommitmentATX(h.cdb, id)
				if err != nil {
					return nil, nil, fmt.Errorf("commitment atx not found for ID %s: %w", id, err)
				}
				if smesherCommitment == nil {
					smesherCommitment = &commitment
				}
			}

			err := h.nipostValidator.PostV2(
				ctx,
				id,
				commitment,
				wire.PostFromWireV1(&post.Post),
				niposts.Challenge[:],
				post.NumUnits,
				PostSubset([]byte(h.local)),
			)
			var invalidIdx *verifying.ErrInvalidIndex
			if errors.As(err, &invalidIdx) {
				h.logger.Info(
					"ATX with invalid post index",
					zap.Stringer("id", atx.ID()),
					zap.Int("index", invalidIdx.Index),
				)
				// TODO generate malfeasance proof
			}
			if err != nil {
				return nil, nil, fmt.Errorf("invalid post for ID %s: %w", id, err)
			}

			nipostChallenge := wire.NIPostChallengeV2{
				PublishEpoch:     atx.PublishEpoch,
				PositioningATXID: atx.PositioningATX,
			}
			if atx.Initial != nil {
				nipostChallenge.InitialPost = &atx.Initial.Post
			} else {
				nipostChallenge.PrevATXID = atx.PreviousATXs[post.PrevATXIndex]
			}

			poetChallenges = append(poetChallenges, nipostChallenge.Hash().Bytes())
		}
		membership := types.MultiMerkleProof{
			Nodes:       niposts.Membership.Nodes,
			LeafIndices: niposts.Membership.LeafIndices,
		}
		leaves, err := h.nipostValidator.PoetMembership(ctx, &membership, niposts.Challenge, poetChallenges)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid poet membership: %w", err)
		}
		minLeaves = min(leaves, minLeaves)
	}

	parts := &atxParts{
		leaves:         minLeaves,
		effectiveUnits: totalEffectiveNumUnits,
	}

	if atx.Initial == nil {
		n, err := h.validateVrfNonce(atx, previousAtxs[0], *smesherCommitment)
		if err != nil {
			return nil, nil, fmt.Errorf("validating VRF nonce: %w", err)
		}
		parts.vrfNonce = n
	} else {
		parts.vrfNonce = types.VRFPostIndex(*atx.VRFNonce)
	}

	return parts, nil, nil
}

func (h *HandlerV2) checkMalicious(
	ctx context.Context,
	tx *sql.Tx,
	watx *wire.ActivationTxV2,
) (bool, *mwire.MalfeasanceProof, error) {
	malicious, err := identities.IsMalicious(tx, watx.SmesherID)
	if err != nil {
		return false, nil, fmt.Errorf("checking if node is malicious: %w", err)
	}
	if malicious {
		return true, nil, nil
	}

	proof, err := h.checkDoubleMarry(tx, watx)
	if err != nil {
		return false, nil, fmt.Errorf("checking double marry: %w", err)
	}
	if proof != nil {
		return true, proof, nil
	}

	// TODO: contextual validation:
	// 1. check double-publish
	// 2. check previous ATX
	// 3  ID already married (same node ID in multiple marriage certificates)
	// 4. two ATXs referencing the same marriage certificate in the same epoch
	// 5. ID participated in two ATXs (merged and solo) in the same epoch

	return false, nil, nil
}

func (h *HandlerV2) checkDoubleMarry(
	tx *sql.Tx,
	watx *wire.ActivationTxV2,
) (*mwire.MalfeasanceProof, error) {
	checkMarried := func(tx *sql.Tx, id types.NodeID) (*mwire.MalfeasanceProof, error) {
		married, err := identities.Married(tx, id)
		if err != nil {
			return nil, fmt.Errorf("checking if ID is married: %w", err)
		}
		if married {
			proof := &mwire.MalfeasanceProof{
				Proof: mwire.Proof{
					Type: mwire.DoubleMarry,
					Data: &mwire.DoubleMarryProof{},
				},
			}
			return proof, nil
		}
		return nil, nil
	}

	if p, err := checkMarried(tx, watx.SmesherID); p != nil || err != nil {
		return p, err
	}

	for _, m := range watx.Marriages {
		if p, err := checkMarried(tx, m.ID); p != nil || err != nil {
			return p, err
		}
	}
	return nil, nil
}

// Store an ATX in the DB.
// TODO: detect malfeasance and create proofs.
func (h *HandlerV2) storeAtx(
	ctx context.Context,
	atx *types.ActivationTx,
	watx *wire.ActivationTxV2,
) (*mwire.MalfeasanceProof, error) {
	var (
		malicious bool
		proof     *mwire.MalfeasanceProof
	)
	if err := h.cdb.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		malicious, proof, err = h.checkMalicious(ctx, tx, watx)
		if err != nil {
			return fmt.Errorf("check malicious: %w", err)
		}

		if len(watx.Marriages) != 0 {
			for _, m := range watx.Marriages {
				if err := identities.SetMarriage(tx, m.ID, atx.ID()); err != nil {
					return err
				}
			}
			if err := identities.SetMarriage(tx, atx.SmesherID, atx.ID()); err != nil {
				return err
			}
			if !malicious && proof == nil {
				// We check for malfeasance again becase the marriage increased the equivocation set.
				malicious, err = identities.IsMalicious(tx, atx.SmesherID)
				if err != nil {
					return fmt.Errorf("re-checking if smesherID is malicious: %w", err)
				}
			}
		}

		err = atxs.Add(tx, atx)
		if err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("add atx to db: %w", err)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("store atx: %w", err)
	}

	atxs.AtxAdded(h.cdb, atx)

	var allMalicious map[types.NodeID]struct{}
	if malicious || proof != nil {
		// Combine IDs from the present equivocation set for atx.SmesherID and IDs in atx.Marriages.
		allMalicious = make(map[types.NodeID]struct{})

		set, err := identities.EquivocationSet(h.cdb, atx.SmesherID)
		if err != nil {
			return nil, fmt.Errorf("getting equivocation set: %w", err)
		}
		for _, id := range set {
			allMalicious[id] = struct{}{}
		}
		for _, m := range watx.Marriages {
			allMalicious[m.ID] = struct{}{}
		}
	}
	if proof != nil {
		encoded, err := codec.Encode(proof)
		if err != nil {
			return nil, fmt.Errorf("encoding malfeasance proof: %w", err)
		}

		for id := range allMalicious {
			if err := identities.SetMalicious(h.cdb, id, encoded, atx.Received()); err != nil {
				return nil, fmt.Errorf("setting malfeasance proof: %w", err)
			}
			h.cdb.CacheMalfeasanceProof(id, proof)
		}
	}

	for id := range allMalicious {
		h.tortoise.OnMalfeasance(id)
	}

	h.beacon.OnAtx(atx)
	if added := h.atxsdata.AddFromAtx(atx, malicious || proof != nil); added != nil {
		h.tortoise.OnAtx(atx.TargetEpoch(), atx.ID(), added)
	}

	h.logger.Debug("finished storing atx in epoch",
		log.ZContext(ctx),
		zap.Stringer("atx_id", atx.ID()),
		zap.Uint32("publish", atx.PublishEpoch.Uint32()),
	)

	return proof, nil
}
