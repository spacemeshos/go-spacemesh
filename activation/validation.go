package activation

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
	poetDb poetDbAPI
	cfg    PostConfig
}

// NewValidator returns a new NIPost validator.
func NewValidator(poetDb poetDbAPI, cfg PostConfig) *Validator {
	return &Validator{poetDb, cfg}
}

// NIPost validates a NIPost, given a node id and expected challenge. It returns an error if the NIPost is invalid.
//
// Some of the Post metadata fields validation values is ought to eventually be derived from
// consensus instead of local configuration. If so, their validation should be removed to contextual validation,
// while still syntactically-validate them here according to locally configured min/max values.
func (v *Validator) NIPost(nodeId types.NodeID, commitmentAtxId types.ATXID, nipost *types.NIPost, expectedChallenge types.Hash32, numUnits uint32, opts ...verifying.OptionFunc) (uint64, error) {
	if !bytes.Equal(nipost.Challenge[:], expectedChallenge[:]) {
		return 0, fmt.Errorf("invalid `Challenge`; expected: %x, given: %x", expectedChallenge, nipost.Challenge)
	}

	if err := v.NumUnits(&v.cfg, numUnits); err != nil {
		return 0, err
	}

	if err := v.PostMetadata(&v.cfg, nipost.PostMetadata); err != nil {
		return 0, err
	}

	var ref types.PoetProofRef
	copy(ref[:], nipost.PostMetadata.Challenge)
	proof, err := v.poetDb.GetProof(ref)
	if err != nil {
		return 0, fmt.Errorf("poet proof is not available %x: %w", nipost.PostMetadata.Challenge, err)
	}

	if !contains(proof, nipost.Challenge.Bytes()) {
		return 0, fmt.Errorf("challenge is not included in the proof %x", nipost.PostMetadata.Challenge)
	}

	if err := v.Post(nodeId, commitmentAtxId, nipost.Post, nipost.PostMetadata, numUnits, opts...); err != nil {
		return 0, fmt.Errorf("invalid Post: %v", err)
	}

	return proof.LeafCount, nil
}

func contains(proof *types.PoetProof, member []byte) bool {
	for _, part := range proof.Members {
		if bytes.Equal(part[:], member) {
			return true
		}
	}
	return false
}

// Post validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (v *Validator) Post(nodeId types.NodeID, commitmentAtxId types.ATXID, PoST *types.Post, PostMetadata *types.PostMetadata, numUnits uint32, opts ...verifying.OptionFunc) error {
	p := (*shared.Proof)(PoST)

	m := &shared.ProofMetadata{
		NodeId:          nodeId.Bytes(),
		CommitmentAtxId: commitmentAtxId.Bytes(),
		NumUnits:        numUnits,
		Challenge:       PostMetadata.Challenge,
		LabelsPerUnit:   PostMetadata.LabelsPerUnit,
	}

	if err := verifying.Verify(p, m, (config.Config)(v.cfg), opts...); err != nil {
		return fmt.Errorf("verify PoST: %w", err)
	}

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
	if metadata.LabelsPerUnit < uint64(cfg.LabelsPerUnit) {
		return fmt.Errorf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", cfg.LabelsPerUnit, metadata.LabelsPerUnit)
	}
	return nil
}

func (*Validator) VRFNonce(nodeId types.NodeID, commitmentAtxId types.ATXID, vrfNonce *types.VRFPostIndex, PostMetadata *types.PostMetadata, numUnits uint32) error {
	if vrfNonce == nil {
		return errors.New("VRFNonce is nil")
	}

	meta := &shared.VRFNonceMetadata{
		NodeId:          nodeId.Bytes(),
		CommitmentAtxId: commitmentAtxId.Bytes(),
		NumUnits:        numUnits,
		LabelsPerUnit:   PostMetadata.LabelsPerUnit,
	}

	if err := verifying.VerifyVRFNonce((*uint64)(vrfNonce), meta); err != nil {
		return fmt.Errorf("verify VRF nonce: %w", err)
	}
	return nil
}

func (*Validator) InitialNIPostChallenge(challenge *types.NIPostChallenge, atxs atxProvider, goldenATXID types.ATXID, expectedPostIndices []byte) error {
	if challenge.Sequence != 0 {
		return fmt.Errorf("no prevATX declared, but sequence number not zero")
	}

	if challenge.InitialPostIndices == nil {
		return fmt.Errorf("no prevATX declared, but initial Post indices is not included in challenge")
	}

	if !bytes.Equal(expectedPostIndices, challenge.InitialPostIndices) {
		return fmt.Errorf("initial Post indices included in challenge does not equal to the initial Post indices included in the atx")
	}

	if challenge.CommitmentATX == nil {
		return fmt.Errorf("no prevATX declared, but commitmentATX is missing")
	}

	if *challenge.CommitmentATX != goldenATXID {
		commitmentAtx, err := atxs.GetAtxHeader(*challenge.CommitmentATX)
		if err != nil {
			return &ErrAtxNotFound{Id: *challenge.CommitmentATX, source: err}
		}
		if challenge.PublishEpoch <= commitmentAtx.PublishEpoch {
			return fmt.Errorf("challenge pubepoch (%v) must be after commitment atx pubepoch (%v)", challenge.PublishEpoch, commitmentAtx.PublishEpoch)
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

	if challenge.InitialPostIndices != nil {
		return fmt.Errorf("prevATX declared, but initial Post indices is included in challenge")
	}

	if challenge.CommitmentATX != nil {
		return fmt.Errorf("prevATX declared, but commitmentATX is included")
	}

	return nil
}

func (*Validator) PositioningAtx(id *types.ATXID, atxs atxProvider, goldenATXID types.ATXID, pubepoch types.EpochID, layersPerEpoch uint32) error {
	if *id == types.EmptyATXID {
		return fmt.Errorf("empty positioning atx")
	}
	if *id == goldenATXID {
		return nil
	}
	posAtx, err := atxs.GetAtxHeader(*id)
	if err != nil {
		return &ErrAtxNotFound{Id: *id, source: err}
	}
	if posAtx.PublishEpoch >= pubepoch {
		return fmt.Errorf("positioning atx epoch (%v) must be before %v", posAtx.PublishEpoch, pubepoch)
	}
	return nil
}
