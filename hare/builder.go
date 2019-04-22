package hare

import (
	"bytes"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/log"
)

// top Message of the protocol
type XDRMessage struct {
	InnerSig []byte
	Message  *InnerMessage
}

// the certificate
type Certificate struct {
	Values  []uint64 // the committed set S
	AggMsgs *AggregatedMessages
}

// Aggregated Messages
type AggregatedMessages struct {
	Messages []*XDRMessage // a collection of Messages
	AggSig   []byte
}

// basic Message
type InnerMessage struct {
	Type       MessageType
	InstanceId InstanceId
	K          int32 // the round counter
	Ki         int32
	Values     []uint64            // the set S. optional for commit Message in a certificate
	RoleProof  []byte              // role is implicit by Message type, this is the proof
	Svp        *AggregatedMessages // optional. only for proposal Messages
	Cert       *Certificate        // optional
}

// Used to build proto Messages
type MessageBuilder struct {
	msg   *Msg
	inner *InnerMessage
}

func NewMessageBuilder() *MessageBuilder {
	m := &MessageBuilder{&Msg{&XDRMessage{}, nil}, &InnerMessage{}}
	m.msg.Message = m.inner

	return m
}

func (builder *MessageBuilder) Build() *Msg {
	return builder.msg
}

func (builder *MessageBuilder) SetCertificate(certificate *Certificate) *MessageBuilder {
	builder.msg.Message.Cert = certificate
	return builder
}

func (builder *MessageBuilder) Sign(signing Signer) *MessageBuilder {
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, builder.inner)
	if err != nil {
		log.Panic("marshal failed during signing")
	}

	// TODO: do we always sign the same Message? order of Values (set)?
	builder.msg.InnerSig = signing.Sign(w.Bytes())

	return builder
}

func (builder *MessageBuilder) SetPubKey(pub []byte) *MessageBuilder {
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
	builder.inner.Values = set.To2DSlice()
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
