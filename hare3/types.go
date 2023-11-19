package hare3

import (
	"fmt"

	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Round uint8

var roundNames = [...]string{"preround", "hardlock", "softlock", "propose", "wait1", "wait2", "commit", "notify"}

func (r Round) String() string {
	return roundNames[r]
}

// NOTE(dshulyak) changes in order is a breaking change.
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

// Delay returns number of network delays since specified iterround.
func (ir IterRound) Delay(since IterRound) uint32 {
	if ir.Absolute() > since.Absolute() {
		delay := ir.Absolute() - since.Absolute()
		// we skip hardlock round in 0th iteration.
		if since.Iter == 0 && since.Round == preround && delay != 0 {
			delay--
		}
		return delay
	}
	return 0
}

func (ir IterRound) Grade(since IterRound) grade {
	return max(grade(6-ir.Delay(since)), grade0)
}

func (ir IterRound) IsMessageRound() bool {
	switch ir.Round {
	case preround:
		return true
	case propose:
		return true
	case commit:
		return true
	case notify:
		return true
	}
	return false
}

func (ir IterRound) Absolute() uint32 {
	return uint32(ir.Iter)*uint32(notify) + uint32(ir.Round)
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
	codec.MustEncodeTo(hash, &m.Body)
	var rst types.Hash32
	hash.Sum(rst[:0])
	return rst
}

func (m *Message) ToMetadata() types.HareMetadata {
	return types.HareMetadata{
		Layer:   m.Layer,
		Round:   m.Absolute(),
		MsgHash: m.ToHash(),
	}
}

func (m *Message) ToMalfeasanceProof() types.HareProofMsg {
	return types.HareProofMsg{
		InnerMsg:  m.ToMetadata(),
		SmesherID: m.Sender,
		Signature: m.Signature,
	}
}

func (m *Message) key() messageKey {
	return messageKey{
		Sender:    m.Sender,
		IterRound: m.IterRound,
	}
}

func (m *Message) ToBytes() []byte {
	return codec.MustEncode(m)
}

func (m *Message) Validate() error {
	if (m.Round == commit || m.Round == notify) && m.Value.Reference == nil {
		return fmt.Errorf("reference can't be nil in commit or notify rounds")
	} else if (m.Round == preround || m.Round == propose) && m.Value.Reference != nil {
		return fmt.Errorf("reference is set to not nil in round %s", m.Round)
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
