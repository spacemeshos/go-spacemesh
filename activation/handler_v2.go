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
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/system"
)

var errAtxNotV2 = errors.New("ATX is not V2")

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
	beacon          atxReceiver
	tortoise        system.Tortoise
	logger          *zap.Logger
	fetcher         system.Fetcher
	malPublisher    atxMalfeasancePublisher
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

	atxData, err := h.syntacticallyValidateDeps(ctx, watx, peer)
	if err != nil {
		return fmt.Errorf("%w: validating atx %s (deps): %w", pubsub.ErrValidationReject, watx.ID(), err)
	}
	atxData.marriages = marrying

	atx := &types.ActivationTx{
		PublishEpoch:   watx.PublishEpoch,
		MarriageATX:    watx.MarriageATX,
		Coinbase:       watx.Coinbase,
		BaseTickHeight: baseTickHeight,
		NumUnits:       atxData.effectiveUnits,
		TickCount:      atxData.ticks,
		Weight:         atxData.weight,
		VRFNonce:       types.VRFPostIndex(watx.VRFNonce),
		SmesherID:      watx.SmesherID,
	}

	if watx.Initial != nil {
		atx.CommitmentATX = &watx.Initial.CommitmentATX
	}

	if h.nipostValidator.IsVerifyingFullPost() {
		atx.SetValidity(types.Valid)
	}
	atx.SetID(watx.ID())
	atx.SetReceived(received)

	if err := h.storeAtx(ctx, atx, atxData, peer); err != nil {
		return fmt.Errorf("cannot store atx %s: %w", atx.ShortString(), err)
	}

	if err := events.ReportNewActivation(atx); err != nil {
		h.logger.Error("failed to emit activation",
			log.ZShortStringer("atx_id", atx.ID()),
			zap.Uint32("epoch", atx.PublishEpoch.Uint32()),
			zap.Error(err),
		)
	}
	h.logger.Debug("new atx", log.ZContext(ctx), zap.Inline(atx))
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
		if len(atx.NIPosts) != 1 {
			return errors.New("solo atx must have one nipost")
		}
		if len(atx.NIPosts[0].Posts) != 1 {
			return errors.New("solo atx must have one post")
		}
		if atx.NIPosts[0].Posts[0].PrevATXIndex != 0 {
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

		numUnits := atx.NIPosts[0].Posts[0].NumUnits
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
	for _, nipost := range atx.NIPosts {
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
			return fmt.Errorf("ATX (%s) not found: %w", commitmentAtxId.ShortString(), err)
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
		return 0, fmt.Errorf("positioning ATX (%s) not found: %w", positioning.ShortString(), err)
	}
	if posAtx.PublishEpoch >= publish {
		return 0, fmt.Errorf("positioning atx epoch (%v) must be before %v", posAtx.PublishEpoch, publish)
	}

	return posAtx.TickHeight(), nil
}

type marriageInfo struct {
	id        types.NodeID
	signature types.EdSignature
}

// Validate marriages and return married IDs.
// Note: The order of returned IDs is important and must match the order of the marriage certificates.
// The MarriageIndex in PoST proof matches the index in this marriage slice.
func (h *HandlerV2) validateMarriages(atx *wire.ActivationTxV2) ([]marriageInfo, error) {
	if len(atx.Marriages) == 0 {
		return nil, nil
	}
	marryingIDsSet := make(map[types.NodeID]struct{}, len(atx.Marriages))
	var marryingIDs []marriageInfo
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
		marryingIDs = append(marryingIDs, marriageInfo{
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
	info, err := marriage.FindByNodeID(h.cdb, atx.SmesherID)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		return nil, errors.New("smesher is not married")
	case err != nil:
		return nil, fmt.Errorf("fetching smesher's marriage atx ID: %w", err)
	}

	if *atx.MarriageATX != info.ATX {
		return nil, fmt.Errorf("smesher's marriage ATX ID mismatch: %s != %s", *atx.MarriageATX, info.ATX)
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
	return marriage.NodeIDsByID(h.cdb, info.ID)
}

type idData struct {
	previous      types.ATXID
	previousIndex int
	units         uint32
}

type activationTx struct {
	*wire.ActivationTxV2
	ticks          uint64
	weight         uint64
	effectiveUnits uint32
	ids            map[types.NodeID]idData
	marriages      []marriageInfo
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
	for _, niPosts := range atx.NIPosts {
		for _, post := range niPosts.Posts {
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
	peer p2p.Peer,
) (*activationTx, error) {
	result := activationTx{
		ActivationTxV2: atx,
		ids:            make(map[types.NodeID]idData),
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
			err := fmt.Errorf("previous atx (%s) is too new (%d >= %d)", prev, prevAtx.PublishEpoch, atx.PublishEpoch)
			return nil, err
		}
		previousAtxs[i] = prevAtx
	}

	equivocationSet, err := h.equivocationSet(atx)
	if err != nil {
		return nil, fmt.Errorf("calculating equivocation set: %w", err)
	}

	// validate previous ATXs
	nipostSizes := make(nipostSizes, len(atx.NIPosts))
	for i, niPosts := range atx.NIPosts {
		nipostSizes[i] = new(nipostSize)
		for _, post := range niPosts.Posts {
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
	for i, niPosts := range atx.NIPosts {
		// verify PoET memberships in a single go
		indexedChallenges := make(map[uint64][]byte)

		for _, post := range niPosts.Posts {
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
			Nodes:       niPosts.Membership.Nodes,
			LeafIndices: leafIndices,
		}
		leaves, err := h.nipostValidator.PoetMembership(ctx, &membership, niPosts.Challenge, poetChallenges)
		if err != nil {
			return nil, fmt.Errorf("validating poet membership: %w", err)
		}
		nipostSizes[i].ticks = leaves / h.tickSize
	}

	result.effectiveUnits, result.weight, err = nipostSizes.sumUp()
	if err != nil {
		return nil, err
	}

	// validate all NIPoSTs
	if atx.Initial != nil {
		commitment := atx.Initial.CommitmentATX
		nipostIdx := 0
		challenge := atx.NIPosts[nipostIdx].Challenge
		post := atx.NIPosts[nipostIdx].Posts[0]
		if err := h.validatePost(ctx, atx.SmesherID, atx, peer, commitment, challenge, post, nipostIdx); err != nil {
			return nil, err
		}
		result.ids[atx.SmesherID] = idData{
			previous:      types.EmptyATXID,
			previousIndex: 0,
			units:         post.NumUnits,
		}
		result.ticks = nipostSizes.minTicks()
		return &result, nil
	}

	var smesherCommitment *types.ATXID
	for idx, niPosts := range atx.NIPosts {
		for _, post := range niPosts.Posts {
			id := equivocationSet[post.MarriageIndex]
			commitment, err := atxs.CommitmentATX(h.cdb, id)
			if err != nil {
				return nil, fmt.Errorf("commitment atx not found for ID %s: %w", id, err)
			}
			if id == atx.SmesherID {
				smesherCommitment = &commitment
			}
			if err := h.validatePost(ctx, id, atx, peer, commitment, niPosts.Challenge, post, idx); err != nil {
				return nil, err
			}
			result.ids[id] = idData{
				previous:      previousAtxs[post.PrevATXIndex].ID(),
				previousIndex: int(post.PrevATXIndex),
				units:         post.NumUnits,
			}
		}
	}

	if smesherCommitment == nil {
		return nil, errors.New("ATX signer not present in merged ATX")
	}
	err = h.nipostValidator.VRFNonceV2(atx.SmesherID, *smesherCommitment, atx.VRFNonce, atx.TotalNumUnits())
	if err != nil {
		return nil, fmt.Errorf("validating VRF nonce: %w", err)
	}

	result.ticks = nipostSizes.minTicks()
	return &result, nil
}

func (h *HandlerV2) validatePost(
	ctx context.Context,
	nodeID types.NodeID,
	atx *wire.ActivationTxV2,
	peer p2p.Peer,
	commitment types.ATXID,
	challenge types.Hash32,
	post wire.SubPostV2,
	nipostIndex int,
) error {
	err := h.nipostValidator.PostV2(
		ctx,
		nodeID,
		commitment,
		wire.PostFromWireV1(&post.Post),
		challenge.Bytes(),
		post.NumUnits,
		PostSubset([]byte(h.local)),
	)
	if err == nil {
		return nil
	}
	errInvalidIdx := &verifying.ErrInvalidIndex{}
	if !errors.As(err, &errInvalidIdx) {
		return fmt.Errorf("validating post for ID %s: %w", nodeID.ShortString(), err)
	}

	if peer == h.local {
		return fmt.Errorf("invalid post for ID %s: %w", nodeID.ShortString(), errInvalidIdx)
	}

	h.logger.Debug("ATX with invalid post index",
		log.ZContext(ctx),
		zap.Stringer("atx_id", atx.ID()),
		zap.Int("index", errInvalidIdx.Index),
	)

	// check if post contains at least one valid label
	validIdx := 0
	for {
		err := h.nipostValidator.PostV2(
			ctx,
			nodeID,
			commitment,
			wire.PostFromWireV1(&post.Post),
			challenge.Bytes(),
			post.NumUnits,
			PostIndex(validIdx),
		)
		if err == nil {
			break
		}
		if errors.Is(err, ErrPostIndexOutOfRange) {
			return fmt.Errorf("invalid post for ID %s: %w", nodeID.ShortString(), err)
		}
		validIdx++
	}

	// TODO(mafa): checkpoints need to include all marriage ATXs in full to be able to create malfeasance proofs
	// like this one (but also others)
	//
	// see https://github.com/spacemeshos/go-spacemesh/issues/6435
	proof, err := wire.NewInvalidPostProof(
		h.cdb,
		atx,
		commitment,
		nodeID,
		nipostIndex,
		uint32(errInvalidIdx.Index),
		uint32(validIdx),
	)
	if err != nil {
		return fmt.Errorf("creating invalid post proof: %w", err)
	}
	if err := h.malPublisher.Publish(ctx, nodeID, proof); err != nil {
		return fmt.Errorf("publishing malfeasance proof for invalid post: %w", err)
	}
	return nil
}

func (h *HandlerV2) checkMalicious(
	ctx context.Context,
	tx sql.Transaction,
	watx *activationTx,
	peer p2p.Peer,
) (wire.Proof, types.NodeID, error) {
	proof, nodeID, err := h.checkDoubleMarry(ctx, tx, watx, peer)
	if err != nil {
		return nil, types.EmptyNodeID, fmt.Errorf("checking double marry: %w", err)
	}
	if proof != nil {
		return proof, nodeID, nil
	}

	proof, nodeID, err = h.checkDoubleMerge(ctx, tx, watx, peer)
	if err != nil {
		return nil, types.EmptyNodeID, fmt.Errorf("checking double merge: %w", err)
	}
	if proof != nil {
		return proof, nodeID, nil
	}

	proof, nodeID, err = h.checkPrevAtx(ctx, tx, watx, peer)
	if err != nil {
		return nil, types.EmptyNodeID, fmt.Errorf("checking previous ATX: %w", err)
	}
	return proof, nodeID, nil
}

func (h *HandlerV2) fetchWireAtx(
	ctx context.Context,
	tx sql.Executor,
	id types.ATXID,
) (*wire.ActivationTxV2, error) {
	var blob sql.Blob
	v, err := atxs.LoadBlob(ctx, tx, id.Bytes(), &blob)
	if err != nil {
		return nil, fmt.Errorf("get atx blob %s: %w", id.ShortString(), err)
	}
	if v != types.AtxV2 {
		return nil, errAtxNotV2
	}
	atx := &wire.ActivationTxV2{}
	codec.MustDecode(blob.Bytes, atx)
	return atx, nil
}

func (h *HandlerV2) checkDoubleMarry(
	ctx context.Context,
	tx sql.Transaction,
	atx *activationTx,
	peer p2p.Peer,
) (wire.Proof, types.NodeID, error) {
	for _, m := range atx.marriages {
		info, err := marriage.FindByNodeID(tx, m.id)
		if err != nil {
			return nil, types.EmptyNodeID, fmt.Errorf("checking if ID is married: %w", err)
		}
		if info.ATX == atx.ID() {
			continue
		}

		if peer == h.local {
			return nil, types.EmptyNodeID, fmt.Errorf("%s is already married via ATX %s",
				atx.SmesherID.ShortString(),
				info.ATX.ShortString(),
			)
		}

		otherAtx, err := h.fetchWireAtx(ctx, tx, info.ATX)
		switch {
		case errors.Is(err, errAtxNotV2):
			h.logger.Fatal("Failed to create double marry malfeasance proof: ATX is not v2",
				zap.Stringer("atx_id", info.ATX),
			)
		case err != nil:
			return nil, types.EmptyNodeID, fmt.Errorf("fetching other ATX: %w", err)
		}

		proof, err := wire.NewDoubleMarryProof(tx, atx.ActivationTxV2, otherAtx, m.id)
		if err != nil {
			return nil, types.EmptyNodeID, fmt.Errorf("creating double marry proof: %w", err)
		}
		return proof, m.id, nil
	}
	return nil, types.EmptyNodeID, nil
}

func (h *HandlerV2) checkDoubleMerge(
	ctx context.Context,
	tx sql.Transaction,
	atx *activationTx,
	peer p2p.Peer,
) (wire.Proof, types.NodeID, error) {
	if atx.MarriageATX == nil {
		return nil, types.EmptyNodeID, nil
	}
	ids, err := atxs.MergeConflict(tx, *atx.MarriageATX, atx.PublishEpoch)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		return nil, types.EmptyNodeID, nil
	case err != nil:
		return nil, types.EmptyNodeID, fmt.Errorf("searching for ATXs with the same marriage ATX: %w", err)
	}

	if peer == h.local {
		return nil, types.EmptyNodeID, fmt.Errorf("multiple ATXs with the same marriage ATX %s published in epoch %d",
			atx.MarriageATX.ShortString(),
			atx.PublishEpoch,
		)
	}

	otherIndex := slices.IndexFunc(ids, func(id types.ATXID) bool { return id != atx.ID() })
	other := ids[otherIndex]

	h.logger.Debug("second merged ATX for single marriage - creating malfeasance proof",
		zap.Stringer("marriage_atx", *atx.MarriageATX),
		zap.Stringer("atx", atx.ID()),
		zap.Stringer("other_atx", other),
		zap.Stringer("smesher_id", atx.SmesherID),
	)

	// TODO(mafa): during syntactical validation we should check if a merged ATX is targeting a checkpointed epoch
	// merged ATXs need to be checkpointed with their marriage ATXs
	// if there is a collision (i.e. the new ATX references the same marriage ATX as a golden ATX) it should be
	// considered syntactically invalid
	//
	// see https://github.com/spacemeshos/go-spacemesh/issues/6434
	otherAtx, err := h.fetchWireAtx(ctx, tx, other)
	if err != nil {
		return nil, types.EmptyNodeID, fmt.Errorf("fetching other ATX: %w", err)
	}

	// TODO(mafa): checkpoints need to include all marriage ATXs in full to be able to create malfeasance proofs
	// like this one (but also others)
	//
	// see https://github.com/spacemeshos/go-spacemesh/issues/6435
	proof, err := wire.NewDoubleMergeProof(tx, atx.ActivationTxV2, otherAtx)
	if err != nil {
		return nil, types.EmptyNodeID, fmt.Errorf("creating double merge proof: %w", err)
	}
	return proof, atx.ActivationTxV2.SmesherID, nil
}

func (h *HandlerV2) checkPrevAtx(
	ctx context.Context,
	tx sql.Transaction,
	atx *activationTx,
	peer p2p.Peer,
) (wire.Proof, types.NodeID, error) {
	for id, data := range atx.ids {
		expectedPrevID, err := atxs.PrevIDByNodeID(tx, atx.ID(), id, atx.PublishEpoch)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return nil, types.EmptyNodeID, fmt.Errorf("get last atx by node id: %w", err)
		}
		if expectedPrevID == data.previous {
			continue
		}

		h.logger.Debug("atx references a wrong previous ATX",
			log.ZShortStringer("smesherID", id),
			log.ZShortStringer("actual", data.previous),
			log.ZShortStringer("expected", expectedPrevID),
		)

		if peer == h.local {
			return nil, types.EmptyNodeID, fmt.Errorf("multiple ATXs with the same previous ATX %s published by %s",
				data.previous.ShortString(),
				id.ShortString(),
			)
		}

		collisions, err := atxs.PrevATXCollisions(tx, data.previous, id)
		switch {
		case errors.Is(err, sql.ErrNotFound):
			continue
		case err != nil:
			return nil, types.EmptyNodeID, fmt.Errorf("checking for previous ATX collision: %w", err)
		}

		var wireAtxV1 *wire.ActivationTxV1
		for _, collision := range collisions {
			if collision == atx.ID() {
				continue
			}
			var blob sql.Blob
			v, err := atxs.LoadBlob(ctx, tx, collision.Bytes(), &blob)
			if err != nil {
				return nil, types.EmptyNodeID, fmt.Errorf("get atx blob %s: %w", id.ShortString(), err)
			}
			switch v {
			case types.AtxV1:
				if wireAtxV1 == nil {
					// we have at least one v2 ATX (the one we are validating right now) so we only need one
					// v1 ATX to create the proof if no other v2 ATXs are found
					wireAtxV1 = &wire.ActivationTxV1{}
					codec.MustDecode(blob.Bytes, wireAtxV1)
				}
			case types.AtxV2:
				wireAtx := &wire.ActivationTxV2{}
				codec.MustDecode(blob.Bytes, wireAtx)
				// prefer creating a proof with 2 ATXs of version 2
				h.logger.Debug("creating a malfeasance proof for invalid previous ATX",
					log.ZShortStringer("smesherID", id),
					log.ZShortStringer("atx1", wireAtx.ID()),
					log.ZShortStringer("atx2", atx.ActivationTxV2.ID()),
				)
				proof, err := wire.NewInvalidPrevAtxProofV2(tx, atx.ActivationTxV2, wireAtx, id)
				if err != nil {
					return nil, types.EmptyNodeID, fmt.Errorf("creating invalid previous ATX proof: %w", err)
				}
				return proof, id, nil
			default:
				h.logger.Fatal("Failed to create invalid previous ATX proof: unknown ATX version",
					zap.Stringer("atx_id", collision),
				)
			}
		}

		// no ATXv2 found, create a proof with an ATXv1
		h.logger.Debug("creating a malfeasance proof for invalid previous ATX",
			log.ZShortStringer("smesherID", id),
			log.ZShortStringer("atx1", wireAtxV1.ID()),
			log.ZShortStringer("atx2", atx.ActivationTxV2.ID()),
		)
		proof, err := wire.NewInvalidPrevAtxProofV1(tx, atx.ActivationTxV2, wireAtxV1, id)
		if err != nil {
			return nil, types.EmptyNodeID, fmt.Errorf("creating invalid previous ATX proof: %w", err)
		}
		return proof, id, nil
	}
	return nil, types.EmptyNodeID, nil
}

// Store an ATX in the DB.
func (h *HandlerV2) storeAtx(ctx context.Context, atx *types.ActivationTx, watx *activationTx, peer p2p.Peer) error {
	republishProof := false
	malicious := false
	var proof wire.Proof
	var nodeID types.NodeID
	if err := h.cdb.WithTxImmediate(ctx, func(tx sql.Transaction) error {
		if len(watx.marriages) != 0 {
			newMarriageID, err := marriage.NewID(tx)
			if err != nil {
				return fmt.Errorf("creating marriage ID: %w", err)
			}
			info := marriage.Info{
				ID:     newMarriageID,
				ATX:    atx.ID(),
				Target: atx.SmesherID,
			}
			marriageIDs := make(map[marriage.ID]struct{}, 1)
			marriageIDs[newMarriageID] = struct{}{}
			for i, m := range watx.marriages {
				info.NodeID = m.id
				info.MarriageIndex = i
				info.Signature = m.signature
				err := marriage.Add(tx, info)
				switch {
				case errors.Is(err, sql.ErrConflict):
					id, err := marriage.FindIDByNodeID(tx, m.id)
					if err != nil {
						return fmt.Errorf("find marriage ID for node ID %s: %w", m.id.ShortString(), err)
					}
					marriageIDs[id] = struct{}{}
				case err != nil:
					return fmt.Errorf("adding marriage: %w", err)
				}
				if malicious {
					continue
				}
				malicious, err = malfeasance.IsMalicious(tx, m.id)
				if err != nil {
					return fmt.Errorf("checking if node is malicious: %w", err)
				}
			}
			if len(marriageIDs) > 1 {
				ids := maps.Keys(marriageIDs)
				newMarriageID = slices.Min(ids)
				for _, id := range ids {
					if id != newMarriageID {
						if err := marriage.UpdateMarriageID(tx, id, newMarriageID); err != nil {
							return fmt.Errorf("updating marriage ID for %d: %w", id, err)
						}
					}
				}
			}
			if malicious {
				nodeIDs, err := marriage.NodeIDsByID(tx, newMarriageID)
				if err != nil {
					return fmt.Errorf("fetching node IDs by marriage ID: %w", err)
				}
				for _, id := range nodeIDs {
					malicious, err := malfeasance.IsMalicious(tx, id)
					if err != nil {
						return fmt.Errorf("checking if node ID is malicious: %w", err)
					}
					if !malicious {
						republishProof = true
					}
					if err := malfeasance.SetMalicious(tx, id, newMarriageID, time.Now()); err != nil {
						return fmt.Errorf("marking node as malicious: %w", err)
					}
				}
			}
		}

		err := atxs.Add(tx, atx, watx.Blob())
		if err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("add atx to db: %w", err)
		}
		for id, post := range watx.ids {
			err = atxs.SetPost(tx, atx.ID(), post.previous, post.previousIndex, id, post.units, atx.PublishEpoch)
			if err != nil && !errors.Is(err, sql.ErrObjectExists) {
				return fmt.Errorf("setting atx units for ID %s: %w", id, err)
			}
		}

		if malicious || republishProof {
			if peer == h.local {
				return errors.New("not publishing ATXs for malicious nodes")
			}
			return nil
		}

		// malfeasance check happens at the end of storing the ATX because storing updates the marriage set
		// that is needed for the malfeasance proof
		proof, nodeID, err = h.checkMalicious(ctx, tx, watx, peer)
		return err
	}); err != nil {
		return fmt.Errorf("store atx: %w", err)
	}

	switch {
	case republishProof: // marriage set of known malicious smesher has changed, force re-gossip of proof
		if err := h.malPublisher.Regossip(ctx, watx.SmesherID); err != nil {
			h.logger.Error("failed to regossip malfeasance proof",
				zap.Stringer("atx_id", watx.ID()),
				zap.Stringer("smesher_id", watx.SmesherID),
				zap.Error(err),
			)
		}
	case proof != nil: // new malfeasance proof for identity created, publish proof (gossip is decided by publisher)
		if err := h.malPublisher.Publish(ctx, nodeID, proof); err != nil {
			h.logger.Error("failed to publish malfeasance proof",
				zap.Stringer("atx_id", watx.ID()),
				zap.Stringer("smesher_id", watx.SmesherID),
				zap.Error(err),
			)
		}
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

	return nil
}
