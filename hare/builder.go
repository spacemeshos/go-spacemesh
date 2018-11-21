package hare

import "github.com/spacemeshos/go-spacemesh/hare/pb"

type OuterBuilder struct {
	m *pb.HareMessage
}

func NewOuterBuilder() *OuterBuilder {
	return &OuterBuilder{&pb.HareMessage{}}
}

func (proto *OuterBuilder) SetPubKey(key PubKey) *OuterBuilder {
	proto.m.PubKey = key.Bytes()
	return proto
}

func (proto *OuterBuilder) SetInnerMessage(message *pb.InnerMessage) *OuterBuilder {
	proto.m.Message = message
	return proto
}

func (proto *OuterBuilder) SetCertificate(certificate *pb.Certificate) *OuterBuilder {
	proto.m.Cert = certificate
	return proto
}

func (proto *OuterBuilder) SetInnerSignature(sig Signature) *OuterBuilder {
	proto.m.InnerSig = sig
	return proto
}

func (proto *OuterBuilder) Build() *pb.HareMessage {
	return proto.m
}

type InnerBuilder struct {
	m *pb.InnerMessage
}

func NewInnerBuilder() *InnerBuilder {
	return &InnerBuilder{&pb.InnerMessage{}}
}

func (inner *InnerBuilder) SetType(msgType MessageType) *InnerBuilder {
	inner.m.Type = int32(msgType)
	return inner
}

func (inner *InnerBuilder) SetLayer(id LayerId) *InnerBuilder {
	inner.m.Layer = id.Bytes()
	return inner
}

func (inner *InnerBuilder) SetIteration(k uint32) *InnerBuilder {
	inner.m.K = k
	return inner
}

func (inner *InnerBuilder) SetKi(ki uint32) *InnerBuilder {
	inner.m.Ki = ki
	return inner
}

func (inner *InnerBuilder) SetBlocks(set Set) *InnerBuilder {
	inner.m.Blocks = set.To2DSlice()
	return inner
}

func (inner *InnerBuilder) SetRoleProof(sig Signature) *InnerBuilder {
	inner.m.RoleProof = sig
	return inner
}

func (inner *InnerBuilder) SetSVP(svp *pb.AggregatedMessages) *InnerBuilder {
	inner.m.Svp = svp
	return inner
}

func (inner *InnerBuilder) Build() *pb.InnerMessage {
	return inner.m
}

type AggregatedBuilder struct {
	m *pb.AggregatedMessages
}


