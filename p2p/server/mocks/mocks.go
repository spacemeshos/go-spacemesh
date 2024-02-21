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
	time "time"

	connmgr "github.com/libp2p/go-libp2p/core/connmgr"
	network "github.com/libp2p/go-libp2p/core/network"
	peer "github.com/libp2p/go-libp2p/core/peer"
	protocol "github.com/libp2p/go-libp2p/core/protocol"
	gomock "go.uber.org/mock/gomock"
)

// MockHost is a mock of Host interface.
type MockHost struct {
	ctrl     *gomock.Controller
	recorder *MockHostMockRecorder
}

// MockHostMockRecorder is the mock recorder for MockHost.
type MockHostMockRecorder struct {
	mock *MockHost
}

// NewMockHost creates a new mock instance.
func NewMockHost(ctrl *gomock.Controller) *MockHost {
	mock := &MockHost{ctrl: ctrl}
	mock.recorder = &MockHostMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockHost) EXPECT() *MockHostMockRecorder {
	return m.recorder
}

// ConnManager mocks base method.
func (m *MockHost) ConnManager() connmgr.ConnManager {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ConnManager")
	ret0, _ := ret[0].(connmgr.ConnManager)
	return ret0
}

// ConnManager indicates an expected call of ConnManager.
func (mr *MockHostMockRecorder) ConnManager() *MockHostConnManagerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ConnManager", reflect.TypeOf((*MockHost)(nil).ConnManager))
	return &MockHostConnManagerCall{Call: call}
}

// MockHostConnManagerCall wrap *gomock.Call
type MockHostConnManagerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockHostConnManagerCall) Return(arg0 connmgr.ConnManager) *MockHostConnManagerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockHostConnManagerCall) Do(f func() connmgr.ConnManager) *MockHostConnManagerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockHostConnManagerCall) DoAndReturn(f func() connmgr.ConnManager) *MockHostConnManagerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Network mocks base method.
func (m *MockHost) Network() network.Network {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Network")
	ret0, _ := ret[0].(network.Network)
	return ret0
}

// Network indicates an expected call of Network.
func (mr *MockHostMockRecorder) Network() *MockHostNetworkCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Network", reflect.TypeOf((*MockHost)(nil).Network))
	return &MockHostNetworkCall{Call: call}
}

// MockHostNetworkCall wrap *gomock.Call
type MockHostNetworkCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockHostNetworkCall) Return(arg0 network.Network) *MockHostNetworkCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockHostNetworkCall) Do(f func() network.Network) *MockHostNetworkCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockHostNetworkCall) DoAndReturn(f func() network.Network) *MockHostNetworkCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// NewStream mocks base method.
func (m *MockHost) NewStream(arg0 context.Context, arg1 peer.ID, arg2 ...protocol.ID) (network.Stream, error) {
	m.ctrl.T.Helper()
	varargs := []any{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "NewStream", varargs...)
	ret0, _ := ret[0].(network.Stream)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewStream indicates an expected call of NewStream.
func (mr *MockHostMockRecorder) NewStream(arg0, arg1 any, arg2 ...any) *MockHostNewStreamCall {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{arg0, arg1}, arg2...)
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewStream", reflect.TypeOf((*MockHost)(nil).NewStream), varargs...)
	return &MockHostNewStreamCall{Call: call}
}

// MockHostNewStreamCall wrap *gomock.Call
type MockHostNewStreamCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockHostNewStreamCall) Return(arg0 network.Stream, arg1 error) *MockHostNewStreamCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockHostNewStreamCall) Do(f func(context.Context, peer.ID, ...protocol.ID) (network.Stream, error)) *MockHostNewStreamCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockHostNewStreamCall) DoAndReturn(f func(context.Context, peer.ID, ...protocol.ID) (network.Stream, error)) *MockHostNewStreamCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// SetStreamHandler mocks base method.
func (m *MockHost) SetStreamHandler(arg0 protocol.ID, arg1 network.StreamHandler) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetStreamHandler", arg0, arg1)
}

// SetStreamHandler indicates an expected call of SetStreamHandler.
func (mr *MockHostMockRecorder) SetStreamHandler(arg0, arg1 any) *MockHostSetStreamHandlerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetStreamHandler", reflect.TypeOf((*MockHost)(nil).SetStreamHandler), arg0, arg1)
	return &MockHostSetStreamHandlerCall{Call: call}
}

// MockHostSetStreamHandlerCall wrap *gomock.Call
type MockHostSetStreamHandlerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockHostSetStreamHandlerCall) Return() *MockHostSetStreamHandlerCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockHostSetStreamHandlerCall) Do(f func(protocol.ID, network.StreamHandler)) *MockHostSetStreamHandlerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockHostSetStreamHandlerCall) DoAndReturn(f func(protocol.ID, network.StreamHandler)) *MockHostSetStreamHandlerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockpeerStream is a mock of peerStream interface.
type MockpeerStream struct {
	ctrl     *gomock.Controller
	recorder *MockpeerStreamMockRecorder
}

// MockpeerStreamMockRecorder is the mock recorder for MockpeerStream.
type MockpeerStreamMockRecorder struct {
	mock *MockpeerStream
}

// NewMockpeerStream creates a new mock instance.
func NewMockpeerStream(ctrl *gomock.Controller) *MockpeerStream {
	mock := &MockpeerStream{ctrl: ctrl}
	mock.recorder = &MockpeerStreamMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpeerStream) EXPECT() *MockpeerStreamMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockpeerStream) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockpeerStreamMockRecorder) Close() *MockpeerStreamCloseCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockpeerStream)(nil).Close))
	return &MockpeerStreamCloseCall{Call: call}
}

// MockpeerStreamCloseCall wrap *gomock.Call
type MockpeerStreamCloseCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpeerStreamCloseCall) Return(arg0 error) *MockpeerStreamCloseCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpeerStreamCloseCall) Do(f func() error) *MockpeerStreamCloseCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpeerStreamCloseCall) DoAndReturn(f func() error) *MockpeerStreamCloseCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Read mocks base method.
func (m *MockpeerStream) Read(p []byte) (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Read", p)
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Read indicates an expected call of Read.
func (mr *MockpeerStreamMockRecorder) Read(p any) *MockpeerStreamReadCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Read", reflect.TypeOf((*MockpeerStream)(nil).Read), p)
	return &MockpeerStreamReadCall{Call: call}
}

// MockpeerStreamReadCall wrap *gomock.Call
type MockpeerStreamReadCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpeerStreamReadCall) Return(n int, err error) *MockpeerStreamReadCall {
	c.Call = c.Call.Return(n, err)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpeerStreamReadCall) Do(f func([]byte) (int, error)) *MockpeerStreamReadCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpeerStreamReadCall) DoAndReturn(f func([]byte) (int, error)) *MockpeerStreamReadCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// SetDeadline mocks base method.
func (m *MockpeerStream) SetDeadline(arg0 time.Time) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetDeadline", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetDeadline indicates an expected call of SetDeadline.
func (mr *MockpeerStreamMockRecorder) SetDeadline(arg0 any) *MockpeerStreamSetDeadlineCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetDeadline", reflect.TypeOf((*MockpeerStream)(nil).SetDeadline), arg0)
	return &MockpeerStreamSetDeadlineCall{Call: call}
}

// MockpeerStreamSetDeadlineCall wrap *gomock.Call
type MockpeerStreamSetDeadlineCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpeerStreamSetDeadlineCall) Return(arg0 error) *MockpeerStreamSetDeadlineCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpeerStreamSetDeadlineCall) Do(f func(time.Time) error) *MockpeerStreamSetDeadlineCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpeerStreamSetDeadlineCall) DoAndReturn(f func(time.Time) error) *MockpeerStreamSetDeadlineCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Write mocks base method.
func (m *MockpeerStream) Write(p []byte) (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Write", p)
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Write indicates an expected call of Write.
func (mr *MockpeerStreamMockRecorder) Write(p any) *MockpeerStreamWriteCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Write", reflect.TypeOf((*MockpeerStream)(nil).Write), p)
	return &MockpeerStreamWriteCall{Call: call}
}

// MockpeerStreamWriteCall wrap *gomock.Call
type MockpeerStreamWriteCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpeerStreamWriteCall) Return(n int, err error) *MockpeerStreamWriteCall {
	c.Call = c.Call.Return(n, err)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpeerStreamWriteCall) Do(f func([]byte) (int, error)) *MockpeerStreamWriteCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpeerStreamWriteCall) DoAndReturn(f func([]byte) (int, error)) *MockpeerStreamWriteCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
