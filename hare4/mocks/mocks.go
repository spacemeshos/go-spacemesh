// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go
//
// Generated by this command:
//
//	mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go
//

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	p2p "github.com/spacemeshos/go-spacemesh/p2p"
	server "github.com/spacemeshos/go-spacemesh/p2p/server"
	signing "github.com/spacemeshos/go-spacemesh/signing"
	gomock "go.uber.org/mock/gomock"
)

// MockstreamRequester is a mock of streamRequester interface.
type MockstreamRequester struct {
	ctrl     *gomock.Controller
	recorder *MockstreamRequesterMockRecorder
}

// MockstreamRequesterMockRecorder is the mock recorder for MockstreamRequester.
type MockstreamRequesterMockRecorder struct {
	mock *MockstreamRequester
}

// NewMockstreamRequester creates a new mock instance.
func NewMockstreamRequester(ctrl *gomock.Controller) *MockstreamRequester {
	mock := &MockstreamRequester{ctrl: ctrl}
	mock.recorder = &MockstreamRequesterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockstreamRequester) EXPECT() *MockstreamRequesterMockRecorder {
	return m.recorder
}

// StreamRequest mocks base method.
func (m *MockstreamRequester) StreamRequest(arg0 context.Context, arg1 p2p.Peer, arg2 []byte, arg3 server.StreamRequestCallback, arg4 ...string) error {
	m.ctrl.T.Helper()
	varargs := []any{arg0, arg1, arg2, arg3}
	for _, a := range arg4 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "StreamRequest", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// StreamRequest indicates an expected call of StreamRequest.
func (mr *MockstreamRequesterMockRecorder) StreamRequest(arg0, arg1, arg2, arg3 any, arg4 ...any) *MockstreamRequesterStreamRequestCall {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{arg0, arg1, arg2, arg3}, arg4...)
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StreamRequest", reflect.TypeOf((*MockstreamRequester)(nil).StreamRequest), varargs...)
	return &MockstreamRequesterStreamRequestCall{Call: call}
}

// MockstreamRequesterStreamRequestCall wrap *gomock.Call
type MockstreamRequesterStreamRequestCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockstreamRequesterStreamRequestCall) Return(arg0 error) *MockstreamRequesterStreamRequestCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockstreamRequesterStreamRequestCall) Do(f func(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error) *MockstreamRequesterStreamRequestCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockstreamRequesterStreamRequestCall) DoAndReturn(f func(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error) *MockstreamRequesterStreamRequestCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Mockverifier is a mock of verifier interface.
type Mockverifier struct {
	ctrl     *gomock.Controller
	recorder *MockverifierMockRecorder
}

// MockverifierMockRecorder is the mock recorder for Mockverifier.
type MockverifierMockRecorder struct {
	mock *Mockverifier
}

// NewMockverifier creates a new mock instance.
func NewMockverifier(ctrl *gomock.Controller) *Mockverifier {
	mock := &Mockverifier{ctrl: ctrl}
	mock.recorder = &MockverifierMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockverifier) EXPECT() *MockverifierMockRecorder {
	return m.recorder
}

// Verify mocks base method.
func (m *Mockverifier) Verify(arg0 signing.Domain, arg1 types.NodeID, arg2 []byte, arg3 types.EdSignature) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Verify", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *MockverifierMockRecorder) Verify(arg0, arg1, arg2, arg3 any) *MockverifierVerifyCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*Mockverifier)(nil).Verify), arg0, arg1, arg2, arg3)
	return &MockverifierVerifyCall{Call: call}
}

// MockverifierVerifyCall wrap *gomock.Call
type MockverifierVerifyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockverifierVerifyCall) Return(arg0 bool) *MockverifierVerifyCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockverifierVerifyCall) Do(f func(signing.Domain, types.NodeID, []byte, types.EdSignature) bool) *MockverifierVerifyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockverifierVerifyCall) DoAndReturn(f func(signing.Domain, types.NodeID, []byte, types.EdSignature) bool) *MockverifierVerifyCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}