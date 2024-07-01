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

	marrying, err := h.validateMarriages(watx)
	if err != nil {
		return nil, fmt.Errorf("validating marriages: %w", err)
	}

	parts, proof, err := h.syntacticallyValidateDeps(ctx, watx)
	if err != nil {
		return nil, fmt.Errorf("atx %s syntactically invalid based on deps: %w", watx.ID(), err)
	}

	if proof != nil {
		return proof, err
	}

	atx := &types.ActivationTx{
		PublishEpoch:   watx.PublishEpoch,
		Coinbase:       watx.Coinbase,
		BaseTickHeight: baseTickHeight,
		NumUnits:       parts.effectiveUnits,
		TickCount:      parts.ticks,
		Weight:         parts.weight,
		VRFNonce:       types.VRFPostIndex(watx.VRFNonce),
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

	proof, err = h.storeAtx(ctx, atx, watx, marrying, parts.units)
	if err != nil {
		return nil, fmt.Errorf("cannot store atx %s: %w", atx.ShortString(), err)
	}

	events.ReportNewActivation(atx)
	h.logger.Info("new atx", log.ZContext(ctx), zap.Inline(atx), zap.Bool("malicious", proof != nil))
	return proof, err
}

// Syntactically validate an ATX.
func (h *HandlerV2) syntacticallyValidate(ctx context.Context, atx *wire.ActivationTxV2) error {
	if !h.edVerifier.Verify(signing.ATX, atx.SmesherID, atx.SignedBytes(), atx.Signature) {
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

// Validate marriages and return married IDs.
// Note: The order of returned IDs is important and must match the order of the marriage certificates.
// The MarriageIndex in PoST proof matches the index in this marriage slice.
func (h *HandlerV2) validateMarriages(atx *wire.ActivationTxV2) ([]types.NodeID, error) {
	if len(atx.Marriages) == 0 {
		return nil, nil
	}
	marryingIDsSet := make(map[types.NodeID]struct{}, len(atx.Marriages))
	var marryingIDs []types.NodeID // for deterministic order
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
		marryingIDs = append(marryingIDs, id)
	}
	return marryingIDs, nil
}

// Validate marriage ATX and return the full equivocation set.
func (h *HandlerV2) equivocationSet(atx *wire.ActivationTxV2) ([]types.NodeID, error) {
	if atx.MarriageATX == nil {
		return []types.NodeID{atx.SmesherID}, nil
	}
	marriageAtxID, _, err := identities.MarriageInfo(h.cdb, atx.SmesherID)
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
) (*atxParts, *mwire.MalfeasanceProof, error) {
	parts := atxParts{
		units: make(map[types.NodeID]uint32),
	}
	if atx.Initial != nil {
		if err := h.validateCommitmentAtx(h.goldenATXID, atx.Initial.CommitmentATX, atx.PublishEpoch); err != nil {
			return nil, nil, fmt.Errorf("verifying commitment ATX: %w", err)
		}
	}

	previousAtxs := make([]*types.ActivationTx, len(atx.PreviousATXs))
	for i, prev := range atx.PreviousATXs {
		prevAtx, err := atxs.Get(h.cdb, prev)
		if err != nil {
			return nil, nil, fmt.Errorf("fetching previous atx: %w", err)
		}
		if prevAtx.PublishEpoch >= atx.PublishEpoch {
			err := fmt.Errorf("previous atx is too new (%d >= %d) (%s) ", prevAtx.PublishEpoch, atx.PublishEpoch, prev)
			return nil, nil, err
		}
		previousAtxs[i] = prevAtx
	}

	equivocationSet, err := h.equivocationSet(atx)
	if err != nil {
		return nil, nil, fmt.Errorf("calculating equivocation set: %w", err)
	}

	// validate previous ATXs
	nipostSizes := make(nipostSizes, len(atx.NiPosts))
	for i, niposts := range atx.NiPosts {
		nipostSizes[i] = new(nipostSize)
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
					return nil, nil, fmt.Errorf("validating previous atx: %w", err)
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
			nipostChallenge := wire.NIPostChallengeV2{
				PublishEpoch:     atx.PublishEpoch,
				PositioningATXID: atx.PositioningATX,
			}
			if atx.Initial != nil {
				nipostChallenge.InitialPost = &atx.Initial.Post
			} else {
				nipostChallenge.PrevATXID = atx.PreviousATXs[post.PrevATXIndex]
			}
			if _, ok := indexedChallenges[post.MembershipLeafIndex]; !ok {
				indexedChallenges[post.MembershipLeafIndex] = nipostChallenge.Hash().Bytes()
			}
		}

		leafIndicies := make([]uint64, 0, len(indexedChallenges))
		for i := range indexedChallenges {
			leafIndicies = append(leafIndicies, i)
		}
		slices.Sort(leafIndicies)
		poetChallenges := make([][]byte, 0, len(indexedChallenges))
		for _, i := range leafIndicies {
			poetChallenges = append(poetChallenges, indexedChallenges[i])
		}

		membership := types.MultiMerkleProof{
			Nodes:       niposts.Membership.Nodes,
			LeafIndices: leafIndicies,
		}
		leaves, err := h.nipostValidator.PoetMembership(ctx, &membership, niposts.Challenge, poetChallenges)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid poet membership: %w", err)
		}
		nipostSizes[i].ticks = leaves / h.tickSize
	}

	parts.effectiveUnits, parts.weight, err = nipostSizes.sumUp()
	if err != nil {
		return nil, nil, err
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
					return nil, nil, fmt.Errorf("commitment atx not found for ID %s: %w", id, err)
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
			parts.units[id] = post.NumUnits
		}
	}

	if atx.Initial == nil {
		if smesherCommitment == nil {
			return nil, nil, errors.New("ATX signer not present in merged ATX")
		}
		err := h.nipostValidator.VRFNonceV2(atx.SmesherID, *smesherCommitment, atx.VRFNonce, atx.TotalNumUnits())
		if err != nil {
			return nil, nil, fmt.Errorf("validating VRF nonce: %w", err)
		}
	}

	parts.ticks = nipostSizes.minTicks()

	return &parts, nil, nil
}

func (h *HandlerV2) checkMalicious(
	tx *sql.Tx,
	watx *wire.ActivationTxV2,
	marrying []types.NodeID,
) (bool, *mwire.MalfeasanceProof, error) {
	malicious, err := identities.IsMalicious(tx, watx.SmesherID)
	if err != nil {
		return false, nil, fmt.Errorf("checking if node is malicious: %w", err)
	}
	if malicious {
		return true, nil, nil
	}

	proof, err := h.checkDoubleMarry(tx, marrying)
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

func (h *HandlerV2) checkDoubleMarry(tx *sql.Tx, marrying []types.NodeID) (*mwire.MalfeasanceProof, error) {
	for _, id := range marrying {
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
	}
	return nil, nil
}

// Store an ATX in the DB.
// TODO: detect malfeasance and create proofs.
func (h *HandlerV2) storeAtx(
	ctx context.Context,
	atx *types.ActivationTx,
	watx *wire.ActivationTxV2,
	marrying []types.NodeID,
	units map[types.NodeID]uint32,
) (*mwire.MalfeasanceProof, error) {
	var (
		malicious bool
		proof     *mwire.MalfeasanceProof
	)
	if err := h.cdb.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		malicious, proof, err = h.checkMalicious(tx, watx, marrying)
		if err != nil {
			return fmt.Errorf("check malicious: %w", err)
		}

		if len(marrying) != 0 {
			for i, id := range marrying {
				if err := identities.SetMarriage(tx, id, atx.ID(), i); err != nil {
					return err
				}
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
		err = atxs.SetUnits(tx, atx.ID(), units)
		if err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("set atx units: %w", err)
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
		for _, id := range marrying {
			allMalicious[id] = struct{}{}
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
