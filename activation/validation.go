package activation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/merkle-tree"
	poetShared "github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

type ErrAtxNotFound struct {
	Id types.ATXID
	// the source (if any) that caused the error
	source error
}

func (e *ErrAtxNotFound) Error() string {
	return fmt.Sprintf("ATX ID (%v) not found (%v)", e.Id.String(), e.source)
}

func (e *ErrAtxNotFound) Unwrap() error { return e.source }

func (e *ErrAtxNotFound) Is(target error) bool {
	if err, ok := target.(*ErrAtxNotFound); ok {
		return err.Id == e.Id
	}
	return false
}

type validatorOptions struct {
	postSubsetSeed []byte
	prioritized    bool
}

// PostSubset configures the validator to validate only a subset of the POST indices.
// The `seed` is used to randomize the selection of indices.
func PostSubset(seed []byte) validatorOption {
	return func(o *validatorOptions) {
		o.postSubsetSeed = seed
	}
}

func PrioritizeCall() validatorOption {
	return func(o *validatorOptions) {
		o.prioritized = true
	}
}

// Validator contains the dependencies required to validate NIPosts.
type Validator struct {
	db           sql.Executor
	poetDb       poetDbAPI
	cfg          PostConfig
	scrypt       config.ScryptParams
	postVerifier PostVerifier
}

// NewValidator returns a new NIPost validator.
func NewValidator(
	db sql.Executor,
	poetDb poetDbAPI,
	cfg PostConfig,
	scrypt config.ScryptParams,
	postVerifier PostVerifier,
) *Validator {
	return &Validator{db, poetDb, cfg, scrypt, postVerifier}
}

// NIPost validates a NIPost, given a node id and expected challenge. It returns an error if the NIPost is invalid.
//
// Some of the Post metadata fields validation values is ought to eventually be derived from
// consensus instead of local configuration. If so, their validation should be removed to contextual validation,
// while still syntactically-validate them here according to locally configured min/max values.
func (v *Validator) NIPost(
	ctx context.Context,
	nodeId types.NodeID,
	commitmentAtxId types.ATXID,
	nipost *types.NIPost,
	poetChallenge types.Hash32,
	numUnits uint32,
	opts ...validatorOption,
) (uint64, error) {
	if err := v.NumUnits(&v.cfg, numUnits); err != nil {
		return 0, err
	}

	if err := v.LabelsPerUnit(&v.cfg, nipost.PostMetadata.LabelsPerUnit); err != nil {
		return 0, err
	}

	err := v.Post(ctx, nodeId, commitmentAtxId, nipost.Post, nipost.PostMetadata, numUnits, opts...)
	if err != nil {
		return 0, fmt.Errorf("validating Post: %w", err)
	}

	var ref types.PoetProofRef
	copy(ref[:], nipost.PostMetadata.Challenge)
	proof, statement, err := v.poetDb.Proof(ref)
	if err != nil {
		return 0, fmt.Errorf("poet proof is not available %x: %w", ref, err)
	}

	if err := validateMerkleProof(poetChallenge[:], &nipost.Membership, statement[:]); err != nil {
		return 0, fmt.Errorf("invalid membership proof %w", err)
	}

	return proof.LeafCount, nil
}

func (v *Validator) PoetMembership(
	_ context.Context,
	membership *types.MultiMerkleProof,
	postChallenge types.Hash32,
	poetChallenges [][]byte,
) (uint64, error) {
	ref := types.PoetProofRef(postChallenge)
	proof, statement, err := v.poetDb.Proof(ref)
	if err != nil {
		return 0, fmt.Errorf("poet proof %x is not available: %w", ref, err)
	}

	if err := validateMultiMerkleProof(poetChallenges, membership, statement[:]); err != nil {
		return 0, fmt.Errorf("invalid membership proof %w", err)
	}

	return proof.LeafCount, nil
}

func validateMerkleProof(leaf []byte, proof *types.MerkleProof, expectedRoot []byte) error {
	membership := types.MultiMerkleProof{
		Nodes:       proof.Nodes,
		LeafIndices: []uint64{proof.LeafIndex},
	}
	return validateMultiMerkleProof([][]byte{leaf}, &membership, expectedRoot)
}

func validateMultiMerkleProof(leaves [][]byte, proof *types.MultiMerkleProof, expectedRoot []byte) error {
	nodes := make([][]byte, 0, len(proof.Nodes))
	for _, n := range proof.Nodes {
		nodes = append(nodes, n.Bytes())
	}
	ok, err := merkle.ValidatePartialTree(
		proof.LeafIndices,
		leaves,
		nodes,
		expectedRoot,
		poetShared.HashMembershipTreeNode,
	)
	if err != nil {
		return fmt.Errorf("validating merkle proof: %w", err)
	}
	if !ok {
		return fmt.Errorf(
			"invalid merkle proof, calculated root does not match proof root, leaf: %x, nodes: %x, expected root: %x",
			leaves,
			proof.Nodes,
			expectedRoot,
		)
	}
	return nil
}

func (v *Validator) IsVerifyingFullPost() bool {
	return v.cfg.K3 >= v.cfg.K2
}

// Post validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (v *Validator) Post(
	ctx context.Context,
	nodeId types.NodeID,
	commitmentAtxId types.ATXID,
	post *types.Post,
	metadata *types.PostMetadata,
	numUnits uint32,
	opts ...validatorOption,
) error {
	p := (*shared.Proof)(post)

	m := &shared.ProofMetadata{
		NodeId:          nodeId.Bytes(),
		CommitmentAtxId: commitmentAtxId.Bytes(),
		NumUnits:        numUnits,
		Challenge:       metadata.Challenge,
		LabelsPerUnit:   metadata.LabelsPerUnit,
	}

	options := &validatorOptions{}
	for _, opt := range opts {
		opt(options)
	}

	verifyOpts := []verifying.OptionFunc{verifying.WithLabelScryptParams(v.scrypt)}
	if options.postSubsetSeed != nil {
		verifyOpts = append(verifyOpts, verifying.Subset(v.cfg.K3, options.postSubsetSeed))
	}

	callOpts := []postVerifierOptionFunc{WithVerifierOptions(verifyOpts...)}
	if options.prioritized {
		callOpts = append(callOpts, PrioritizedCall())
	}

	start := time.Now()
	if err := v.postVerifier.Verify(ctx, p, m, callOpts...); err != nil {
		return fmt.Errorf("verifying PoST: %w", err)
	}
	metrics.PostVerificationLatency.Observe(time.Since(start).Seconds())
	return nil
}

func (v *Validator) PostV2(
	ctx context.Context,
	nodeId types.NodeID,
	commitmentAtxId types.ATXID,
	post *types.Post,
	challenge []byte,
	numUnits uint32,
	opts ...validatorOption,
) error {
	return v.Post(ctx, nodeId, commitmentAtxId, post, &types.PostMetadata{
		Challenge:     challenge,
		LabelsPerUnit: v.cfg.LabelsPerUnit,
	}, numUnits, opts...)
}

func (*Validator) NumUnits(cfg *PostConfig, numUnits uint32) error {
	if numUnits < cfg.MinNumUnits {
		return fmt.Errorf("invalid `numUnits`; expected: >=%d, given: %d", cfg.MinNumUnits, numUnits)
	}

	if numUnits > cfg.MaxNumUnits {
		return fmt.Errorf("invalid `numUnits`; expected: <=%d, given: %d", cfg.MaxNumUnits, numUnits)
	}
	return nil
}

func (*Validator) LabelsPerUnit(cfg *PostConfig, labelsPerUnit uint64) error {
	if labelsPerUnit < cfg.LabelsPerUnit {
		return fmt.Errorf(
			"invalid `LabelsPerUnit`; expected: >=%d, given: %d",
			cfg.LabelsPerUnit,
			labelsPerUnit,
		)
	}
	return nil
}

func (v *Validator) VRFNonce(
	nodeId types.NodeID,
	commitmentAtxId types.ATXID,
	vrfNonce, labelsPerUnit uint64,
	numUnits uint32,
) error {
	if err := v.LabelsPerUnit(&v.cfg, labelsPerUnit); err != nil {
		return err
	}
	meta := &shared.VRFNonceMetadata{
		NodeId:          nodeId.Bytes(),
		CommitmentAtxId: commitmentAtxId.Bytes(),
		NumUnits:        numUnits,
		LabelsPerUnit:   labelsPerUnit,
	}

	err := verifying.VerifyVRFNonce(&vrfNonce, meta, verifying.WithLabelScryptParams(v.scrypt))
	if err != nil {
		return fmt.Errorf("verify VRF nonce: %w", err)
	}
	return nil
}

func (v *Validator) VRFNonceV2(nodeId types.NodeID, commitment types.ATXID, vrfNonce uint64, numUnits uint32) error {
	return v.VRFNonce(nodeId, commitment, vrfNonce, v.cfg.LabelsPerUnit, numUnits)
}

func (v *Validator) InitialNIPostChallengeV1(
	challenge *wire.NIPostChallengeV1,
	atxs atxProvider,
	goldenATXID types.ATXID,
) error {
	if challenge.CommitmentATXID == nil {
		return errors.New("nil commitment atx in initial post challenge")
	}
	commitmentATXId := *challenge.CommitmentATXID
	if commitmentATXId != goldenATXID {
		commitmentAtx, err := atxs.GetAtx(commitmentATXId)
		if err != nil {
			return &ErrAtxNotFound{Id: commitmentATXId, source: err}
		}
		if challenge.PublishEpoch <= commitmentAtx.PublishEpoch {
			return fmt.Errorf(
				"challenge pubepoch (%v) must be after commitment atx pubepoch (%v)",
				challenge.PublishEpoch,
				commitmentAtx.PublishEpoch,
			)
		}
	}
	return nil
}

func (*Validator) NIPostChallengeV1(
	challenge *wire.NIPostChallengeV1,
	prevATX *types.ActivationTx,
	nodeID types.NodeID,
) error {
	if prevATX.SmesherID != nodeID {
		return fmt.Errorf(
			"previous atx belongs to different miner. nodeID: %v, prevAtx.ID: %v, prevAtx.NodeID: %v",
			nodeID, prevATX.ID().ShortString(), prevATX.SmesherID,
		)
	}

	if prevATX.PublishEpoch >= challenge.PublishEpoch {
		return fmt.Errorf(
			"prevAtx epoch (%d) isn't older than current atx epoch (%d)",
			prevATX.PublishEpoch, challenge.PublishEpoch,
		)
	}

	if prevATX.Sequence+1 != challenge.Sequence {
		return fmt.Errorf(
			"sequence number (%d) is not one more than the prev one (%d)", challenge.Sequence, prevATX.Sequence)
	}
	return nil
}

func (v *Validator) PositioningAtx(
	id types.ATXID,
	atxs atxProvider,
	goldenATXID types.ATXID,
	pubepoch types.EpochID,
) error {
	if id == types.EmptyATXID {
		return errors.New("positioning atx id is empty")
	}
	if id == goldenATXID {
		return nil
	}
	posAtx, err := atxs.GetAtx(id)
	if err != nil {
		return &ErrAtxNotFound{Id: id, source: err}
	}
	if posAtx.PublishEpoch >= pubepoch {
		return fmt.Errorf("positioning atx epoch (%v) must be before %v", posAtx.PublishEpoch, pubepoch)
	}
	return nil
}

type verifyChainOpts struct {
	assumedValidTime time.Time
	trustedNodeID    types.NodeID
	logger           *zap.Logger
}

type verifyChainOptsNs struct{}

var VerifyChainOpts verifyChainOptsNs

type VerifyChainOption func(*verifyChainOpts)

// AssumeValidBefore configures the validator to assume that ATXs received before the given time are valid.
func (verifyChainOptsNs) AssumeValidBefore(val time.Time) VerifyChainOption {
	return func(o *verifyChainOpts) {
		o.assumedValidTime = val
	}
}

// WithTrustedID configures the validator to assume that ATXs created by the given node ID are valid.
func (verifyChainOptsNs) WithTrustedID(val types.NodeID) VerifyChainOption {
	return func(o *verifyChainOpts) {
		o.trustedNodeID = val
	}
}

func (verifyChainOptsNs) WithLogger(log *zap.Logger) VerifyChainOption {
	return func(o *verifyChainOpts) {
		o.logger = log
	}
}

type InvalidChainError struct {
	ID  types.ATXID
	src error
}

func (e *InvalidChainError) Error() string {
	msg := fmt.Sprintf("invalid POST found in ATX chain for ID %v", e.ID.String())
	if e.src != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.src)
	}
	return msg
}

func (e *InvalidChainError) Unwrap() error { return e.src }

func (e *InvalidChainError) Is(target error) bool {
	if err, ok := target.(*InvalidChainError); ok {
		return err.ID == e.ID
	}
	return false
}

func (v *Validator) VerifyChain(ctx context.Context, id, goldenATXID types.ATXID, opts ...VerifyChainOption) error {
	options := verifyChainOpts{
		logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt(&options)
	}
	options.logger.Debug("verifying ATX chain", zap.Stringer("atx_id", id))
	return v.verifyChainWithOpts(ctx, id, goldenATXID, options)
}

type atxDeps struct {
	nipost      types.NIPost
	positioning types.ATXID
	previous    types.ATXID
	commitment  types.ATXID
}

func (v *Validator) getAtxDeps(ctx context.Context, id types.ATXID) (*atxDeps, error) {
	var blob sql.Blob
	version, err := atxs.LoadBlob(ctx, v.db, id.Bytes(), &blob)
	if err != nil {
		return nil, fmt.Errorf("getting blob for %s: %w", id, err)
	}

	switch version {
	case types.AtxV1:
		var commitment types.ATXID
		var atx wire.ActivationTxV1
		if err := codec.Decode(blob.Bytes, &atx); err != nil {
			return nil, fmt.Errorf("decoding ATX blob: %w", err)
		}
		if atx.CommitmentATXID != nil {
			commitment = *atx.CommitmentATXID
		} else {
			catx, err := atxs.CommitmentATX(v.db, atx.SmesherID)
			if err != nil {
				return nil, fmt.Errorf("getting commitment ATX: %w", err)
			}
			commitment = catx
		}

		deps := &atxDeps{
			nipost:      *wire.NiPostFromWireV1(atx.NIPost),
			positioning: atx.PositioningATXID,
			previous:    atx.PrevATXID,
			commitment:  commitment,
		}
		return deps, nil
	case types.AtxV2:
		// TODO: support merged ATXs
		var atx wire.ActivationTxV2
		if err := codec.Decode(blob.Bytes, &atx); err != nil {
			return nil, fmt.Errorf("decoding ATX blob: %w", err)
		}

		var commitment types.ATXID
		if atx.Initial != nil {
			commitment = atx.Initial.CommitmentATX
		} else {
			catx, err := atxs.CommitmentATX(v.db, atx.SmesherID)
			if err != nil {
				return nil, fmt.Errorf("getting commitment ATX: %w", err)
			}
			commitment = catx
		}
		var previous types.ATXID
		if len(atx.PreviousATXs) != 0 {
			previous = atx.PreviousATXs[0]
		}

		deps := &atxDeps{
			nipost: types.NIPost{
				Post: wire.PostFromWireV1(&atx.NiPosts[0].Posts[0].Post),
				PostMetadata: &types.PostMetadata{
					Challenge:     atx.NiPosts[0].Challenge[:],
					LabelsPerUnit: v.cfg.LabelsPerUnit,
				},
			},
			positioning: atx.PositioningATX,
			previous:    previous,
			commitment:  commitment,
		}
		return deps, nil
	}

	return nil, fmt.Errorf("unsupported ATX version: %v", version)
}

func (v *Validator) verifyChainWithOpts(
	ctx context.Context,
	id, goldenATXID types.ATXID,
	opts verifyChainOpts,
) error {
	log := opts.logger
	atx, err := atxs.Get(v.db, id)
	if err != nil {
		return fmt.Errorf("get atx: %w", err)
	}
	if atx.Golden() {
		log.Debug("not verifying ATX chain", zap.Stringer("atx_id", id), zap.String("reason", "golden"))
		return nil
	}

	switch {
	case atx.Validity() == types.Valid:
		log.Debug("not verifying ATX chain", zap.Stringer("atx_id", id), zap.String("reason", "already verified"))
		return nil
	case atx.Validity() == types.Invalid:
		log.Debug("not verifying ATX chain", zap.Stringer("atx_id", id), zap.String("reason", "invalid"))
		return &InvalidChainError{ID: id}
	case atx.Received().Before(opts.assumedValidTime):
		log.Debug(
			"not verifying ATX chain",
			zap.Stringer("atx_id", id),
			zap.String("reason", "assumed valid"),
			zap.Time("received", atx.Received()),
			zap.Time("valid_before", opts.assumedValidTime),
		)
		return nil
	case atx.SmesherID == opts.trustedNodeID:
		log.Debug("not verifying ATX chain", zap.Stringer("atx_id", id), zap.String("reason", "trusted"))
		return nil
	}

	// validate POST fully
	deps, err := v.getAtxDeps(ctx, id)
	if err != nil {
		return fmt.Errorf("getting ATX dependencies: %w", err)
	}

	if err := v.Post(
		ctx,
		atx.SmesherID,
		deps.commitment,
		deps.nipost.Post,
		deps.nipost.PostMetadata,
		atx.NumUnits,
		[]validatorOption{PrioritizeCall()}...,
	); err != nil {
		if err := atxs.SetValidity(v.db, id, types.Invalid); err != nil {
			log.Warn("failed to persist atx validity", zap.Error(err), zap.Stringer("atx_id", id))
		}
		return &InvalidChainError{ID: id, src: err}
	}

	err = v.verifyChainDeps(ctx, deps, goldenATXID, opts)
	invalidChain := &InvalidChainError{}
	switch {
	case err == nil:
		if err := atxs.SetValidity(v.db, id, types.Valid); err != nil {
			log.Warn("failed to persist atx validity", zap.Error(err), zap.Stringer("atx_id", id))
		}
	case errors.As(err, &invalidChain):
		if err := atxs.SetValidity(v.db, id, types.Invalid); err != nil {
			log.Warn("failed to persist atx validity", zap.Error(err), zap.Stringer("atx_id", id))
		}
	}
	return err
}

func (v *Validator) verifyChainDeps(
	ctx context.Context,
	deps *atxDeps,
	goldenATXID types.ATXID,
	opts verifyChainOpts,
) error {
	if deps.previous != types.EmptyATXID {
		if err := v.verifyChainWithOpts(ctx, deps.previous, goldenATXID, opts); err != nil {
			return fmt.Errorf("validating previous ATX %s chain: %w", deps.previous.ShortString(), err)
		}
	}
	if deps.positioning != goldenATXID {
		if err := v.verifyChainWithOpts(ctx, deps.positioning, goldenATXID, opts); err != nil {
			return fmt.Errorf("validating positioning ATX %s chain: %w", deps.positioning.ShortString(), err)
		}
	}
	// verify commitment only if arrived at the first ATX in the chain
	// to avoid verifying the same commitment ATX multiple times.
	if deps.previous == types.EmptyATXID && deps.commitment != goldenATXID {
		if err := v.verifyChainWithOpts(ctx, deps.commitment, goldenATXID, opts); err != nil {
			return fmt.Errorf("validating commitment ATX %s chain: %w", deps.commitment.ShortString(), err)
		}
	}
	return nil
}
