package hare

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
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

func (builder *MessageBuilder) Sign(signing Signing) (*MessageBuilder, error) {
	buff, err := proto.Marshal(builder.inner)
	if err != nil {
		return nil, errors.New("error marshaling inner message")
	}

	builder.outer.InnerSig = signing.Sign(buff)

	return builder, nil
}

func (builder *MessageBuilder) SetType(msgType MessageType) *MessageBuilder {
	builder.inner.Type = int32(msgType)
	return builder
}

func (builder *MessageBuilder) SetLayer(id LayerId) *MessageBuilder {
	builder.inner.Layer = id.Bytes()
	return builder
}

func (builder *MessageBuilder) SetIteration(k uint32) *MessageBuilder {
	builder.inner.K = k
	return builder
}

func (builder *MessageBuilder) SetKi(ki uint32) *MessageBuilder {
	builder.inner.Ki = ki
	return builder
}

func (builder *MessageBuilder) SetBlocks(set Set) *MessageBuilder {
	builder.inner.Blocks = set.To2DSlice()
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


