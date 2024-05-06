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
}

// PostSubset configures the validator to validate only a subset of the POST indices.
// The `seed` is used to randomize the selection of indices.
func PostSubset(seed []byte) validatorOption {
	return func(o *validatorOptions) {
		o.postSubsetSeed = seed
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
	expectedChallenge types.Hash32,
	numUnits uint32,
	opts ...validatorOption,
) (uint64, error) {
	if err := v.NumUnits(&v.cfg, numUnits); err != nil {
		return 0, err
	}

	if err := v.PostMetadata(&v.cfg, nipost.PostMetadata); err != nil {
		return 0, err
	}

	if err := v.Post(ctx, nodeId, commitmentAtxId, nipost.Post, nipost.PostMetadata, numUnits, opts...); err != nil {
		return 0, fmt.Errorf("invalid Post: %w", err)
	}

	var ref types.PoetProofRef
	copy(ref[:], nipost.PostMetadata.Challenge)
	proof, statement, err := v.poetDb.Proof(ref)
	if err != nil {
		return 0, fmt.Errorf("poet proof is not available %x: %w", nipost.PostMetadata.Challenge, err)
	}

	if err := validateMerkleProof(expectedChallenge[:], &nipost.Membership, statement[:]); err != nil {
		return 0, fmt.Errorf("invalid membership proof %w", err)
	}

	return proof.LeafCount, nil
}

func validateMerkleProof(leaf []byte, proof *types.MerkleProof, expectedRoot []byte) error {
	nodes := make([][]byte, 0, len(proof.Nodes))
	for _, n := range proof.Nodes {
		nodes = append(nodes, n.Bytes())
	}
	ok, err := merkle.ValidatePartialTree(
		[]uint64{proof.LeafIndex},
		[][]byte{leaf},
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
			leaf,
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
	PoST *types.Post,
	PostMetadata *types.PostMetadata,
	numUnits uint32,
	opts ...validatorOption,
) error {
	p := (*shared.Proof)(PoST)

	m := &shared.ProofMetadata{
		NodeId:          nodeId.Bytes(),
		CommitmentAtxId: commitmentAtxId.Bytes(),
		NumUnits:        numUnits,
		Challenge:       PostMetadata.Challenge,
		LabelsPerUnit:   PostMetadata.LabelsPerUnit,
	}

	options := &validatorOptions{}
	for _, opt := range opts {
		opt(options)
	}
	verifyOpts := []verifying.OptionFunc{verifying.WithLabelScryptParams(v.scrypt)}
	if options.postSubsetSeed != nil {
		verifyOpts = append(verifyOpts, verifying.Subset(v.cfg.K3, options.postSubsetSeed))
	}

	start := time.Now()
	if err := v.postVerifier.Verify(ctx, p, m, verifyOpts...); err != nil {
		return fmt.Errorf("verify PoST: %w", err)
	}
	metrics.PostVerificationLatency.Observe(time.Since(start).Seconds())
	return nil
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

func (*Validator) PostMetadata(cfg *PostConfig, metadata *types.PostMetadata) error {
	if metadata.LabelsPerUnit < cfg.LabelsPerUnit {
		return fmt.Errorf(
			"invalid `LabelsPerUnit`; expected: >=%d, given: %d",
			cfg.LabelsPerUnit,
			metadata.LabelsPerUnit,
		)
	}
	return nil
}

func (v *Validator) VRFNonce(
	nodeId types.NodeID,
	commitmentAtxId types.ATXID,
	vrfNonce *types.VRFPostIndex,
	PostMetadata *types.PostMetadata,
	numUnits uint32,
) error {
	if vrfNonce == nil {
		return errors.New("VRFNonce is nil")
	}

	meta := &shared.VRFNonceMetadata{
		NodeId:          nodeId.Bytes(),
		CommitmentAtxId: commitmentAtxId.Bytes(),
		NumUnits:        numUnits,
		LabelsPerUnit:   PostMetadata.LabelsPerUnit,
	}

	err := verifying.VerifyVRFNonce((*uint64)(vrfNonce), meta, verifying.WithLabelScryptParams(v.scrypt))
	if err != nil {
		return fmt.Errorf("verify VRF nonce: %w", err)
	}
	return nil
}

func (v *Validator) InitialNIPostChallenge(
	challenge *types.NIPostChallenge,
	atxs atxProvider,
	goldenATXID types.ATXID,
) error {
	if challenge.CommitmentATX == nil {
		return errors.New("nil commitment atx in initial post challenge")
	}

	if *challenge.CommitmentATX != goldenATXID {
		commitmentAtx, err := atxs.GetAtxHeader(*challenge.CommitmentATX)
		if err != nil {
			return &ErrAtxNotFound{Id: *challenge.CommitmentATX, source: err}
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

func (*Validator) NIPostChallenge(challenge *types.NIPostChallenge, atxs atxProvider, nodeID types.NodeID) error {
	prevATX, err := atxs.GetAtxHeader(challenge.PrevATXID)
	if err != nil {
		return &ErrAtxNotFound{Id: challenge.PrevATXID, source: err}
	}

	if prevATX.NodeID != nodeID {
		return fmt.Errorf(
			"previous atx belongs to different miner. nodeID: %v, prevAtx.ID: %v, prevAtx.NodeID: %v",
			nodeID, prevATX.ID.ShortString(), prevATX.NodeID,
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
	posAtx, err := atxs.GetAtxHeader(id)
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
	options.logger.Info("verifying ATX chain", zap.Stringer("atx_id", id))
	return v.verifyChainWithOpts(ctx, id, goldenATXID, options)
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
	commitmentAtxId := atx.CommitmentATX
	if commitmentAtxId == nil {
		if atxId, err := atxs.CommitmentATX(v.db, atx.SmesherID); err != nil {
			return fmt.Errorf("getting commitment atx: %w", err)
		} else {
			commitmentAtxId = &atxId
		}
	}
	if err := v.Post(
		ctx,
		atx.SmesherID,
		*commitmentAtxId,
		atx.NIPost.Post,
		atx.NIPost.PostMetadata,
		atx.NumUnits,
	); err != nil {
		if err := atxs.SetValidity(v.db, id, types.Invalid); err != nil {
			log.Warn("failed to persist atx validity", zap.Error(err), zap.Stringer("atx_id", id))
		}
		return &InvalidChainError{ID: id, src: err}
	}

	err = v.verifyChainDeps(ctx, atx.ActivationTx, goldenATXID, opts)
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
	atx *types.ActivationTx,
	goldenATXID types.ATXID,
	opts verifyChainOpts,
) error {
	if atx.PrevATXID != types.EmptyATXID {
		if err := v.verifyChainWithOpts(ctx, atx.PrevATXID, goldenATXID, opts); err != nil {
			return fmt.Errorf("validating previous ATX %s chain: %w", atx.PrevATXID.ShortString(), err)
		}
	}
	if atx.PositioningATX != goldenATXID {
		if err := v.verifyChainWithOpts(ctx, atx.PositioningATX, goldenATXID, opts); err != nil {
			return fmt.Errorf("validating positioning ATX %s chain: %w", atx.PositioningATX.ShortString(), err)
		}
	}
	if atx.CommitmentATX != nil && *atx.CommitmentATX != goldenATXID {
		if err := v.verifyChainWithOpts(ctx, *atx.CommitmentATX, goldenATXID, opts); err != nil {
			return fmt.Errorf("validating commitment ATX %s chain: %w", atx.CommitmentATX.ShortString(), err)
		}
	}
	return nil
}
