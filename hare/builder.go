package hare

import "github.com/spacemeshos/go-spacemesh/hare/pb"

type ProtoMessageBuilder struct {
	m *pb.HareMessage
}

func NewProtoBuilder() *ProtoMessageBuilder {
	return &ProtoMessageBuilder{}
}

func (proto *ProtoMessageBuilder) SetPubKey(key PubKey) *ProtoMessageBuilder {
	proto.m.PubKey = key.Bytes()
	return proto
}

func (proto *ProtoMessageBuilder) SetInnerMessage(message *pb.InnerMessage) *ProtoMessageBuilder {
	proto.m.Message = message
	return proto
}

func (proto *ProtoMessageBuilder) SetCertificate(certificate *pb.Certificate) *ProtoMessageBuilder {
	proto.m.Cert = certificate
	return proto
}

func (proto *ProtoMessageBuilder) SetInnerSignature(sig Signature) *ProtoMessageBuilder {
	proto.m.InnerSig = sig
	return proto
}

func (proto *ProtoMessageBuilder) Build() *pb.HareMessage {
	return proto.m
}

type InnerMessageBuilder struct {
	m *pb.InnerMessage
}

func NewInnerBuilder() *InnerMessageBuilder {
	return &InnerMessageBuilder{}
}

func (inner *InnerMessageBuilder) SetType(msgType MessageType) *InnerMessageBuilder {
	inner.m.Type = int32(msgType)
	return inner
}

func (inner *InnerMessageBuilder) SetLayer(id LayerId) *InnerMessageBuilder {
	inner.m.Layer = id.Bytes()
	return inner
}

func (inner *InnerMessageBuilder) SetIteration(k uint32) *InnerMessageBuilder {
	inner.m.K = k
	return inner
}

func (inner *InnerMessageBuilder) SetKi(ki uint32) *InnerMessageBuilder {
	inner.m.Ki = ki
	return inner
}

func (inner *InnerMessageBuilder) SetBlocks(set Set) *InnerMessageBuilder {
	inner.m.Blocks = set.To2DSlice()
	return inner
}

func (inner *InnerMessageBuilder) SetRoleProof(sig Signature) *InnerMessageBuilder {
	inner.m.RoleProof = sig
	return inner
}

func (inner *InnerMessageBuilder) SetSVP(svp *pb.AggregatedMessages) *InnerMessageBuilder {
	inner.m.Svp = svp
	return inner
}

func (inner *InnerMessageBuilder) Build() *pb.InnerMessage {
	return inner.m
}

type AggregatedBuilder struct {
	m *pb.AggregatedMessages
}


