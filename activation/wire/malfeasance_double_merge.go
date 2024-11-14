package wire

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

//go:generate scalegen

// ProofDoubleMerge is a proof that two distinct ATXs published in the same epoch
// contain the same marriage ATX.
//
// We are proving the following:
//  1. The ATXs have different IDs.
//  2. Both ATXs have a valid signature.
//  3. Both ATXs contain the same marriage ATX.
//  4. Both ATXs were published in the same epoch.
//  5. Signers of both ATXs are married - to prevent banning others by
//     publishing an ATX with the same marriage ATX.
type ProofDoubleMerge struct {
	// PublishEpoch and its proof that it is contained in the ATX.
	PublishEpoch types.EpochID

	// MarriageATXID is the ID of the marriage ATX.
	MarriageATX types.ATXID
	// MarriageATXSmesherID is the ID of the smesher that published the marriage ATX.
	MarriageATXSmesherID types.NodeID

	// ATXID1 is the ID of the ATX being proven.
	ATXID1 types.ATXID
	// SmesherID1 is the ID of the smesher that published the ATX.
	SmesherID1 types.NodeID
	// Signature1 is the signature of the ATXID by the smesher.
	Signature1 types.EdSignature
	// PublishEpochProof1 is the proof that the publish epoch is contained in the ATX.
	PublishEpochProof1 PublishEpochProof `scale:"max=32"`
	// MarriageATXProof1 is the proof that MarriageATX is contained in the ATX.
	MarriageATXProof1 MarriageATXProof `scale:"max=32"`
	// SmesherID1MarryProof is the proof that they married in MarriageATX.
	SmesherID1MarryProof MarryProof

	// ATXID2 is the ID of the ATX being proven.
	ATXID2 types.ATXID
	// SmesherID is the ID of the smesher that published the ATX.
	SmesherID2 types.NodeID
	// Signature2 is the signature of the ATXID by the smesher.
	Signature2 types.EdSignature
	// PublishEpochProof2 is the proof that the publish epoch is contained in the ATX.
	PublishEpochProof2 PublishEpochProof `scale:"max=32"`
	// MarriageATXProof1 is the proof that MarriageATX is contained in the ATX.
	MarriageATXProof2 MarriageATXProof `scale:"max=32"`
	// SmesherID1MarryProof is the proof that they married in MarriageATX.
	SmesherID2MarryProof MarryProof
}

var _ Proof = &ProofDoubleMerge{}

func NewDoubleMergeProof(db sql.Executor, atx1, atx2 *ActivationTxV2) (*ProofDoubleMerge, error) {
	if atx1.ID() == atx2.ID() {
		return nil, errors.New("ATXs have the same ID")
	}
	if atx1.SmesherID == atx2.SmesherID {
		return nil, errors.New("ATXs have the same smesher ID")
	}
	if atx1.PublishEpoch != atx2.PublishEpoch {
		return nil, fmt.Errorf("ATXs have different publish epoch (%v != %v)", atx1.PublishEpoch, atx2.PublishEpoch)
	}
	if atx1.MarriageATX == nil {
		return nil, errors.New("ATX 1 have no marriage ATX")
	}
	if atx2.MarriageATX == nil {
		return nil, errors.New("ATX 2 have no marriage ATX")
	}
	if *atx1.MarriageATX != *atx2.MarriageATX {
		return nil, errors.New("ATXs have different marriage ATXs")
	}

	var blob sql.Blob
	v, err := atxs.LoadBlob(context.Background(), db, atx1.MarriageATX.Bytes(), &blob)
	if err != nil {
		return nil, fmt.Errorf("get marriage ATX: %w", err)
	}
	if v != types.AtxV2 {
		return nil, errors.New("invalid ATX version for marriage ATX")
	}
	marriageATX, err := DecodeAtxV2(blob.Bytes)
	if err != nil {
		return nil, fmt.Errorf("decode marriage ATX: %w", err)
	}

	marriageProof1, err := createMarryProof(db, marriageATX, atx1.SmesherID)
	if err != nil {
		return nil, fmt.Errorf("NodeID marriage proof: %w", err)
	}
	marriageProof2, err := createMarryProof(db, marriageATX, atx2.SmesherID)
	if err != nil {
		return nil, fmt.Errorf("SmesherID marriage proof: %w", err)
	}

	proof := ProofDoubleMerge{
		PublishEpoch:         atx1.PublishEpoch,
		MarriageATX:          marriageATX.ID(),
		MarriageATXSmesherID: marriageATX.SmesherID,

		ATXID1:               atx1.ID(),
		SmesherID1:           atx1.SmesherID,
		Signature1:           atx1.Signature,
		PublishEpochProof1:   atx1.PublishEpochProof(),
		MarriageATXProof1:    atx1.MarriageATXProof(),
		SmesherID1MarryProof: marriageProof1,

		ATXID2:               atx2.ID(),
		SmesherID2:           atx2.SmesherID,
		Signature2:           atx2.Signature,
		PublishEpochProof2:   atx2.PublishEpochProof(),
		MarriageATXProof2:    atx2.MarriageATXProof(),
		SmesherID2MarryProof: marriageProof2,
	}

	return &proof, nil
}

func (p *ProofDoubleMerge) Valid(_ context.Context, edVerifier MalfeasanceValidator) (types.NodeID, error) {
	// 1. The ATXs have different IDs.
	if p.ATXID1 == p.ATXID2 {
		return types.EmptyNodeID, errors.New("ATXs have the same ID")
	}

	// 2. Both ATXs have a valid signature.
	if !edVerifier.Signature(signing.ATX, p.SmesherID1, p.ATXID1.Bytes(), p.Signature1) {
		return types.EmptyNodeID, errors.New("ATX 1 invalid signature")
	}
	if !edVerifier.Signature(signing.ATX, p.SmesherID2, p.ATXID2.Bytes(), p.Signature2) {
		return types.EmptyNodeID, errors.New("ATX 2 invalid signature")
	}

	// 3. and 4. publish epoch is contained in the ATXs
	if !p.PublishEpochProof1.Valid(p.ATXID1, p.PublishEpoch) {
		return types.EmptyNodeID, errors.New("ATX 1 invalid publish epoch proof")
	}
	if !p.PublishEpochProof2.Valid(p.ATXID2, p.PublishEpoch) {
		return types.EmptyNodeID, errors.New("ATX 2 invalid publish epoch proof")
	}

	// 5. signers are married
	if !p.MarriageATXProof1.Valid(p.ATXID1, p.MarriageATX) {
		return types.EmptyNodeID, errors.New("ATX 1 invalid marriage ATX proof")
	}
	err := p.SmesherID1MarryProof.Valid(edVerifier, p.MarriageATX, p.MarriageATXSmesherID, p.SmesherID1)
	if err != nil {
		return types.EmptyNodeID, errors.New("ATX 1 invalid marriage ATX proof")
	}
	if !p.MarriageATXProof2.Valid(p.ATXID2, p.MarriageATX) {
		return types.EmptyNodeID, errors.New("ATX 2 invalid marriage ATX proof")
	}
	err = p.SmesherID2MarryProof.Valid(edVerifier, p.MarriageATX, p.MarriageATXSmesherID, p.SmesherID2)
	if err != nil {
		return types.EmptyNodeID, errors.New("ATX 2 invalid marriage ATX proof")
	}

	return p.SmesherID1, nil
}
