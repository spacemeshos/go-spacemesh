package hare3

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"go.uber.org/zap/zapcore"
)

type Round uint8

var roundNames = [...]string{"preround", "hardlock", "softlock", "propose", "wait1", "wait2", "commit", "notify"}

func (r Round) String() string {
	return roundNames[r]
}

const (
	preround Round = iota
	hardlock
	softlock
	propose
	wait1
	wait2
	commit
	notify
)

//go:generate scalegen

type IterRound struct {
	Iter  uint8
	Round Round
}

// Since returns number of network delays since specified iterround.
func (ir IterRound) Since(since IterRound) int {
	return int(ir.Single() - since.Single())
}

func (ir IterRound) Single() uint32 {
	return uint32(ir.Iter*uint8(notify) + uint8(ir.Round))
}

type Value struct {
	// Proposals is set in messages for preround and propose rounds.
	Proposals []types.ProposalID `scale:"max=200"`
	// Reference is set in messages for commit and notify rounds.
	Reference *types.Hash32
}

type Body struct {
	Layer types.LayerID
	IterRound
	Value       Value
	Eligibility types.HareEligibility
}

type Message struct {
	Body
	Sender    types.NodeID
	Signature types.EdSignature
}

func (m *Message) ToHash() types.Hash32 {
	hash := hash.New()
	_, err := codec.EncodeTo(hash, &m.Body)
	if err != nil {
		panic(err.Error())
	}
	var rst types.Hash32
	hash.Sum(rst[:0])
	return rst
}

func (m *Message) ToMetadata() types.HareMetadata {
	return types.HareMetadata{
		Layer:   m.Layer,
		Round:   m.Single(),
		MsgHash: m.ToHash(),
	}
}

func (m *Message) ToMalfeasenceProof() types.HareProofMsg {
	return types.HareProofMsg{
		InnerMsg:  m.ToMetadata(),
		SmesherID: m.Sender,
		Signature: m.Signature,
	}
}

func (m *Message) Key() messageKey {
	return messageKey{
		sender:    m.Sender,
		IterRound: m.IterRound,
	}
}

func (m *Message) ToBytes() []byte {
	buf, err := codec.Encode(m)
	if err != nil {
		panic(err.Error())
	}
	return buf
}

func (m *Message) Validate() error {
	if (m.Round == commit || m.Round == notify) && m.Value.Reference == nil {
		return fmt.Errorf("reference can't be nil in commit or notify rounds")
	}
	return nil
}

func (m *Message) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddUint32("lid", m.Layer.Uint32())
	encoder.AddUint8("iter", m.Iter)
	encoder.AddString("round", m.Round.String())
	encoder.AddString("sender", m.Sender.ShortString())
	if m.Value.Proposals != nil {
		encoder.AddArray("full", zapcore.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for _, id := range m.Value.Proposals {
				encoder.AppendString(types.Hash20(id).ShortString())
			}
			return nil
		}))
	} else if m.Value.Reference != nil {
		encoder.AddString("ref", m.Value.Reference.ShortString())
	}
	encoder.AddUint16("vrf_count", m.Eligibility.Count)
	return nil
}
