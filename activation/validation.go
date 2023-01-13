package activation

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/spacemeshos/post/proving"
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
func (v *Validator) NIPost(nodeId types.NodeID, commitmentAtxId types.ATXID, nipost *types.NIPost, expectedChallenge types.Hash32, numUnits uint32) (uint64, error) {
	if !bytes.Equal(nipost.Challenge[:], expectedChallenge[:]) {
		return 0, fmt.Errorf("invalid `Challenge`; expected: %x, given: %x", expectedChallenge, nipost.Challenge)
	}

	if err := v.NumUnits(&v.cfg, numUnits); err != nil {
		return 0, err
	}

	if err := v.PostMetadata(&v.cfg, nipost.PostMetadata); err != nil {
		return 0, err
	}

	proof, err := v.poetDb.GetProof(nipost.PostMetadata.Challenge)
	if err != nil {
		return 0, fmt.Errorf("poet proof is not available %x: %w", nipost.PostMetadata.Challenge, err)
	}

	if !contains(proof, nipost.Challenge.Bytes()) {
		return 0, fmt.Errorf("challenge is not included in the proof %x", nipost.PostMetadata.Challenge)
	}

	if err := v.Post(nodeId, commitmentAtxId, nipost.Post, nipost.PostMetadata, numUnits); err != nil {
		return 0, fmt.Errorf("invalid Post: %v", err)
	}

	return proof.LeafCount, nil
}

func contains(proof *types.PoetProof, member []byte) bool {
	for _, part := range proof.Members {
		if bytes.Equal(part, member) {
			return true
		}
	}
	return false
}

// Post validates a Proof of Space-Time (PoST). It returns nil if validation passed or an error indicating why
// validation failed.
func (*Validator) Post(nodeId types.NodeID, commitmentAtxId types.ATXID, PoST *types.Post, PostMetadata *types.PostMetadata, numUnits uint32) error {
	p := (*proving.Proof)(PoST)

	m := &proving.ProofMetadata{
		NodeId:          nodeId.Bytes(),
		CommitmentAtxId: commitmentAtxId.Bytes(),
		NumUnits:        numUnits,
		Challenge:       PostMetadata.Challenge,
		BitsPerLabel:    PostMetadata.BitsPerLabel,
		LabelsPerUnit:   PostMetadata.LabelsPerUnit,
		K1:              PostMetadata.K1,
		K2:              PostMetadata.K2,
	}

	if err := verifying.Verify(p, m); err != nil {
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
	if metadata.BitsPerLabel < cfg.BitsPerLabel {
		return fmt.Errorf("invalid `BitsPerLabel`; expected: >=%d, given: %d", cfg.BitsPerLabel, metadata.BitsPerLabel)
	}

	if metadata.LabelsPerUnit < uint64(cfg.LabelsPerUnit) {
		return fmt.Errorf("invalid `LabelsPerUnit`; expected: >=%d, given: %d", cfg.LabelsPerUnit, metadata.LabelsPerUnit)
	}

	if metadata.K1 > cfg.K1 {
		return fmt.Errorf("invalid `K1`; expected: <=%d, given: %d", cfg.K1, metadata.K1)
	}

	if metadata.K2 < cfg.K2 {
		return fmt.Errorf("invalid `K2`; expected: >=%d, given: %d", cfg.K2, metadata.K2)
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
		BitsPerLabel:    PostMetadata.BitsPerLabel,
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
		if !challenge.PubLayerID.After(commitmentAtx.PubLayerID) {
			return fmt.Errorf("challenge publayer (%v) must be after commitment atx publayer (%v)", challenge.PubLayerID, commitmentAtx.PubLayerID)
		}
	}

	if *challenge.CommitmentATX == goldenATXID && !challenge.PublishEpoch().IsGenesis() {
		return fmt.Errorf("golden atx used for commitment atx in epoch %d, but is only valid in epoch 1", challenge.PublishEpoch())
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

	if prevATX.PublishEpoch() >= challenge.PublishEpoch() {
		return fmt.Errorf(
			"prevAtx epoch (%v, layer %v) isn't older than current atx epoch (%v, layer %v)",
			prevATX.PublishEpoch(), prevATX.PubLayerID, challenge.PublishEpoch(), challenge.PubLayerID,
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

func (*Validator) PositioningAtx(id *types.ATXID, atxs atxProvider, goldenATXID types.ATXID, publayer types.LayerID, layersPerEpoch uint32) error {
	if *id == *types.EmptyATXID {
		return fmt.Errorf("empty positioning atx")
	}

	if *id == goldenATXID && !publayer.GetEpoch().IsGenesis() {
		return fmt.Errorf("golden atx used for positioning atx in epoch %d, but is only valid in epoch 1", publayer.GetEpoch())
	}

	if *id != goldenATXID {
		posAtx, err := atxs.GetAtxHeader(*id)
		if err != nil {
			return &ErrAtxNotFound{Id: *id, source: err}
		}
		if !posAtx.PubLayerID.Before(publayer) {
			return fmt.Errorf("positioning atx layer (%v) must be before %v", posAtx.PubLayerID, publayer)
		}
		if d := publayer.Difference(posAtx.PubLayerID); d > layersPerEpoch {
			return fmt.Errorf("expected distance of one epoch (%v layers) from positioning atx but found %v", layersPerEpoch, d)
		}
	}

	return nil
}
