package types

import (
	"errors"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen -types MalfeasanceProof,MalfeasanceGossip,AtxProof,BallotProof,HareProof,AtxProofMsg,BallotProofMsg,HareProofMsg,HareMetadata

const (
	MultipleATXs uint8 = iota + 1
	MultipleBallots
	HareEquivocation
)

type MalfeasanceProof struct {
	// for network upgrade
	Layer     LayerID
	ProofData TypedProof
}

type TypedProof struct {
	// MultipleATXs | MultipleBallots | HareEquivocation
	Type uint8
	// AtxProof | BallotProof | HareProof
	Proof scale.Type
}

func (e *TypedProof) EncodeScale(enc *scale.Encoder) (int, error) {
	var total int
	{
		// not compact, as scale spec uses "full" uint8 for enums
		n, err := scale.EncodeByte(enc, e.Type)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := e.Proof.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (e *TypedProof) DecodeScale(dec *scale.Decoder) (int, error) {
	var total int
	{
		typ, n, err := scale.DecodeByte(dec)
		if err != nil {
			return total, err
		}
		e.Type = typ
		total += n
	}
	switch e.Type {
	case MultipleATXs:
		var proof AtxProof
		n, err := proof.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		e.Proof = &proof
		total += n
	case MultipleBallots:
		var proof BallotProof
		n, err := proof.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		e.Proof = &proof
		total += n
	case HareEquivocation:
		var proof HareProof
		n, err := proof.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		e.Proof = &proof
		total += n
	default:
		return total, errors.New("unknown malfeasance type")
	}
	return total, nil
}

type MalfeasanceGossip struct {
	MalfeasanceProof
	Eligibility *HareEligibility // optional, only useful in live hare rounds
}

type AtxProof struct {
	Messages [2]AtxProofMsg
}

type BallotProof struct {
	Messages [2]BallotProofMsg
}

type HareProof struct {
	Messages [2]HareProofMsg
}

type AtxProofMsg struct {
	InnerMsg  ATXMetadata
	Signature []byte
}

// SignedBytes returns the actual data being signed in a AtxProofMsg.
func (m *AtxProofMsg) SignedBytes() []byte {
	data, err := codec.Encode(&m.InnerMsg)
	if err != nil {
		log.With().Fatal("failed to serialize AtxProofMsg", log.Err(err))
	}
	return data
}

type BallotProofMsg struct {
	InnerMsg  BallotMetadata
	Signature []byte
}

// SignedBytes returns the actual data being signed in a BallotProofMsg.
func (m *BallotProofMsg) SignedBytes() []byte {
	data, err := codec.Encode(&m.InnerMsg)
	if err != nil {
		log.With().Fatal("failed to serialize MultiBlockProposalsMsg", log.Err(err))
	}
	return data
}

type HareMetadata struct {
	Layer LayerID
	// the round counter (K)
	Round uint32
	// hash of hare.Message.InnerMessage
	MsgHash Hash32
}

type HareProofMsg struct {
	InnerMsg  HareMetadata
	Signature []byte
}

// SignedBytes returns the actual data being signed in a HareProofMsg.
func (m *HareProofMsg) SignedBytes() []byte {
	data, err := codec.Encode(&m.InnerMsg)
	if err != nil {
		log.With().Fatal("failed to serialize MultiBlockProposalsMsg", log.Err(err))
	}
	return data
}
