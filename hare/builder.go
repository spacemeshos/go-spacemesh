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

// SignedBytes returns the signed data for hare message.
func (m *Message) SignedBytes() []byte {
	return m.InnerMsg.Bytes()
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
func (mb *messageBuilder) Build() *Msg {
	return mb.msg
}

// SetCertificate sets certificate.
func (mb *messageBuilder) SetCertificate(certificate *Certificate) *messageBuilder {
	mb.inner.Cert = certificate
	return mb
}

// Sign calls the provided signer to calculate the signature and then set it accordingly.
func (mb *messageBuilder) Sign(signing Signer) *messageBuilder {
	mb.msg.Signature = signing.Sign(mb.msg.SignedBytes())

	return mb
}

// SetPubKey sets the public key of the message.
// Note: the message itself does not contain the public key. The builder returns the wrapper of the message which does.
func (mb *messageBuilder) SetPubKey(pub *signing.PublicKey) *messageBuilder {
	mb.msg.PubKey = pub
	return mb
}

// SetType sets message type.
func (mb *messageBuilder) SetType(msgType MessageType) *messageBuilder {
	mb.inner.Type = msgType
	return mb
}

// SetLayer sets the layer.
func (mb *messageBuilder) SetLayer(id types.LayerID) *messageBuilder {
	mb.inner.Layer = id
	return mb
}

// SetRoundCounter sets the round counter.
func (mb *messageBuilder) SetRoundCounter(round uint32) *messageBuilder {
	mb.inner.Round = round
	return mb
}

// SetCommittedRound sets the committed round of the set values.
func (mb *messageBuilder) SetCommittedRound(round uint32) *messageBuilder {
	mb.inner.CommittedRound = round
	return mb
}

// SetValues sets values.
func (mb *messageBuilder) SetValues(set *Set) *messageBuilder {
	mb.inner.Values = set.ToSlice()
	return mb
}

// SetRoleProof sets role proof.
func (mb *messageBuilder) SetRoleProof(sig []byte) *messageBuilder {
	mb.inner.RoleProof = sig
	return mb
}

// SetEligibilityCount sets eligibility count.
func (mb *messageBuilder) SetEligibilityCount(eligibilityCount uint16) *messageBuilder {
	mb.inner.EligibilityCount = eligibilityCount
	return mb
}

// SetSVP sets svp.
func (mb *messageBuilder) SetSVP(svp *AggregatedMessages) *messageBuilder {
	mb.inner.Svp = svp
	return mb
}
