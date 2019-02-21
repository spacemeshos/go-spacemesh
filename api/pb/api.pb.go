// Code generated by protoc-gen-go. DO NOT EDIT.
// source: api/pb/api.proto

package pb

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import _ "google.golang.org/genproto/googleapis/api/annotations"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type SimpleMessage struct {
	Value                string   `protobuf:"bytes,1,opt,name=value,proto3" json:"value,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SimpleMessage) Reset()         { *m = SimpleMessage{} }
func (m *SimpleMessage) String() string { return proto.CompactTextString(m) }
func (*SimpleMessage) ProtoMessage()    {}
func (*SimpleMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_e55214edd576488b, []int{0}
}
func (m *SimpleMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_SimpleMessage.Unmarshal(m, b)
}
func (m *SimpleMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_SimpleMessage.Marshal(b, m, deterministic)
}
func (dst *SimpleMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_SimpleMessage.Merge(dst, src)
}
func (m *SimpleMessage) XXX_Size() int {
	return xxx_messageInfo_SimpleMessage.Size(m)
}
func (m *SimpleMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_SimpleMessage.DiscardUnknown(m)
}

var xxx_messageInfo_SimpleMessage proto.InternalMessageInfo

func (m *SimpleMessage) GetValue() string {
	if m != nil {
		return m.Value
	}
	return ""
}

type AccountId struct {
	Address              string   `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *AccountId) Reset()         { *m = AccountId{} }
func (m *AccountId) String() string { return proto.CompactTextString(m) }
func (*AccountId) ProtoMessage()    {}
func (*AccountId) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_e55214edd576488b, []int{1}
}
func (m *AccountId) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_AccountId.Unmarshal(m, b)
}
func (m *AccountId) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_AccountId.Marshal(b, m, deterministic)
}
func (dst *AccountId) XXX_Merge(src proto.Message) {
	xxx_messageInfo_AccountId.Merge(dst, src)
}
func (m *AccountId) XXX_Size() int {
	return xxx_messageInfo_AccountId.Size(m)
}
func (m *AccountId) XXX_DiscardUnknown() {
	xxx_messageInfo_AccountId.DiscardUnknown(m)
}

var xxx_messageInfo_AccountId proto.InternalMessageInfo

func (m *AccountId) GetAddress() string {
	if m != nil {
		return m.Address
	}
	return ""
}

type TransferFunds struct {
	Sender               *AccountId `protobuf:"bytes,1,opt,name=sender,proto3" json:"sender,omitempty"`
	Receiver             *AccountId `protobuf:"bytes,2,opt,name=receiver,proto3" json:"receiver,omitempty"`
	Nonce                uint64     `protobuf:"varint,3,opt,name=Nonce,proto3" json:"Nonce,omitempty"`
	Amount               uint64     `protobuf:"varint,4,opt,name=Amount,proto3" json:"Amount,omitempty"`
	XXX_NoUnkeyedLiteral struct{}   `json:"-"`
	XXX_unrecognized     []byte     `json:"-"`
	XXX_sizecache        int32      `json:"-"`
}

func (m *TransferFunds) Reset()         { *m = TransferFunds{} }
func (m *TransferFunds) String() string { return proto.CompactTextString(m) }
func (*TransferFunds) ProtoMessage()    {}
func (*TransferFunds) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_e55214edd576488b, []int{2}
}
func (m *TransferFunds) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_TransferFunds.Unmarshal(m, b)
}
func (m *TransferFunds) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_TransferFunds.Marshal(b, m, deterministic)
}
func (dst *TransferFunds) XXX_Merge(src proto.Message) {
	xxx_messageInfo_TransferFunds.Merge(dst, src)
}
func (m *TransferFunds) XXX_Size() int {
	return xxx_messageInfo_TransferFunds.Size(m)
}
func (m *TransferFunds) XXX_DiscardUnknown() {
	xxx_messageInfo_TransferFunds.DiscardUnknown(m)
}

var xxx_messageInfo_TransferFunds proto.InternalMessageInfo

func (m *TransferFunds) GetSender() *AccountId {
	if m != nil {
		return m.Sender
	}
	return nil
}

func (m *TransferFunds) GetReceiver() *AccountId {
	if m != nil {
		return m.Receiver
	}
	return nil
}

func (m *TransferFunds) GetNonce() uint64 {
	if m != nil {
		return m.Nonce
	}
	return 0
}

func (m *TransferFunds) GetAmount() uint64 {
	if m != nil {
		return m.Amount
	}
	return 0
}

type SignedTransaction struct {
	SrcAddress           string   `protobuf:"bytes,1,opt,name=srcAddress,proto3" json:"srcAddress,omitempty"`
	DstAddress           string   `protobuf:"bytes,2,opt,name=dstAddress,proto3" json:"dstAddress,omitempty"`
	Amount               string   `protobuf:"bytes,3,opt,name=amount,proto3" json:"amount,omitempty"`
	Nonce                string   `protobuf:"bytes,4,opt,name=nonce,proto3" json:"nonce,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SignedTransaction) Reset()         { *m = SignedTransaction{} }
func (m *SignedTransaction) String() string { return proto.CompactTextString(m) }
func (*SignedTransaction) ProtoMessage()    {}
func (*SignedTransaction) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_e55214edd576488b, []int{3}
}
func (m *SignedTransaction) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_SignedTransaction.Unmarshal(m, b)
}
func (m *SignedTransaction) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_SignedTransaction.Marshal(b, m, deterministic)
}
func (dst *SignedTransaction) XXX_Merge(src proto.Message) {
	xxx_messageInfo_SignedTransaction.Merge(dst, src)
}
func (m *SignedTransaction) XXX_Size() int {
	return xxx_messageInfo_SignedTransaction.Size(m)
}
func (m *SignedTransaction) XXX_DiscardUnknown() {
	xxx_messageInfo_SignedTransaction.DiscardUnknown(m)
}

var xxx_messageInfo_SignedTransaction proto.InternalMessageInfo

func (m *SignedTransaction) GetSrcAddress() string {
	if m != nil {
		return m.SrcAddress
	}
	return ""
}

func (m *SignedTransaction) GetDstAddress() string {
	if m != nil {
		return m.DstAddress
	}
	return ""
}

func (m *SignedTransaction) GetAmount() string {
	if m != nil {
		return m.Amount
	}
	return ""
}

func (m *SignedTransaction) GetNonce() string {
	if m != nil {
		return m.Nonce
	}
	return ""
}

type BroadcastMessage struct {
	Data                 string   `protobuf:"bytes,1,opt,name=Data,proto3" json:"Data,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *BroadcastMessage) Reset()         { *m = BroadcastMessage{} }
func (m *BroadcastMessage) String() string { return proto.CompactTextString(m) }
func (*BroadcastMessage) ProtoMessage()    {}
func (*BroadcastMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_e55214edd576488b, []int{4}
}
func (m *BroadcastMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_BroadcastMessage.Unmarshal(m, b)
}
func (m *BroadcastMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_BroadcastMessage.Marshal(b, m, deterministic)
}
func (dst *BroadcastMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_BroadcastMessage.Merge(dst, src)
}
func (m *BroadcastMessage) XXX_Size() int {
	return xxx_messageInfo_BroadcastMessage.Size(m)
}
func (m *BroadcastMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_BroadcastMessage.DiscardUnknown(m)
}

var xxx_messageInfo_BroadcastMessage proto.InternalMessageInfo

func (m *BroadcastMessage) GetData() string {
	if m != nil {
		return m.Data
	}
	return ""
}

func init() {
	proto.RegisterType((*SimpleMessage)(nil), "pb.SimpleMessage")
	proto.RegisterType((*AccountId)(nil), "pb.AccountId")
	proto.RegisterType((*TransferFunds)(nil), "pb.TransferFunds")
	proto.RegisterType((*SignedTransaction)(nil), "pb.SignedTransaction")
	proto.RegisterType((*BroadcastMessage)(nil), "pb.BroadcastMessage")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// SpacemeshServiceClient is the client API for SpacemeshService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type SpacemeshServiceClient interface {
	Echo(ctx context.Context, in *SimpleMessage, opts ...grpc.CallOption) (*SimpleMessage, error)
	GetNonce(ctx context.Context, in *AccountId, opts ...grpc.CallOption) (*SimpleMessage, error)
	GetBalance(ctx context.Context, in *AccountId, opts ...grpc.CallOption) (*SimpleMessage, error)
	SubmitTransaction(ctx context.Context, in *SignedTransaction, opts ...grpc.CallOption) (*SimpleMessage, error)
	Broadcast(ctx context.Context, in *BroadcastMessage, opts ...grpc.CallOption) (*SimpleMessage, error)
}

type spacemeshServiceClient struct {
	cc *grpc.ClientConn
}

func NewSpacemeshServiceClient(cc *grpc.ClientConn) SpacemeshServiceClient {
	return &spacemeshServiceClient{cc}
}

func (c *spacemeshServiceClient) Echo(ctx context.Context, in *SimpleMessage, opts ...grpc.CallOption) (*SimpleMessage, error) {
	out := new(SimpleMessage)
	err := c.cc.Invoke(ctx, "/pb.SpacemeshService/Echo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *spacemeshServiceClient) GetNonce(ctx context.Context, in *AccountId, opts ...grpc.CallOption) (*SimpleMessage, error) {
	out := new(SimpleMessage)
	err := c.cc.Invoke(ctx, "/pb.SpacemeshService/GetNonce", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *spacemeshServiceClient) GetBalance(ctx context.Context, in *AccountId, opts ...grpc.CallOption) (*SimpleMessage, error) {
	out := new(SimpleMessage)
	err := c.cc.Invoke(ctx, "/pb.SpacemeshService/GetBalance", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *spacemeshServiceClient) SubmitTransaction(ctx context.Context, in *SignedTransaction, opts ...grpc.CallOption) (*SimpleMessage, error) {
	out := new(SimpleMessage)
	err := c.cc.Invoke(ctx, "/pb.SpacemeshService/SubmitTransaction", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *spacemeshServiceClient) Broadcast(ctx context.Context, in *BroadcastMessage, opts ...grpc.CallOption) (*SimpleMessage, error) {
	out := new(SimpleMessage)
	err := c.cc.Invoke(ctx, "/pb.SpacemeshService/Broadcast", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SpacemeshServiceServer is the server API for SpacemeshService service.
type SpacemeshServiceServer interface {
	Echo(context.Context, *SimpleMessage) (*SimpleMessage, error)
	GetNonce(context.Context, *AccountId) (*SimpleMessage, error)
	GetBalance(context.Context, *AccountId) (*SimpleMessage, error)
	SubmitTransaction(context.Context, *SignedTransaction) (*SimpleMessage, error)
	Broadcast(context.Context, *BroadcastMessage) (*SimpleMessage, error)
}

func RegisterSpacemeshServiceServer(s *grpc.Server, srv SpacemeshServiceServer) {
	s.RegisterService(&_SpacemeshService_serviceDesc, srv)
}

func _SpacemeshService_Echo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SimpleMessage)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SpacemeshServiceServer).Echo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/pb.SpacemeshService/Echo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SpacemeshServiceServer).Echo(ctx, req.(*SimpleMessage))
	}
	return interceptor(ctx, in, info, handler)
}

func _SpacemeshService_GetNonce_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AccountId)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SpacemeshServiceServer).GetNonce(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/pb.SpacemeshService/GetNonce",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SpacemeshServiceServer).GetNonce(ctx, req.(*AccountId))
	}
	return interceptor(ctx, in, info, handler)
}

func _SpacemeshService_GetBalance_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AccountId)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SpacemeshServiceServer).GetBalance(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/pb.SpacemeshService/GetBalance",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SpacemeshServiceServer).GetBalance(ctx, req.(*AccountId))
	}
	return interceptor(ctx, in, info, handler)
}

func _SpacemeshService_SubmitTransaction_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SignedTransaction)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SpacemeshServiceServer).SubmitTransaction(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/pb.SpacemeshService/SubmitTransaction",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SpacemeshServiceServer).SubmitTransaction(ctx, req.(*SignedTransaction))
	}
	return interceptor(ctx, in, info, handler)
}

func _SpacemeshService_Broadcast_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BroadcastMessage)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SpacemeshServiceServer).Broadcast(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/pb.SpacemeshService/Broadcast",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SpacemeshServiceServer).Broadcast(ctx, req.(*BroadcastMessage))
	}
	return interceptor(ctx, in, info, handler)
}

var _SpacemeshService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "pb.SpacemeshService",
	HandlerType: (*SpacemeshServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Echo",
			Handler:    _SpacemeshService_Echo_Handler,
		},
		{
			MethodName: "GetNonce",
			Handler:    _SpacemeshService_GetNonce_Handler,
		},
		{
			MethodName: "GetBalance",
			Handler:    _SpacemeshService_GetBalance_Handler,
		},
		{
			MethodName: "SubmitTransaction",
			Handler:    _SpacemeshService_SubmitTransaction_Handler,
		},
		{
			MethodName: "Broadcast",
			Handler:    _SpacemeshService_Broadcast_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/pb/api.proto",
}

func init() { proto.RegisterFile("api/pb/api.proto", fileDescriptor_api_e55214edd576488b) }

var fileDescriptor_api_e55214edd576488b = []byte{
	// 461 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x8c, 0x93, 0xc1, 0x6e, 0xd3, 0x40,
	0x10, 0x86, 0xe5, 0x24, 0x84, 0x66, 0xaa, 0x20, 0x67, 0x95, 0x54, 0x56, 0x40, 0x28, 0x5a, 0x29,
	0xa8, 0xf4, 0x10, 0x8b, 0x72, 0xeb, 0x2d, 0x11, 0xb4, 0xea, 0x81, 0x1e, 0x62, 0xee, 0x68, 0xbc,
	0x1e, 0x12, 0x4b, 0xc9, 0xae, 0xb5, 0xbb, 0x89, 0xb8, 0xc2, 0x0b, 0x70, 0xe0, 0xc6, 0x6b, 0xf1,
	0x0a, 0x3c, 0x08, 0xda, 0x5d, 0x27, 0x6d, 0x4a, 0x0e, 0xbd, 0x79, 0xe6, 0x9f, 0xf9, 0x66, 0xf6,
	0x1f, 0x19, 0x62, 0xac, 0xca, 0xb4, 0xca, 0x53, 0xac, 0xca, 0x49, 0xa5, 0x95, 0x55, 0xac, 0x51,
	0xe5, 0xc3, 0x57, 0x0b, 0xa5, 0x16, 0x2b, 0x72, 0xd9, 0x14, 0xa5, 0x54, 0x16, 0x6d, 0xa9, 0xa4,
	0x09, 0x15, 0x7c, 0x0c, 0xdd, 0xac, 0x5c, 0x57, 0x2b, 0xfa, 0x44, 0xc6, 0xe0, 0x82, 0x58, 0x1f,
	0x9e, 0x6d, 0x71, 0xb5, 0xa1, 0x24, 0x1a, 0x45, 0xe7, 0x9d, 0x79, 0x08, 0xf8, 0x18, 0x3a, 0x53,
	0x21, 0xd4, 0x46, 0xda, 0xdb, 0x82, 0x25, 0xf0, 0x1c, 0x8b, 0x42, 0x93, 0x31, 0x75, 0xd1, 0x2e,
	0xe4, 0x3f, 0x23, 0xe8, 0x7e, 0xd6, 0x28, 0xcd, 0x57, 0xd2, 0xd7, 0x1b, 0x59, 0x18, 0x36, 0x86,
	0xb6, 0x21, 0x59, 0x90, 0xf6, 0xa5, 0xa7, 0x97, 0xdd, 0x49, 0x95, 0x4f, 0xf6, 0xa8, 0x79, 0x2d,
	0xb2, 0xb7, 0x70, 0xa2, 0x49, 0x50, 0xb9, 0x25, 0x9d, 0x34, 0x8e, 0x15, 0xee, 0x65, 0xb7, 0xe0,
	0x9d, 0x92, 0x82, 0x92, 0xe6, 0x28, 0x3a, 0x6f, 0xcd, 0x43, 0xc0, 0xce, 0xa0, 0x3d, 0x5d, 0xbb,
	0xda, 0xa4, 0xe5, 0xd3, 0x75, 0xc4, 0xbf, 0x47, 0xd0, 0xcb, 0xca, 0x85, 0xa4, 0xc2, 0xef, 0x85,
	0xc2, 0x3d, 0x9e, 0xbd, 0x06, 0x30, 0x5a, 0x4c, 0x0f, 0x1e, 0xf1, 0x20, 0xe3, 0xf4, 0xc2, 0xd8,
	0x9d, 0xde, 0x08, 0xfa, 0x7d, 0xc6, 0x4d, 0xc3, 0x30, 0xad, 0xe9, 0xb5, 0x3a, 0x72, 0xbb, 0x49,
	0xbf, 0x5b, 0x2b, 0x98, 0xe7, 0x03, 0xfe, 0x06, 0xe2, 0x99, 0x56, 0x58, 0x08, 0x34, 0x76, 0x67,
	0x33, 0x83, 0xd6, 0x07, 0xb4, 0x58, 0xcf, 0xf6, 0xdf, 0x97, 0xbf, 0x9b, 0x10, 0x67, 0x15, 0x0a,
	0x5a, 0x93, 0x59, 0x66, 0xa4, 0xb7, 0xa5, 0x20, 0x76, 0x0b, 0xad, 0x8f, 0x62, 0xa9, 0x58, 0xcf,
	0xf9, 0x71, 0x70, 0xaa, 0xe1, 0xff, 0x29, 0xfe, 0xf2, 0xc7, 0x9f, 0xbf, 0xbf, 0x1a, 0x03, 0x1e,
	0xa7, 0xdb, 0x77, 0x29, 0x7d, 0x43, 0xa7, 0xa5, 0x24, 0x96, 0xea, 0x2a, 0xba, 0x60, 0x33, 0x38,
	0xb9, 0x21, 0x1b, 0xfc, 0x3a, 0xb4, 0xf7, 0x18, 0xaa, 0xef, 0x51, 0x2f, 0x78, 0xc7, 0xa1, 0xfc,
	0x43, 0x1c, 0xe3, 0x1a, 0xe0, 0x86, 0xec, 0x0c, 0x57, 0xf8, 0x34, 0xca, 0x99, 0xa7, 0xc4, 0xfc,
	0xd4, 0x51, 0xf2, 0xd0, 0xe6, 0x38, 0x5f, 0xa0, 0x97, 0x6d, 0xf2, 0x75, 0x69, 0x1f, 0x9e, 0x65,
	0x10, 0xfa, 0x1f, 0x5d, 0xeb, 0x18, 0x76, 0xe4, 0xb1, 0x43, 0x3e, 0x70, 0x58, 0xe3, 0x41, 0xf6,
	0xbe, 0xc3, 0x0d, 0xb8, 0x83, 0xce, 0xde, 0x74, 0xd6, 0x77, 0x84, 0xc7, 0x37, 0x38, 0xc6, 0x4d,
	0x3c, 0x97, 0xf1, 0xae, 0x5f, 0x77, 0xd7, 0x70, 0x15, 0x5d, 0xe4, 0x6d, 0xff, 0xbf, 0xbc, 0xff,
	0x17, 0x00, 0x00, 0xff, 0xff, 0x0f, 0x4a, 0xf5, 0xb6, 0x65, 0x03, 0x00, 0x00,
}
