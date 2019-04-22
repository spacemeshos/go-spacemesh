package hare

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Used to build proto messages
type MessageBuilder struct {
	msg   *Msg
	inner *pb.InnerMessage
}

func NewMessageBuilder() *MessageBuilder {
	m := &MessageBuilder{&Msg{&pb.HareMessage{}, nil}, &pb.InnerMessage{}}
	m.msg.Message = m.inner

	return m
}

func (builder *MessageBuilder) Build() *Msg {
	return builder.msg
}

func (builder *MessageBuilder) SetCertificate(certificate *pb.Certificate) *MessageBuilder {
	builder.msg.Message.Cert = certificate
	return builder
}

func (builder *MessageBuilder) Sign(signing Signer) *MessageBuilder {
	buff, err := proto.Marshal(builder.inner)
	if err != nil {
		log.Panic("marshal failed during signing")
	}

	builder.msg.InnerSig = signing.Sign(buff)

	return builder
}

func (builder *MessageBuilder) SetPubKey(pub []byte) *MessageBuilder {
	builder.msg.PubKey = pub
	return builder
}

func (builder *MessageBuilder) SetType(msgType MessageType) *MessageBuilder {
	builder.inner.Type = int32(msgType)
	return builder
}

func (builder *MessageBuilder) SetInstanceId(id InstanceId) *MessageBuilder {
	builder.inner.InstanceId = uint32(id)
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

func (builder *MessageBuilder) SetSVP(svp *pb.AggregatedMessages) *MessageBuilder {
	builder.inner.Svp = svp
	return builder
}

type AggregatedBuilder struct {
	m *pb.AggregatedMessages
}
