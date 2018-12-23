package hare

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type MessageBuilder struct {
	outer *pb.HareMessage
	inner *pb.InnerMessage
}

func NewMessageBuilder() *MessageBuilder {
	m := &MessageBuilder{&pb.HareMessage{}, &pb.InnerMessage{}}
	m.outer.Message = m.inner

	return m
}

func (builder *MessageBuilder) Build() *pb.HareMessage {
	return builder.outer
}

func (builder *MessageBuilder) SetPubKey(key crypto.PublicKey) *MessageBuilder {
	builder.outer.PubKey = key.Bytes()
	return builder
}

func (builder *MessageBuilder) SetCertificate(certificate *pb.Certificate) *MessageBuilder {
	builder.outer.Cert = certificate
	return builder
}

func (builder *MessageBuilder) Sign(signing Signing) *MessageBuilder {
	buff, err := proto.Marshal(builder.inner)
	if err != nil {
		log.Error("could not sign message")
		panic("marshal failed during signing")
	}

	builder.outer.InnerSig = signing.Sign(buff)

	return builder
}

func (builder *MessageBuilder) SetType(msgType MessageType) *MessageBuilder {
	builder.inner.Type = int32(msgType)
	return builder
}

func (builder *MessageBuilder) SetSetId(id SetId) *MessageBuilder {
	builder.inner.SetId = id.Bytes()
	return builder
}

func (builder *MessageBuilder) SetIteration(k uint32) *MessageBuilder {
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

func (builder *MessageBuilder) SetSVP(svp *pb.AggregatedMessages) *MessageBuilder {
	builder.inner.Svp = svp
	return builder
}

type AggregatedBuilder struct {
	m *pb.AggregatedMessages
}
