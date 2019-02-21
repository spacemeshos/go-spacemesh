// Code generated by protoc-gen-go. DO NOT EDIT.
// source: hare/pb/hare.proto

package pb

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// top message of the protocol
type HareMessage struct {
	PubKey               []byte        `protobuf:"bytes,1,opt,name=pubKey,proto3" json:"pubKey,omitempty"`
	InnerSig             []byte        `protobuf:"bytes,2,opt,name=innerSig,proto3" json:"innerSig,omitempty"`
	Message              *InnerMessage `protobuf:"bytes,3,opt,name=message,proto3" json:"message,omitempty"`
	Cert                 *Certificate  `protobuf:"bytes,4,opt,name=cert,proto3" json:"cert,omitempty"`
	XXX_NoUnkeyedLiteral struct{}      `json:"-"`
	XXX_unrecognized     []byte        `json:"-"`
	XXX_sizecache        int32         `json:"-"`
}

func (m *HareMessage) Reset()         { *m = HareMessage{} }
func (m *HareMessage) String() string { return proto.CompactTextString(m) }
func (*HareMessage) ProtoMessage()    {}
func (*HareMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_hare_9f806e8b8973ae2d, []int{0}
}
func (m *HareMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_HareMessage.Unmarshal(m, b)
}
func (m *HareMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_HareMessage.Marshal(b, m, deterministic)
}
func (dst *HareMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_HareMessage.Merge(dst, src)
}
func (m *HareMessage) XXX_Size() int {
	return xxx_messageInfo_HareMessage.Size(m)
}
func (m *HareMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_HareMessage.DiscardUnknown(m)
}

var xxx_messageInfo_HareMessage proto.InternalMessageInfo

func (m *HareMessage) GetPubKey() []byte {
	if m != nil {
		return m.PubKey
	}
	return nil
}

func (m *HareMessage) GetInnerSig() []byte {
	if m != nil {
		return m.InnerSig
	}
	return nil
}

func (m *HareMessage) GetMessage() *InnerMessage {
	if m != nil {
		return m.Message
	}
	return nil
}

func (m *HareMessage) GetCert() *Certificate {
	if m != nil {
		return m.Cert
	}
	return nil
}

// the certificate
type Certificate struct {
	Values               [][]byte            `protobuf:"bytes,1,rep,name=values,proto3" json:"values,omitempty"`
	AggMsgs              *AggregatedMessages `protobuf:"bytes,2,opt,name=aggMsgs,proto3" json:"aggMsgs,omitempty"`
	XXX_NoUnkeyedLiteral struct{}            `json:"-"`
	XXX_unrecognized     []byte              `json:"-"`
	XXX_sizecache        int32               `json:"-"`
}

func (m *Certificate) Reset()         { *m = Certificate{} }
func (m *Certificate) String() string { return proto.CompactTextString(m) }
func (*Certificate) ProtoMessage()    {}
func (*Certificate) Descriptor() ([]byte, []int) {
	return fileDescriptor_hare_9f806e8b8973ae2d, []int{1}
}
func (m *Certificate) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Certificate.Unmarshal(m, b)
}
func (m *Certificate) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Certificate.Marshal(b, m, deterministic)
}
func (dst *Certificate) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Certificate.Merge(dst, src)
}
func (m *Certificate) XXX_Size() int {
	return xxx_messageInfo_Certificate.Size(m)
}
func (m *Certificate) XXX_DiscardUnknown() {
	xxx_messageInfo_Certificate.DiscardUnknown(m)
}

var xxx_messageInfo_Certificate proto.InternalMessageInfo

func (m *Certificate) GetValues() [][]byte {
	if m != nil {
		return m.Values
	}
	return nil
}

func (m *Certificate) GetAggMsgs() *AggregatedMessages {
	if m != nil {
		return m.AggMsgs
	}
	return nil
}

// Aggregated messages
type AggregatedMessages struct {
	Messages             []*HareMessage `protobuf:"bytes,1,rep,name=messages,proto3" json:"messages,omitempty"`
	AggSig               []byte         `protobuf:"bytes,2,opt,name=aggSig,proto3" json:"aggSig,omitempty"`
	XXX_NoUnkeyedLiteral struct{}       `json:"-"`
	XXX_unrecognized     []byte         `json:"-"`
	XXX_sizecache        int32          `json:"-"`
}

func (m *AggregatedMessages) Reset()         { *m = AggregatedMessages{} }
func (m *AggregatedMessages) String() string { return proto.CompactTextString(m) }
func (*AggregatedMessages) ProtoMessage()    {}
func (*AggregatedMessages) Descriptor() ([]byte, []int) {
	return fileDescriptor_hare_9f806e8b8973ae2d, []int{2}
}
func (m *AggregatedMessages) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_AggregatedMessages.Unmarshal(m, b)
}
func (m *AggregatedMessages) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_AggregatedMessages.Marshal(b, m, deterministic)
}
func (dst *AggregatedMessages) XXX_Merge(src proto.Message) {
	xxx_messageInfo_AggregatedMessages.Merge(dst, src)
}
func (m *AggregatedMessages) XXX_Size() int {
	return xxx_messageInfo_AggregatedMessages.Size(m)
}
func (m *AggregatedMessages) XXX_DiscardUnknown() {
	xxx_messageInfo_AggregatedMessages.DiscardUnknown(m)
}

var xxx_messageInfo_AggregatedMessages proto.InternalMessageInfo

func (m *AggregatedMessages) GetMessages() []*HareMessage {
	if m != nil {
		return m.Messages
	}
	return nil
}

func (m *AggregatedMessages) GetAggSig() []byte {
	if m != nil {
		return m.AggSig
	}
	return nil
}

// basic message
type InnerMessage struct {
	Type                 int32               `protobuf:"varint,1,opt,name=type,proto3" json:"type,omitempty"`
	InstanceId           []byte              `protobuf:"bytes,2,opt,name=instanceId,proto3" json:"instanceId,omitempty"`
	K                    int32               `protobuf:"varint,3,opt,name=k,proto3" json:"k,omitempty"`
	Ki                   int32               `protobuf:"varint,4,opt,name=ki,proto3" json:"ki,omitempty"`
	Values               [][]byte            `protobuf:"bytes,5,rep,name=values,proto3" json:"values,omitempty"`
	RoleProof            []byte              `protobuf:"bytes,6,opt,name=roleProof,proto3" json:"roleProof,omitempty"`
	Svp                  *AggregatedMessages `protobuf:"bytes,7,opt,name=svp,proto3" json:"svp,omitempty"`
	XXX_NoUnkeyedLiteral struct{}            `json:"-"`
	XXX_unrecognized     []byte              `json:"-"`
	XXX_sizecache        int32               `json:"-"`
}

func (m *InnerMessage) Reset()         { *m = InnerMessage{} }
func (m *InnerMessage) String() string { return proto.CompactTextString(m) }
func (*InnerMessage) ProtoMessage()    {}
func (*InnerMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_hare_9f806e8b8973ae2d, []int{3}
}
func (m *InnerMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_InnerMessage.Unmarshal(m, b)
}
func (m *InnerMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_InnerMessage.Marshal(b, m, deterministic)
}
func (dst *InnerMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_InnerMessage.Merge(dst, src)
}
func (m *InnerMessage) XXX_Size() int {
	return xxx_messageInfo_InnerMessage.Size(m)
}
func (m *InnerMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_InnerMessage.DiscardUnknown(m)
}

var xxx_messageInfo_InnerMessage proto.InternalMessageInfo

func (m *InnerMessage) GetType() int32 {
	if m != nil {
		return m.Type
	}
	return 0
}

func (m *InnerMessage) GetInstanceId() []byte {
	if m != nil {
		return m.InstanceId
	}
	return nil
}

func (m *InnerMessage) GetK() int32 {
	if m != nil {
		return m.K
	}
	return 0
}

func (m *InnerMessage) GetKi() int32 {
	if m != nil {
		return m.Ki
	}
	return 0
}

func (m *InnerMessage) GetValues() [][]byte {
	if m != nil {
		return m.Values
	}
	return nil
}

func (m *InnerMessage) GetRoleProof() []byte {
	if m != nil {
		return m.RoleProof
	}
	return nil
}

func (m *InnerMessage) GetSvp() *AggregatedMessages {
	if m != nil {
		return m.Svp
	}
	return nil
}

func init() {
	proto.RegisterType((*HareMessage)(nil), "pb.HareMessage")
	proto.RegisterType((*Certificate)(nil), "pb.Certificate")
	proto.RegisterType((*AggregatedMessages)(nil), "pb.AggregatedMessages")
	proto.RegisterType((*InnerMessage)(nil), "pb.InnerMessage")
}

func init() { proto.RegisterFile("hare/pb/hare.proto", fileDescriptor_hare_9f806e8b8973ae2d) }

var fileDescriptor_hare_9f806e8b8973ae2d = []byte{
	// 337 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x7c, 0x92, 0xc1, 0x4a, 0xeb, 0x40,
	0x14, 0x86, 0x99, 0x34, 0x69, 0x7b, 0x4f, 0xca, 0xbd, 0x97, 0xb3, 0x28, 0x41, 0x44, 0x4a, 0xdc,
	0x14, 0x85, 0x56, 0xea, 0x13, 0xa8, 0x1b, 0x8b, 0x14, 0x64, 0x5c, 0x88, 0xee, 0x26, 0xed, 0xe9,
	0x18, 0x52, 0x93, 0x61, 0x26, 0x2d, 0xf4, 0x35, 0x7c, 0x22, 0x1f, 0x4d, 0x66, 0x3a, 0x6d, 0x23,
	0x82, 0xab, 0xe4, 0x9c, 0xff, 0xcf, 0x3f, 0x7f, 0x3e, 0x06, 0xf0, 0x4d, 0x68, 0x1a, 0xab, 0x6c,
	0x6c, 0x9f, 0x23, 0xa5, 0xab, 0xba, 0xc2, 0x40, 0x65, 0xe9, 0x07, 0x83, 0xf8, 0x5e, 0x68, 0x9a,
	0x91, 0x31, 0x42, 0x12, 0xf6, 0xa1, 0xad, 0xd6, 0xd9, 0x03, 0x6d, 0x13, 0x36, 0x60, 0xc3, 0x1e,
	0xf7, 0x13, 0x9e, 0x40, 0x37, 0x2f, 0x4b, 0xd2, 0x4f, 0xb9, 0x4c, 0x02, 0xa7, 0x1c, 0x66, 0xbc,
	0x80, 0xce, 0xfb, 0xee, 0xf3, 0xa4, 0x35, 0x60, 0xc3, 0x78, 0xf2, 0x7f, 0xa4, 0xb2, 0xd1, 0xd4,
	0xca, 0x3e, 0x96, 0xef, 0x0d, 0x78, 0x0e, 0xe1, 0x9c, 0x74, 0x9d, 0x84, 0xce, 0xf8, 0xcf, 0x1a,
	0xef, 0x48, 0xd7, 0xf9, 0x32, 0x9f, 0x8b, 0x9a, 0xb8, 0x13, 0xd3, 0x67, 0x88, 0x1b, 0x4b, 0xdb,
	0x69, 0x23, 0x56, 0x6b, 0x32, 0x09, 0x1b, 0xb4, 0x6c, 0xa7, 0xdd, 0x84, 0x57, 0xd0, 0x11, 0x52,
	0xce, 0x8c, 0x34, 0xae, 0x52, 0x3c, 0xe9, 0xdb, 0xb8, 0x1b, 0x29, 0x35, 0x49, 0x51, 0xd3, 0xc2,
	0x1f, 0x6e, 0xf8, 0xde, 0x96, 0xbe, 0x00, 0xfe, 0x94, 0xf1, 0x12, 0xba, 0xbe, 0xde, 0xee, 0x04,
	0xdf, 0xab, 0x81, 0x85, 0x1f, 0x0c, 0xb6, 0x8c, 0x90, 0xf2, 0x88, 0xc1, 0x4f, 0xe9, 0x27, 0x83,
	0x5e, 0xf3, 0x97, 0x11, 0x21, 0xac, 0xb7, 0x8a, 0x1c, 0xc7, 0x88, 0xbb, 0x77, 0x3c, 0x03, 0xc8,
	0x4b, 0x53, 0x8b, 0x72, 0x4e, 0xd3, 0x85, 0x0f, 0x68, 0x6c, 0xb0, 0x07, 0xac, 0x70, 0x0c, 0x23,
	0xce, 0x0a, 0xfc, 0x0b, 0x41, 0x91, 0x3b, 0x52, 0x11, 0x0f, 0x8a, 0xbc, 0xc1, 0x21, 0xfa, 0xc6,
	0xe1, 0x14, 0xfe, 0xe8, 0x6a, 0x45, 0x8f, 0xba, 0xaa, 0x96, 0x49, 0xdb, 0x85, 0x1e, 0x17, 0x38,
	0x84, 0x96, 0xd9, 0xa8, 0xa4, 0xf3, 0x2b, 0x21, 0x6b, 0xb9, 0x0d, 0x5f, 0x03, 0x95, 0x65, 0x6d,
	0x77, 0x39, 0xae, 0xbf, 0x02, 0x00, 0x00, 0xff, 0xff, 0x30, 0x35, 0x94, 0x40, 0x32, 0x02, 0x00,
	0x00,
}
