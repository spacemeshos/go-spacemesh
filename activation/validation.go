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

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
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

// Validator contains the dependencies required to validate NIPosts.
type Validator struct {
	poetDb       poetDbAPI
	cfg          PostConfig
	scrypt       config.ScryptParams
	postVerifier PostVerifier
}

// NewValidator returns a new NIPost validator.
func NewValidator(poetDb poetDbAPI, cfg PostConfig, scrypt config.ScryptParams, postVerifier PostVerifier) *Validator {
	return &Validator{poetDb, cfg, scrypt, postVerifier}
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
) (uint64, error) {
	if err := v.NumUnits(&v.cfg, numUnits); err != nil {
		return 0, err
	}

	if err := v.PostMetadata(&v.cfg, nipost.PostMetadata); err != nil {
		return 0, err
	}

	if err := v.Post(ctx, nodeId, commitmentAtxId, nipost.Post, nipost.PostMetadata, numUnits); err != nil {
		return 0, fmt.Errorf("invalid Post: %w", err)
	}

	var ref types.PoetProofRef
	copy(ref[:], nipost.PostMetadata.Challenge)
	proof, statement, err := v.poetDb.GetProof(ref)
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
		hexNodes := make([]string, 0, len(proof.Nodes))
		for _, n := range proof.Nodes {
			hexNodes = append(hexNodes, n.Hex())
		}
		return fmt.Errorf(
			"invalid merkle proof, calculated root does not match the proof root, leaf: %v, nodes: %v, expected root: %v",
			util.Encode(leaf),
			hexNodes,
			util.Encode(expectedRoot),
		)
	}
	return nil
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
) error {
	p := (*shared.Proof)(PoST)

	m := &shared.ProofMetadata{
		NodeId:          nodeId.Bytes(),
		CommitmentAtxId: commitmentAtxId.Bytes(),
		NumUnits:        numUnits,
		Challenge:       PostMetadata.Challenge,
		LabelsPerUnit:   PostMetadata.LabelsPerUnit,
	}

	start := time.Now()
	if err := v.postVerifier.Verify(ctx, p, m,
		verifying.WithPowCreator(nodeId.Bytes()), verifying.WithLabelScryptParams(v.scrypt),
	); err != nil {
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

	if err := verifying.VerifyVRFNonce((*uint64)(vrfNonce), meta,
		verifying.WithPowCreator(nodeId.Bytes()), verifying.WithLabelScryptParams(v.scrypt),
	); err != nil {
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
		return fmt.Errorf("sequence number is not one more than prev sequence number")
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
