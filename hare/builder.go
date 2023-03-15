package hare

import (
	"encoding/hex"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate scalegen -types Message,Certificate,AggregatedMessages,InnerMessage

type Message struct {
	types.HareMetadata
	// signature over Metadata
	Signature   []byte `scale:"max=32"`
	InnerMsg    *InnerMessage
	Eligibility types.HareEligibility
}

// MessageFromBuffer builds an Hare message from the provided bytes buffer.
// It returns an error if unmarshal of the provided byte slice failed.
func MessageFromBuffer(buf []byte) (Message, error) {
	msg := Message{}
	if err := codec.Decode(buf, &msg); err != nil {
		return msg, fmt.Errorf("serialize: %w", err)
	}
	if msg.MsgHash != types.BytesToHash(msg.InnerMsg.HashBytes()) {
		return Message{}, fmt.Errorf("bad message hash")
	}
	return msg, nil
}

func (m *Message) MarshalLogObject(encoder log.ObjectEncoder) error {
	_ = encoder.AddObject("inner_msg", m.InnerMsg)
	encoder.AddUint32("layer_id", m.Layer.Value)
	encoder.AddUint32("round", m.Round)
	encoder.AddString("signature", hex.EncodeToString(m.Signature))
	_ = encoder.AddObject("eligibility", &m.Eligibility)
	return nil
}

// Field returns a log field. Implements the LoggableField interface.
func (m *Message) Field() log.Field {
	return log.Object("hare_msg", m)
}

func (m *Message) SetMetadata() {
	if m.Layer == (types.LayerID{}) {
		log.Fatal("Message is missing layer")
	}
	m.MsgHash = types.BytesToHash(m.InnerMsg.HashBytes())
}

// SignedBytes returns the signed data for hare message.
func (m *Message) SignedBytes() []byte {
	m.SetMetadata()
	buf, err := codec.Encode(&m.HareMetadata)
	if err != nil {
		log.With().Fatal("failed to encode HareMetadata", log.Err(err))
	}
	return buf
}

// Certificate is a collection of messages and the set of values.
// Typically used as a collection of commit messages.
type Certificate struct {
	Values  []types.ProposalID `scale:"max=800"` // the committed set S
	AggMsgs *AggregatedMessages
}

// AggregatedMessages is a collection of messages.
type AggregatedMessages struct {
	Messages []Message `scale:"max=800"`
}

// InnerMessage is the actual set of fields that describe a message in the Hare protocol.
type InnerMessage struct {
	Type           MessageType
	CommittedRound uint32              // the round Values (S) is committed (Ki)
	Values         []types.ProposalID  `scale:"max=800"` // the set S. optional for commit InnerMsg in a certificate
	Svp            *AggregatedMessages // optional. only for proposal Messages
	Cert           *Certificate        // optional
}

// HashBytes returns the message as bytes.
func (im *InnerMessage) HashBytes() []byte {
	hshr := hash.New()
	_, err := codec.EncodeTo(hshr, im)
	if err != nil {
		log.Fatal("failed to encode InnerMsg for hashing")
	}
	return hshr.Sum(nil)
}

func (im *InnerMessage) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("msg_type", im.Type.String())
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
func (mb *messageBuilder) Sign(signer *signing.EdSigner) *messageBuilder {
	mb.msg.Signature = signer.Sign(signing.HARE, mb.msg.SignedBytes())
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
	mb.msg.Layer = id
	return mb
}

// SetRoundCounter sets the round counter.
func (mb *messageBuilder) SetRoundCounter(round uint32) *messageBuilder {
	mb.msg.Round = round
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
	mb.msg.Eligibility.Proof = sig
	return mb
}

// SetEligibilityCount sets eligibility count.
func (mb *messageBuilder) SetEligibilityCount(eligibilityCount uint16) *messageBuilder {
	mb.msg.Eligibility.Count = eligibilityCount
	return mb
}

// SetSVP sets svp.
func (mb *messageBuilder) SetSVP(svp *AggregatedMessages) *messageBuilder {
	mb.inner.Svp = svp
	return mb
}
