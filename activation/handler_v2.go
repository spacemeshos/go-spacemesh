package activation

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"
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
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
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
	malPublisher    malfeasancePublisher
}

func (h *HandlerV2) processATX(
	ctx context.Context,
	peer p2p.Peer,
	watx *wire.ActivationTxV2,
	received time.Time,
) error {
	exists, err := atxs.Has(h.cdb, watx.ID())
	if err != nil {
		return fmt.Errorf("failed to check if atx exists: %w", err)
	}
	if exists {
		return nil
	}

	h.logger.Debug(
		"processing atx",
		log.ZContext(ctx),
		zap.Stringer("atx_id", watx.ID()),
		zap.Uint32("publish", watx.PublishEpoch.Uint32()),
		zap.Stringer("smesherID", watx.SmesherID),
	)

	if err := h.syntacticallyValidate(ctx, watx); err != nil {
		return fmt.Errorf("%w: validating atx %s: %w", pubsub.ErrValidationReject, watx.ID(), err)
	}

	poetRef, atxIDs := h.collectAtxDeps(watx)
	h.registerHashes(peer, poetRef, atxIDs)
	if err := h.fetchReferences(ctx, poetRef, atxIDs); err != nil {
		return fmt.Errorf("fetching references for atx %s: %w", watx.ID(), err)
	}

	baseTickHeight, err := h.validatePositioningAtx(watx.PublishEpoch, h.goldenATXID, watx.PositioningATX)
	if err != nil {
		return fmt.Errorf("%w: validating positioning atx: %w", pubsub.ErrValidationReject, err)
	}

	marrying, err := h.validateMarriages(watx)
	if err != nil {
		return fmt.Errorf("%w: validating marriages: %w", pubsub.ErrValidationReject, err)
	}

	parts, err := h.syntacticallyValidateDeps(ctx, watx)
	if err != nil {
		return fmt.Errorf("%w: validating atx %s (deps): %w", pubsub.ErrValidationReject, watx.ID(), err)
	}

	atx := &types.ActivationTx{
		PublishEpoch:   watx.PublishEpoch,
		MarriageATX:    watx.MarriageATX,
		Coinbase:       watx.Coinbase,
		BaseTickHeight: baseTickHeight,
		NumUnits:       parts.effectiveUnits,
		TickCount:      parts.ticks,
		Weight:         parts.weight,
		VRFNonce:       types.VRFPostIndex(watx.VRFNonce),
		SmesherID:      watx.SmesherID,
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

	if err := h.storeAtx(ctx, atx, watx, marrying, parts.units); err != nil {
		return fmt.Errorf("cannot store atx %s: %w", atx.ShortString(), err)
	}

	events.ReportNewActivation(atx)
	h.logger.Info("new atx", log.ZContext(ctx), zap.Inline(atx))
	return err
}

// Syntactically validate an ATX.
func (h *HandlerV2) syntacticallyValidate(ctx context.Context, atx *wire.ActivationTxV2) error {
	if !h.edVerifier.Verify(signing.ATX, atx.SmesherID, atx.ID().Bytes(), atx.Signature) {
		return fmt.Errorf("invalid atx signature: %w", errMalformedData)
	}
	if atx.PositioningATX == types.EmptyATXID {
		return errors.New("empty positioning atx")
	}
	if len(atx.Marriages) != 0 {
		// Marriage ATX must contain a self-signed certificate.
		// It's identified by having ReferenceAtx == EmptyATXID.
		idx := slices.IndexFunc(atx.Marriages, func(cert wire.MarriageCertificate) bool {
			return cert.ReferenceAtx == types.EmptyATXID
		})
		if idx == -1 {
			return errors.New("signer must marry itself")
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
		if len(atx.PreviousATXs) != 0 {
			return errors.New("initial atx must not have previous atxs")
		}

		numUnits := atx.NiPosts[0].Posts[0].NumUnits
		if err := h.nipostValidator.VRFNonceV2(
			atx.SmesherID, atx.Initial.CommitmentATX, atx.VRFNonce, numUnits,
		); err != nil {
			return fmt.Errorf("invalid vrf nonce: %w", err)
		}
		post := wire.PostFromWireV1(&atx.Initial.Post)
		if err := h.nipostValidator.PostV2(
			ctx, atx.SmesherID, atx.Initial.CommitmentATX, post, shared.ZeroChallenge, numUnits,
		); err != nil {
			return fmt.Errorf("validating initial post: %w", err)
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
		if err := h.verifyIncludedIDsUniqueness(atx); err != nil {
			return err
		}
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
				return fmt.Errorf("missing atxs %s: %w", atxIDs, err)
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
	for _, cert := range atx.Marriages {
		ids = append(ids, cert.ReferenceAtx)
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

// Validate the previous ATX for the given PoST and return the effective numunits.
func (h *HandlerV2) validatePreviousAtx(
	id types.NodeID,
	post *wire.SubPostV2,
	prevAtxs []*types.ActivationTx,
) (uint32, error) {
	if post.PrevATXIndex >= uint32(len(prevAtxs)) {
		return 0, fmt.Errorf("prevATXIndex out of bounds: %d > %d", post.PrevATXIndex, len(prevAtxs))
	}
	prev := prevAtxs[post.PrevATXIndex]
	prevUnits, err := atxs.Units(h.cdb, prev.ID(), id)
	if err != nil {
		return 0, fmt.Errorf("fetching previous atx %s units for ID %s: %w", prev.ID(), id, err)
	}

	return min(prevUnits, post.NumUnits), nil
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

type marriage struct {
	id        types.NodeID
	signature types.EdSignature
}

// Validate marriages and return married IDs.
// Note: The order of returned IDs is important and must match the order of the marriage certificates.
// The MarriageIndex in PoST proof matches the index in this marriage slice.
func (h *HandlerV2) validateMarriages(atx *wire.ActivationTxV2) ([]marriage, error) {
	if len(atx.Marriages) == 0 {
		return nil, nil
	}
	marryingIDsSet := make(map[types.NodeID]struct{}, len(atx.Marriages))
	var marryingIDs []marriage
	for i, m := range atx.Marriages {
		var id types.NodeID
		if m.ReferenceAtx == types.EmptyATXID {
			id = atx.SmesherID
		} else {
			atx, err := atxs.Get(h.cdb, m.ReferenceAtx)
			if err != nil {
				return nil, fmt.Errorf("getting marriage reference atx: %w", err)
			}
			id = atx.SmesherID
		}

		if !h.edVerifier.Verify(signing.MARRIAGE, id, atx.SmesherID.Bytes(), m.Signature) {
			return nil, fmt.Errorf("invalid marriage[%d] signature", i)
		}
		if _, ok := marryingIDsSet[id]; ok {
			return nil, fmt.Errorf("more than 1 marriage certificate for ID %s", id)
		}
		marryingIDsSet[id] = struct{}{}
		marryingIDs = append(marryingIDs, marriage{
			id:        id,
			signature: m.Signature,
		})
	}
	return marryingIDs, nil
}

// Validate marriage ATX and return the full equivocation set.
func (h *HandlerV2) equivocationSet(atx *wire.ActivationTxV2) ([]types.NodeID, error) {
	if atx.MarriageATX == nil {
		return []types.NodeID{atx.SmesherID}, nil
	}
	marriageAtxID, err := identities.MarriageATX(h.cdb, atx.SmesherID)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		return nil, errors.New("smesher is not married")
	case err != nil:
		return nil, fmt.Errorf("fetching smesher's marriage atx ID: %w", err)
	}

	if *atx.MarriageATX != marriageAtxID {
		return nil, fmt.Errorf("smesher's marriage ATX ID mismatch: %s != %s", *atx.MarriageATX, marriageAtxID)
	}

	marriageAtx, err := atxs.Get(h.cdb, *atx.MarriageATX)
	if err != nil {
		return nil, fmt.Errorf("fetching marriage atx: %w", err)
	}
	if marriageAtx.PublishEpoch+2 > atx.PublishEpoch {
		return nil, fmt.Errorf(
			"marriage atx must be published at least 2 epochs before %v (is %v)",
			atx.PublishEpoch,
			marriageAtx.PublishEpoch,
		)
	}

	return identities.EquivocationSetByMarriageATX(h.cdb, *atx.MarriageATX)
}

type atxParts struct {
	ticks          uint64
	weight         uint64
	effectiveUnits uint32
	units          map[types.NodeID]uint32
}

type nipostSize struct {
	units uint32
	ticks uint64
}

func (n *nipostSize) addUnits(units uint32) error {
	sum, carry := bits.Add32(n.units, units, 0)
	if carry != 0 {
		return errors.New("units overflow")
	}
	n.units = sum
	return nil
}

type nipostSizes []*nipostSize

func (n nipostSizes) minTicks() uint64 {
	return slices.MinFunc(n, func(a, b *nipostSize) int { return cmp.Compare(a.ticks, b.ticks) }).ticks
}

func (n nipostSizes) sumUp() (units uint32, weight uint64, err error) {
	var totalUnits uint64
	var totalWeight uint64
	for _, ns := range n {
		totalUnits += uint64(ns.units)

		hi, weight := bits.Mul64(uint64(ns.units), ns.ticks)
		if hi != 0 {
			return 0, 0, fmt.Errorf("weight overflow (%d * %d)", ns.units, ns.ticks)
		}
		totalWeight += weight
	}
	if totalUnits > math.MaxUint32 {
		return 0, 0, fmt.Errorf("total units overflow: %d", totalUnits)
	}
	return uint32(totalUnits), totalWeight, nil
}

func (h *HandlerV2) verifyIncludedIDsUniqueness(atx *wire.ActivationTxV2) error {
	seen := make(map[uint32]struct{})
	for _, niposts := range atx.NiPosts {
		for _, post := range niposts.Posts {
			if _, ok := seen[post.MarriageIndex]; ok {
				return fmt.Errorf("ID present twice (duplicated marriage index): %d", post.MarriageIndex)
			}
			seen[post.MarriageIndex] = struct{}{}
		}
	}
	return nil
}

// Syntactically validate the ATX with its dependencies.
func (h *HandlerV2) syntacticallyValidateDeps(
	ctx context.Context,
	atx *wire.ActivationTxV2,
) (*atxParts, error) {
	parts := atxParts{
		units: make(map[types.NodeID]uint32),
	}
	if atx.Initial != nil {
		if err := h.validateCommitmentAtx(h.goldenATXID, atx.Initial.CommitmentATX, atx.PublishEpoch); err != nil {
			return nil, fmt.Errorf("verifying commitment ATX: %w", err)
		}
	}

	previousAtxs := make([]*types.ActivationTx, len(atx.PreviousATXs))
	for i, prev := range atx.PreviousATXs {
		prevAtx, err := atxs.Get(h.cdb, prev)
		if err != nil {
			return nil, fmt.Errorf("fetching previous atx: %w", err)
		}
		if prevAtx.PublishEpoch >= atx.PublishEpoch {
			err := fmt.Errorf("previous atx is too new (%d >= %d) (%s) ", prevAtx.PublishEpoch, atx.PublishEpoch, prev)
			return nil, err
		}
		previousAtxs[i] = prevAtx
	}

	equivocationSet, err := h.equivocationSet(atx)
	if err != nil {
		return nil, fmt.Errorf("calculating equivocation set: %w", err)
	}

	// validate previous ATXs
	nipostSizes := make(nipostSizes, len(atx.NiPosts))
	for i, niposts := range atx.NiPosts {
		nipostSizes[i] = new(nipostSize)
		for _, post := range niposts.Posts {
			if post.MarriageIndex >= uint32(len(equivocationSet)) {
				err := fmt.Errorf("marriage index out of bounds: %d > %d", post.MarriageIndex, len(equivocationSet)-1)
				return nil, err
			}

			id := equivocationSet[post.MarriageIndex]
			effectiveNumUnits := post.NumUnits
			if atx.Initial == nil {
				var err error
				effectiveNumUnits, err = h.validatePreviousAtx(id, &post, previousAtxs)
				if err != nil {
					return nil, fmt.Errorf("validating previous atx: %w", err)
				}
			}
			nipostSizes[i].addUnits(effectiveNumUnits)
		}
	}

	// validate poet membership proofs
	for i, niposts := range atx.NiPosts {
		// verify PoET memberships in a single go
		indexedChallenges := make(map[uint64][]byte)

		for _, post := range niposts.Posts {
			if _, ok := indexedChallenges[post.MembershipLeafIndex]; ok {
				continue
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
			indexedChallenges[post.MembershipLeafIndex] = nipostChallenge.Hash().Bytes()
		}

		leafIndices := maps.Keys(indexedChallenges)
		slices.Sort(leafIndices)
		poetChallenges := make([][]byte, 0, len(leafIndices))
		for _, i := range leafIndices {
			poetChallenges = append(poetChallenges, indexedChallenges[i])
		}

		membership := types.MultiMerkleProof{
			Nodes:       niposts.Membership.Nodes,
			LeafIndices: leafIndices,
		}
		leaves, err := h.nipostValidator.PoetMembership(ctx, &membership, niposts.Challenge, poetChallenges)
		if err != nil {
			return nil, fmt.Errorf("validating poet membership: %w", err)
		}
		nipostSizes[i].ticks = leaves / h.tickSize
	}

	parts.effectiveUnits, parts.weight, err = nipostSizes.sumUp()
	if err != nil {
		return nil, err
	}

	// validate all niposts
	var smesherCommitment *types.ATXID
	for _, niposts := range atx.NiPosts {
		for _, post := range niposts.Posts {
			id := equivocationSet[post.MarriageIndex]
			var commitment types.ATXID
			if atx.Initial != nil {
				commitment = atx.Initial.CommitmentATX
			} else {
				var err error
				commitment, err = atxs.CommitmentATX(h.cdb, id)
				if err != nil {
					return nil, fmt.Errorf("commitment atx not found for ID %s: %w", id, err)
				}
				if id == atx.SmesherID {
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
			invalidIdx := &verifying.ErrInvalidIndex{}
			if errors.As(err, invalidIdx) {
				h.logger.Info(
					"ATX with invalid post index",
					zap.Stringer("id", atx.ID()),
					zap.Int("index", invalidIdx.Index),
				)
				// TODO(mafa): finish proof
				var proof wire.Proof
				if err := h.malPublisher.Publish(ctx, id, proof); err != nil {
					return nil, fmt.Errorf("publishing malfeasance proof for invalid post: %w", err)
				}
			}
			if err != nil {
				return nil, fmt.Errorf("validating post for ID %s: %w", id.ShortString(), err)
			}
			parts.units[id] = post.NumUnits
		}
	}

	if atx.Initial == nil {
		if smesherCommitment == nil {
			return nil, errors.New("ATX signer not present in merged ATX")
		}
		err := h.nipostValidator.VRFNonceV2(atx.SmesherID, *smesherCommitment, atx.VRFNonce, atx.TotalNumUnits())
		if err != nil {
			return nil, fmt.Errorf("validating VRF nonce: %w", err)
		}
	}

	parts.ticks = nipostSizes.minTicks()
	return &parts, nil
}

func (h *HandlerV2) checkMalicious(
	ctx context.Context,
	tx *sql.Tx,
	watx *wire.ActivationTxV2,
	marrying []marriage,
	ids []types.NodeID,
) error {
	malicious, err := identities.IsMalicious(tx, watx.SmesherID)
	if err != nil {
		return fmt.Errorf("checking if node is malicious: %w", err)
	}
	if malicious {
		return nil
	}

	malicious, err = h.checkDoubleMarry(ctx, tx, watx, marrying)
	if err != nil {
		return fmt.Errorf("checking double marry: %w", err)
	}
	if malicious {
		return nil
	}

	malicious, err = h.checkDoublePost(ctx, tx, watx, ids)
	if err != nil {
		return fmt.Errorf("checking double post: %w", err)
	}
	if malicious {
		return nil
	}

	malicious, err = h.checkDoubleMerge(ctx, tx, watx)
	if err != nil {
		return fmt.Errorf("checking double merge: %w", err)
	}
	if malicious {
		return nil
	}

	// TODO(mafa): contextual validation:
	// 1. check double-publish = ID contributed post to two ATXs in the same epoch
	// 2. check previous ATX
	// 3  ID already married (same node ID in multiple marriage certificates)
	// 4. two ATXs referencing the same marriage certificate in the same epoch
	return nil
}

func (h *HandlerV2) checkDoubleMarry(
	ctx context.Context,
	tx *sql.Tx,
	atx *wire.ActivationTxV2,
	marrying []marriage,
) (bool, error) {
	for _, m := range marrying {
		mATX, err := identities.MarriageATX(tx, m.id)
		if err != nil {
			return false, fmt.Errorf("checking if ID is married: %w", err)
		}
		if mATX != atx.ID() {
			var blob sql.Blob
			v, err := atxs.LoadBlob(ctx, tx, mATX.Bytes(), &blob)
			if err != nil {
				return true, fmt.Errorf("creating double marry proof: %w", err)
			}
			if v != types.AtxV2 {
				h.logger.Fatal("Failed to create double marry malfeasance proof: ATX is not v2",
					zap.Stringer("atx_id", mATX),
				)
			}
			var otherAtx wire.ActivationTxV2
			codec.MustDecode(blob.Bytes, &otherAtx)

			proof, err := wire.NewDoubleMarryProof(tx, atx, &otherAtx, m.id)
			if err != nil {
				return true, fmt.Errorf("creating double marry proof: %w", err)
			}
			return true, h.malPublisher.Publish(ctx, m.id, proof)
		}
	}
	return false, nil
}

func (h *HandlerV2) checkDoublePost(
	ctx context.Context,
	tx *sql.Tx,
	atx *wire.ActivationTxV2,
	ids []types.NodeID,
) (bool, error) {
	for _, id := range ids {
		atxids, err := atxs.FindDoublePublish(tx, id, atx.PublishEpoch)
		switch {
		case errors.Is(err, sql.ErrNotFound):
			continue
		case err != nil:
			return false, fmt.Errorf("searching for double publish: %w", err)
		}
		otherAtxId := slices.IndexFunc(atxids, func(other types.ATXID) bool { return other != atx.ID() })
		otherAtx := atxids[otherAtxId]
		h.logger.Debug(
			"found ID that has already contributed its PoST in this epoch",
			zap.Stringer("node_id", id),
			zap.Stringer("atx_id", atx.ID()),
			zap.Stringer("other_atx_id", otherAtx),
			zap.Uint32("epoch", atx.PublishEpoch.Uint32()),
		)
		// TODO(mafa): finish proof
		var proof wire.Proof
		return true, h.malPublisher.Publish(ctx, id, proof)
	}
	return false, nil
}

func (h *HandlerV2) checkDoubleMerge(ctx context.Context, tx *sql.Tx, watx *wire.ActivationTxV2) (bool, error) {
	if watx.MarriageATX == nil {
		return false, nil
	}
	ids, err := atxs.MergeConflict(tx, *watx.MarriageATX, watx.PublishEpoch)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("searching for ATXs with the same marriage ATX: %w", err)
	}
	otherIndex := slices.IndexFunc(ids, func(id types.ATXID) bool { return id != watx.ID() })
	other := ids[otherIndex]

	h.logger.Debug("second merged ATX for single marriage - creating malfeasance proof",
		zap.Stringer("marriage_atx", *watx.MarriageATX),
		zap.Stringer("atx", watx.ID()),
		zap.Stringer("other_atx", other),
		zap.Stringer("smesher_id", watx.SmesherID),
	)

	var proof wire.Proof
	return true, h.malPublisher.Publish(ctx, watx.SmesherID, proof)
}

// Store an ATX in the DB.
func (h *HandlerV2) storeAtx(
	ctx context.Context,
	atx *types.ActivationTx,
	watx *wire.ActivationTxV2,
	marrying []marriage,
	units map[types.NodeID]uint32,
) error {
	if err := h.cdb.WithTx(ctx, func(tx *sql.Tx) error {
		if len(marrying) != 0 {
			marriageData := identities.MarriageData{
				ATX:    atx.ID(),
				Target: atx.SmesherID,
			}
			for i, m := range marrying {
				marriageData.Signature = m.signature
				marriageData.Index = i
				if err := identities.SetMarriage(tx, m.id, &marriageData); err != nil {
					return err
				}
			}
		}

		err := atxs.Add(tx, atx, watx.Blob())
		if err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("add atx to db: %w", err)
		}
		for id, units := range units {
			err = atxs.SetUnits(tx, atx.ID(), id, units)
			if err != nil && !errors.Is(err, sql.ErrObjectExists) {
				return fmt.Errorf("setting atx units for ID %s: %w", id, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("store atx: %w", err)
	}

	atxs.AtxAdded(h.cdb, atx)

	var malicious bool
	err := h.cdb.WithTx(ctx, func(tx *sql.Tx) error {
		// malfeasance check happens after storing the ATX because storing updates the marriage set
		// that is needed for the malfeasance proof
		// TODO(mafa): don't store own ATX if it would mark the node as malicious
		//    this probably needs to be done by validating and storing own ATXs eagerly and skipping validation in
		//    the gossip handler (not sync!)
		err := h.checkMalicious(ctx, tx, watx, marrying, maps.Keys(units))
		if err != nil {
			return fmt.Errorf("check malicious: %w", err)
		}
		malicious, err = identities.IsMalicious(tx, watx.SmesherID)
		if err != nil {
			return fmt.Errorf("checking if identity is malicious: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("check malicious: %w", err)
	}

	h.beacon.OnAtx(atx)
	if added := h.atxsdata.AddFromAtx(atx, malicious); added != nil {
		h.tortoise.OnAtx(atx.TargetEpoch(), atx.ID(), added)
	}

	h.logger.Debug("finished storing atx in epoch",
		log.ZContext(ctx),
		zap.Stringer("atx_id", atx.ID()),
		zap.Uint32("publish", atx.PublishEpoch.Uint32()),
	)

	return nil
}
