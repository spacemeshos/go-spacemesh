package types

import (
	"errors"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen -types MalfeasanceProof,MalfeasanceGossip,AtxProof,BallotProof,HareProof,AtxProofMsg,BallotProofMsg,HareProofMsg,HareMetadata

const (
	MultipleATXs byte = iota + 1
	MultipleBallots
	HareEquivocation
)

type MalfeasanceProof struct {
	// for network upgrade
	Layer LayerID
	Proof Proof
}

func (mp *MalfeasanceProof) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("generated_layer", mp.Layer.Uint32())
	switch mp.Proof.Type {
	case MultipleATXs:
		encoder.AddString("type", "multiple atxs")
		p, ok := mp.Proof.Data.(*AtxProof)
		if !ok {
			encoder.AddString("msgs", "n/a")
		} else {
			encoder.AddObject("msgs", p)
		}
	case MultipleBallots:
		encoder.AddString("type", "multiple ballots")
		p, ok := mp.Proof.Data.(*BallotProof)
		if !ok {
			encoder.AddString("msgs", "n/a")
		} else {
			encoder.AddObject("msgs", p)
		}
	case HareEquivocation:
		encoder.AddString("type", "hare equivocation")
		p, ok := mp.Proof.Data.(*HareProof)
		if !ok {
			encoder.AddString("msgs", "n/a")
		} else {
			encoder.AddObject("msgs", p)
		}
	default:
		encoder.AddString("type", "unknown")
	}

	return nil
}

type Proof struct {
	// MultipleATXs | MultipleBallots | HareEquivocation
	Type uint8
	// AtxProof | BallotProof | HareProof
	Data scale.Type
}

func (e *Proof) EncodeScale(enc *scale.Encoder) (int, error) {
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
		n, err := e.Data.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (e *Proof) DecodeScale(dec *scale.Decoder) (int, error) {
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
		e.Data = &proof
		total += n
	case MultipleBallots:
		var proof BallotProof
		n, err := proof.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		e.Data = &proof
		total += n
	case HareEquivocation:
		var proof HareProof
		n, err := proof.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		e.Data = &proof
		total += n
	default:
		return total, errors.New("unknown malfeasance type")
	}
	return total, nil
}

type MalfeasanceGossip struct {
	MalfeasanceProof
	Eligibility *HareEligibilityGossip // optional, only useful in live hare rounds
}

func (mg *MalfeasanceGossip) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddObject("proof", &mg.MalfeasanceProof)
	if mg.Eligibility != nil {
		encoder.AddObject("hare eligibility", mg.Eligibility)
	}
	return nil
}

type AtxProof struct {
	Messages [2]AtxProofMsg
}

func (ap *AtxProof) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddObject("first", &ap.Messages[0].InnerMsg)
	encoder.AddObject("second", &ap.Messages[1].InnerMsg)
	return nil
}

type BallotProof struct {
	Messages [2]BallotProofMsg
}

func (bp *BallotProof) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddObject("first", &bp.Messages[0].InnerMsg)
	encoder.AddObject("second", &bp.Messages[1].InnerMsg)
	return nil
}

type HareProof struct {
	Messages [2]HareProofMsg
}

func (hp *HareProof) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddObject("first", &hp.Messages[0].InnerMsg)
	encoder.AddObject("second", &hp.Messages[1].InnerMsg)
	return nil
}

type AtxProofMsg struct {
	InnerMsg ATXMetadata

	SmesherID NodeID
	Signature EdSignature
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
	Signature EdSignature
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

func (hm *HareMetadata) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("layer", hm.Layer.Uint32())
	encoder.AddUint32("round", hm.Round)
	encoder.AddString("msgHash", hm.MsgHash.String())
	return nil
}

type HareProofMsg struct {
	InnerMsg  HareMetadata
	Signature EdSignature
}

// SignedBytes returns the actual data being signed in a HareProofMsg.
func (m *HareProofMsg) SignedBytes() []byte {
	data, err := codec.Encode(&m.InnerMsg)
	if err != nil {
		log.With().Fatal("failed to serialize MultiBlockProposalsMsg", log.Err(err))
	}
	return data
}
