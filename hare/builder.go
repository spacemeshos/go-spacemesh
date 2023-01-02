package hare

import (
	"encoding/hex"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen -types Message,Certificate,AggregatedMessages,InnerMessage

// Message is the tuple of a message and its corresponding signature.
type Message struct {
	Signature []byte
	InnerMsg  *InnerMessage
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

func (m *Message) MarshalLogObject(encoder log.ObjectEncoder) error {
	_ = encoder.AddObject("inner_msg", m.InnerMsg)
	encoder.AddString("signature", hex.EncodeToString(m.Signature))
	return nil
}

// Field returns a log field. Implements the LoggableField interface.
func (m *Message) Field() log.Field {
	return log.Object("hare_msg", m)
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
	Layer            types.LayerID
	Round            uint32              // the round counter (K)
	CommittedRound   uint32              // the round Values (S) is committed (Ki)
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
		log.With().Fatal("failed to encode InnerMessage", log.Err(err))
	}
	return buf
}

func (im *InnerMessage) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("msg_type", im.Type.String())
	encoder.AddUint32("layer_id", im.Layer.Value)
	encoder.AddUint32("round", im.Round)
	encoder.AddUint32("committed_round", im.CommittedRound)
	return nil
}

// messageBuilder is the impl of the builder DP.
// It allows the user to set the different fields of the builder and eventually Build the message.
type messageBuilder struct {
	msg   *Msg
	inner *InnerMessage
}

// newMessageBuilder returns a new, empty message builder.
// One should not assume any values are pre-set.
func newMessageBuilder() *messageBuilder {
	m := &messageBuilder{&Msg{Message: Message{}, PubKey: nil}, &InnerMessage{}}
	m.msg.InnerMsg = m.inner

	return m
}

// Build returns the protocol message as type Msg.
func (builder *messageBuilder) Build() *Msg {
	return builder.msg
}

// SetCertificate sets certificate.
func (builder *messageBuilder) SetCertificate(certificate *Certificate) *messageBuilder {
	builder.inner.Cert = certificate
	return builder
}

// Sign calls the provided signer to calculate the signature and then set it accordingly.
func (builder *messageBuilder) Sign(signing Signer) *messageBuilder {
	builder.msg.Signature = signing.Sign(builder.inner.Bytes())

	return builder
}

// SetPubKey sets the public key of the message.
// Note: the message itself does not contain the public key. The builder returns the wrapper of the message which does.
func (builder *messageBuilder) SetPubKey(pub *signing.PublicKey) *messageBuilder {
	builder.msg.PubKey = pub
	return builder
}

// SetType sets message type.
func (builder *messageBuilder) SetType(msgType MessageType) *messageBuilder {
	builder.inner.Type = msgType
	return builder
}

// SetLayer sets the layer.
func (builder *messageBuilder) SetLayer(id types.LayerID) *messageBuilder {
	builder.inner.Layer = id
	return builder
}

// SetRoundCounter sets the round counter.
func (builder *messageBuilder) SetRoundCounter(round uint32) *messageBuilder {
	builder.inner.Round = round
	return builder
}

// SetCommittedRound sets the committed round of the set values.
func (builder *messageBuilder) SetCommittedRound(round uint32) *messageBuilder {
	builder.inner.CommittedRound = round
	return builder
}

// SetValues sets values.
func (builder *messageBuilder) SetValues(set *Set) *messageBuilder {
	builder.inner.Values = set.ToSlice()
	return builder
}

// SetRoleProof sets role proof.
func (builder *messageBuilder) SetRoleProof(sig []byte) *messageBuilder {
	builder.inner.RoleProof = sig
	return builder
}

// SetEligibilityCount sets eligibility count.
func (builder *messageBuilder) SetEligibilityCount(eligibilityCount uint16) *messageBuilder {
	builder.inner.EligibilityCount = eligibilityCount
	return builder
}

// SetSVP sets svp.
func (builder *messageBuilder) SetSVP(svp *AggregatedMessages) *messageBuilder {
	builder.inner.Svp = svp
	return builder
}
