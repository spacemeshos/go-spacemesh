package types

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen -types MalfeasanceGossip,MultiATXsMsg,MultiBallotsMsg,HareEquivocationMsg,HareMetadata

type MalfeasanceType uint16

const (
	MultipleATXs MalfeasanceType = iota + 1
	MultipleBallots
	HareEquivocation
)

type MalfeasanceProof struct {
	// for network upgrade
	Layer LayerID
	Type  MalfeasanceType
	// conflicting messages signed by the same NodeID
	Messages []any
}

func (t *MalfeasanceProof) EncodeScale(enc *scale.Encoder) (int, error) {
	var total int
	{
		n, err := t.Layer.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact16(enc, uint16(t.Type))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		switch t.Type {
		case MultipleATXs:
			var msgs []MultiATXsMsg
			for _, data := range t.Messages {
				msg, ok := data.(MultiATXsMsg)
				if !ok {
					log.Fatal("data not of type MultipleATXs")
				}
				msgs = append(msgs, msg)
			}
			n, err := scale.EncodeStructSlice(enc, msgs)
			if err != nil {
				return total, err
			}
			total += n
		case MultipleBallots:
			var msgs []MultiBallotsMsg
			for _, data := range t.Messages {
				msg, ok := data.(MultiBallotsMsg)
				if !ok {
					log.Fatal("data not of type BallotMetadata")
				}
				msgs = append(msgs, msg)
			}
			n, err := scale.EncodeStructSlice(enc, msgs)
			if err != nil {
				return total, err
			}
			total += n
		case HareEquivocation:
			var msgs []HareEquivocationMsg
			for _, data := range t.Messages {
				msg, ok := data.(HareEquivocationMsg)
				if !ok {
					log.Fatal("data not of type HareMetadata")
				}
				msgs = append(msgs, msg)
			}
			n, err := scale.EncodeStructSlice(enc, msgs)
			if err != nil {
				return total, err
			}
			total += n
		default:
			log.With().Fatal("unknown malfeasance type", log.Uint16("malfeasance_type", uint16(t.Type)))
		}
	}
	return total, nil
}

func (t *MalfeasanceProof) DecodeScale(dec *scale.Decoder) (int, error) {
	var total int
	{
		n, err := t.Layer.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeCompact16(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Type = MalfeasanceType(field)
	}
	{
		switch t.Type {
		case MultipleATXs:
			field, n, err := scale.DecodeStructSlice[MultiATXsMsg](dec)
			if err != nil {
				return total, err
			}
			total += n
			for _, f := range field {
				t.Messages = append(t.Messages, f)
			}
		case MultipleBallots:
			field, n, err := scale.DecodeStructSlice[MultiBallotsMsg](dec)
			if err != nil {
				return total, err
			}
			total += n
			for _, f := range field {
				t.Messages = append(t.Messages, f)
			}
		case HareEquivocation:
			field, n, err := scale.DecodeStructSlice[HareEquivocationMsg](dec)
			if err != nil {
				return total, err
			}
			total += n
			for _, f := range field {
				t.Messages = append(t.Messages, f)
			}
		default:
			log.With().Fatal("unknown malfeasance type", log.Uint16("malfeasance_type", uint16(t.Type)))
		}
	}
	return total, nil
}

type MalfeasanceGossip struct {
	Proof       MalfeasanceProof
	Eligibility *HareEligibility // optional, only useful in live hare rounds
}

type MultiATXsMsg struct {
	InnerMsg  ATXMetadata
	Signature []byte
}

// SignedBytes returns the actual data being signed in a MultiATXsMsg.
func (m *MultiATXsMsg) SignedBytes() []byte {
	data, err := codec.Encode(&m.InnerMsg)
	if err != nil {
		log.With().Fatal("failed to serialize MultiATXsMsg", log.Err(err))
	}
	return data
}

type MultiBallotsMsg struct {
	InnerMsg  BallotMetadata
	Signature []byte
}

// SignedBytes returns the actual data being signed in a MultiBallotsMsg.
func (m *MultiBallotsMsg) SignedBytes() []byte {
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

type HareEquivocationMsg struct {
	InnerMsg  HareMetadata
	Signature []byte
}

// SignedBytes returns the actual data being signed in a HareEquivocationMsg.
func (m *HareEquivocationMsg) SignedBytes() []byte {
	data, err := codec.Encode(&m.InnerMsg)
	if err != nil {
		log.With().Fatal("failed to serialize MultiBlockProposalsMsg", log.Err(err))
	}
	return data
}
