package types

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen -types MalfeasanceProof,MalfeasanceGossip,AtxProof,BallotProof,HareProof,AtxProofMsg,BallotProofMsg,HareProofMsg,HareMetadata

const (
	MultipleATXs byte = iota + 1
	MultipleBallots
	HareEquivocation
	InvalidPostIndex
)

type MalfeasanceProof struct {
	// for network upgrade
	Layer LayerID
	Proof Proof

	received time.Time
}

func (mp *MalfeasanceProof) Received() time.Time {
	return mp.received
}

func (mp *MalfeasanceProof) SetReceived(received time.Time) {
	mp.received = received
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
	case InvalidPostIndex:
		encoder.AddString("type", "invalid post index")
		// p, ok := mp.Proof.Data.(*wire.InvalidPostIndexProofV1)
		// if ok {
		// FIXME
		// p.Atx.Initialize()
		// encoder.AddString("atx_id", p.Atx.ID().String())
		// encoder.AddString("smesher", p.Atx.SmesherID.String())
		// encoder.AddUint32("invalid index", p.InvalidIdx)
		// }
	default:
		encoder.AddString("type", "unknown")
	}
	encoder.AddTime("received", mp.received)
	return nil
}

type Proof struct {
	// MultipleATXs | MultipleBallots | HareEquivocation | InvalidPostIndex
	Type uint8
	// AtxProof | BallotProof | HareProof | InvalidPostIndexProof
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
	case InvalidPostIndex:
		// var proof wire.InvalidPostIndexProofV1
		// n, err := proof.DecodeScale(dec)
		// if err != nil {
		// 	return total, err
		// }
		// e.Data = &proof
		// total += n
	default:
		return total, errors.New("unknown malfeasance proof type")
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

func (hp *HareProof) ToMalfeasanceProof() *MalfeasanceProof {
	return &MalfeasanceProof{
		Layer: hp.Messages[0].InnerMsg.Layer,
		Proof: Proof{
			Type: HareEquivocation,
			Data: hp,
		},
	}
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
	InnerMsg BallotMetadata

	SmesherID NodeID
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

// Equivocation detects if two messages form an equivocation, based on their HareMetadata.
// It returns true if the two messages are from the same layer and round, but have different hashes.
func (hm *HareMetadata) Equivocation(other *HareMetadata) bool {
	return hm.Layer == other.Layer && hm.Round == other.Round && hm.MsgHash != other.MsgHash
}

func (hm HareMetadata) ToBytes() []byte {
	buf, err := codec.Encode(&hm)
	if err != nil {
		panic(err.Error())
	}
	return buf
}

type HareProofMsg struct {
	InnerMsg HareMetadata

	SmesherID NodeID
	Signature EdSignature
}

// SignedBytes returns the actual data being signed in a HareProofMsg.
func (m *HareProofMsg) SignedBytes() []byte {
	return m.InnerMsg.ToBytes()
}

func MalfeasanceInfo(smesher NodeID, mp *MalfeasanceProof) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("generate layer: %v\n", mp.Layer))
	b.WriteString(fmt.Sprintf("smesher id: %s\n", smesher.String()))
	switch mp.Proof.Type {
	case MultipleATXs:
		p, ok := mp.Proof.Data.(*AtxProof)
		if ok {
			b.WriteString(
				fmt.Sprintf(
					"cause: smesher published multiple ATXs in epoch %d\n",
					p.Messages[0].InnerMsg.PublishEpoch,
				),
			)
			b.WriteString(
				fmt.Sprintf("1st message hash: %s\n", hex.EncodeToString(p.Messages[0].InnerMsg.MsgHash.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("1st message signature: %s\n", hex.EncodeToString(p.Messages[0].Signature.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("2nd message hash: %s\n", hex.EncodeToString(p.Messages[1].InnerMsg.MsgHash.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("2nd message signature: %s\n", hex.EncodeToString(p.Messages[1].Signature.Bytes())),
			)
		}
	case MultipleBallots:
		p, ok := mp.Proof.Data.(*BallotProof)
		if ok {
			b.WriteString(
				fmt.Sprintf("cause: smesher published multiple ballots in layer %d\n", p.Messages[0].InnerMsg.Layer),
			)
			b.WriteString(
				fmt.Sprintf("1st message hash: %s\n", hex.EncodeToString(p.Messages[0].InnerMsg.MsgHash.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("1st message signature: %s\n", hex.EncodeToString(p.Messages[0].Signature.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("2nd message hash: %s\n", hex.EncodeToString(p.Messages[1].InnerMsg.MsgHash.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("2nd message signature: %s\n", hex.EncodeToString(p.Messages[1].Signature.Bytes())),
			)
		}
	case HareEquivocation:
		p, ok := mp.Proof.Data.(*HareProof)
		if ok {
			b.WriteString(fmt.Sprintf("cause: smesher published multiple hare messages in layer %d round %d\n",
				p.Messages[0].InnerMsg.Layer, p.Messages[0].InnerMsg.Round))
			b.WriteString(
				fmt.Sprintf("1st message hash: %s\n", hex.EncodeToString(p.Messages[0].InnerMsg.MsgHash.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("1st message signature: %s\n", hex.EncodeToString(p.Messages[0].Signature.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("2nd message hash: %s\n", hex.EncodeToString(p.Messages[1].InnerMsg.MsgHash.Bytes())),
			)
			b.WriteString(
				fmt.Sprintf("2nd message signature: %s\n", hex.EncodeToString(p.Messages[1].Signature.Bytes())),
			)
		}
	case InvalidPostIndex:
		// FIXME
		// p, ok := mp.Proof.Data.(*wire.InvalidPostIndexProofV1)
		// if ok {

		// p.Atx.Initialize()
		// b.WriteString(
		// 	fmt.Sprintf(
		// 		"cause: smesher published ATX %s with invalid post index %d in epoch %d\n",
		// 		p.Atx.ID().ShortString(),
		// 		p.InvalidIdx,
		// 		p.Atx.PublishEpoch,
		// 	))
		// }
	}
	return b.String()
}
