package hare

import (
	"encoding/hex"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen

// Message is the tuple of a message and its corresponding signature.
type Message struct {
	Sig      []byte
	InnerMsg *InnerMessage
}

// MessageFromBuffer builds an Hare message from the provided bytes buffer.
// It returns an error if unmarshal of the provided byte slice failed.
func MessageFromBuffer(buf []byte) (Message, error) {
	msg := Message{}
	if err := codec.Decode(buf, &msg); err != nil {
		return msg, fmt.Errorf("serialize: %w", err)
	}

	return msg, nil
}

func (m *Message) String() string {
	sig := hex.EncodeToString(m.Sig)
	l := len(sig)
	if l > 5 {
		l = 5
	}
	return fmt.Sprintf("Sig: %v… InnerMsg: %v", sig[:l], m.InnerMsg.String())
}

// Field returns a log field. Implements the LoggableField interface.
func (m *Message) Field() log.Field {
	return log.String("message", m.String())
}

// Certificate is a collection of messages and the set of values.
// Typically used as a collection of commit messages.
type Certificate struct {
	Values  []types.ProposalID // the committed set S
	AggMsgs *AggregatedMessages
}

// AggregatedMessages is a collection of messages.
type AggregatedMessages struct {
	Messages []Message
}

// InnerMessage is the actual set of fields that describe a message in the Hare protocol.
type InnerMessage struct {
	Type             MessageType
	InstanceID       types.LayerID
	K                uint32 // the round counter
	Ki               uint32
	Values           []types.ProposalID  // the set S. optional for commit InnerMsg in a certificate
	RoleProof        []byte              // role is implicit by InnerMsg type, this is the proof
	EligibilityCount uint16              // the number of claimed eligibilities
	Svp              *AggregatedMessages // optional. only for proposal Messages
	Cert             *Certificate        // optional
}

// Bytes returns the message as bytes.
func (im *InnerMessage) Bytes() []byte {
	buf, err := codec.Encode(im)
	if err != nil {
		log.Panic("could not marshal InnerMsg before send")
	}
	return buf
}

func (im *InnerMessage) String() string {
	if im == nil {
		return ""
	}
	return fmt.Sprintf("Type: %v InstanceID: %v K: %v Ki: %v", im.Type, im.InstanceID, im.K, im.Ki)
}

// MessageBuilder is the impl of the builder DP.
// It allows the user to set the different fields of the builder and eventually Build the message.
type MessageBuilder struct {
	msg   *Msg
	inner *InnerMessage
}

// newMessageBuilder returns a new, empty message builder.
// One should not assume any values are pre-set.
func newMessageBuilder() *MessageBuilder {
	m := &MessageBuilder{&Msg{Message: Message{}, PubKey: nil}, &InnerMessage{}}
	m.msg.InnerMsg = m.inner

	return m
}

// Build returns the protocol message as type Msg.
func (builder *MessageBuilder) Build() *Msg {
	return builder.msg
}

// SetCertificate sets certificate.
func (builder *MessageBuilder) SetCertificate(certificate *Certificate) *MessageBuilder {
	builder.inner.Cert = certificate
	return builder
}

// Sign calls the provided signer to calculate the signature and then set it accordingly.
func (builder *MessageBuilder) Sign(signing Signer) *MessageBuilder {
	builder.msg.Sig = signing.Sign(builder.inner.Bytes())

	return builder
}

// SetPubKey sets the public key of the message.
// Note: the message itself does not contain the public key. The builder returns the wrapper of the message which does.
func (builder *MessageBuilder) SetPubKey(pub *signing.PublicKey) *MessageBuilder {
	builder.msg.PubKey = pub
	return builder
}

// SetType sets message type.
func (builder *MessageBuilder) SetType(msgType MessageType) *MessageBuilder {
	builder.inner.Type = msgType
	return builder
}

// SetInstanceID sets instance ID.
func (builder *MessageBuilder) SetInstanceID(id types.LayerID) *MessageBuilder {
	builder.inner.InstanceID = id
	return builder
}

// SetRoundCounter sets round counter.
func (builder *MessageBuilder) SetRoundCounter(k uint32) *MessageBuilder {
	builder.inner.K = k
	return builder
}

// SetKi sets ki.
func (builder *MessageBuilder) SetKi(ki uint32) *MessageBuilder {
	builder.inner.Ki = ki
	return builder
}

// SetValues sets values.
func (builder *MessageBuilder) SetValues(set *Set) *MessageBuilder {
	builder.inner.Values = set.ToSlice()
	return builder
}

// SetRoleProof sets role proof.
func (builder *MessageBuilder) SetRoleProof(sig []byte) *MessageBuilder {
	builder.inner.RoleProof = sig
	return builder
}

// SetEligibilityCount sets eligibility count.
func (builder *MessageBuilder) SetEligibilityCount(eligibilityCount uint16) *MessageBuilder {
	builder.inner.EligibilityCount = eligibilityCount
	return builder
}

// SetSVP sets svp.
func (builder *MessageBuilder) SetSVP(svp *AggregatedMessages) *MessageBuilder {
	builder.inner.Svp = svp
	return builder
}
