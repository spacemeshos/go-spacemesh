package hare

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// top InnerMsg of the protocol
type Message struct {
	Sig      []byte
	InnerMsg *InnerMessage
}

// MessageFromBuffer builds an Hare message from the provided bytes buffer.
// It returns an error if unmarshal of the provided byte slice failed.
func MessageFromBuffer(buffer []byte) (*Message, error) {
	rdr := bytes.NewReader(buffer)
	hareMsg := &Message{}
	_, err := xdr.Unmarshal(rdr, hareMsg)
	if err != nil {
		log.Error("Could not unmarshal message: %v", err)
		return nil, err
	}

	return hareMsg, nil
}

func (m *Message) String() string {
	sig := hex.EncodeToString(m.Sig)
	l := len(sig)
	if l > 5 {
		l = 5
	}
	return fmt.Sprintf("Sig: %v… InnerMsg: %v", sig[:l], m.InnerMsg.String())
}

// the certificate
type Certificate struct {
	Values  []uint64 // the committed set S
	AggMsgs *AggregatedMessages
}

// Aggregated Messages
type AggregatedMessages struct {
	Messages []*Message // a collection of Messages
	AggSig   []byte
}

// basic InnerMsg
type InnerMessage struct {
	Type       MessageType
	InstanceId InstanceId
	K          int32 // the round counter
	Ki         int32
	Values     []uint64            // the set S. optional for commit InnerMsg in a certificate
	RoleProof  []byte              // role is implicit by InnerMsg type, this is the proof
	Svp        *AggregatedMessages // optional. only for proposal Messages
	Cert       *Certificate        // optional
}

func (im *InnerMessage) Bytes() []byte {
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, im)
	if err != nil {
		log.Panic("could not marshal InnerMsg before send")
	}

	return w.Bytes()
}

func (im *InnerMessage) String() string {
	return fmt.Sprintf("Type: %v InstanceId: %v K: %v Ki: %v", im.Type, im.InstanceId, im.K, im.Ki)
}

// MessageBuilder is the impl of the builder DP.
// It allows the user to set the different fields of the builder and eventually Build the message.
type MessageBuilder struct {
	msg   *Msg
	inner *InnerMessage
}

// NewMessageBuilder returns a new, empty message builder.
// One should not assume any values are pre-set.
func NewMessageBuilder() *MessageBuilder {
	m := &MessageBuilder{&Msg{&Message{}, nil}, &InnerMessage{}}
	m.msg.InnerMsg = m.inner

	return m
}

// Build returns the protocol message as type Msg.
func (builder *MessageBuilder) Build() *Msg {
	return builder.msg
}

func (builder *MessageBuilder) SetCertificate(certificate *Certificate) *MessageBuilder {
	builder.msg.InnerMsg.Cert = certificate
	return builder
}

// Sign calls the provided signer to calculate the signature and then set it accordingly.
func (builder *MessageBuilder) Sign(signing Signer) *MessageBuilder {
	builder.msg.Sig = signing.Sign(builder.inner.Bytes())

	return builder
}

func (builder *MessageBuilder) SetPubKey(pub *signing.PublicKey) *MessageBuilder {
	builder.msg.PubKey = pub
	return builder
}

func (builder *MessageBuilder) SetType(msgType MessageType) *MessageBuilder {
	builder.inner.Type = msgType
	return builder
}

func (builder *MessageBuilder) SetInstanceId(id InstanceId) *MessageBuilder {
	builder.inner.InstanceId = id
	return builder
}

func (builder *MessageBuilder) SetRoundCounter(k int32) *MessageBuilder {
	builder.inner.K = k
	return builder
}

func (builder *MessageBuilder) SetKi(ki int32) *MessageBuilder {
	builder.inner.Ki = ki
	return builder
}

func (builder *MessageBuilder) SetValues(set *Set) *MessageBuilder {
	builder.inner.Values = set.ToSlice()
	return builder
}

func (builder *MessageBuilder) SetRoleProof(sig Signature) *MessageBuilder {
	builder.inner.RoleProof = sig
	return builder
}

func (builder *MessageBuilder) SetSVP(svp *AggregatedMessages) *MessageBuilder {
	builder.inner.Svp = svp
	return builder
}
