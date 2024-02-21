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
	gomock "go.uber.org/mock/gomock"
)

// Mockrequester is a mock of requester interface.
type Mockrequester struct {
	ctrl     *gomock.Controller
	recorder *MockrequesterMockRecorder
}

// MockrequesterMockRecorder is the mock recorder for Mockrequester.
type MockrequesterMockRecorder struct {
	mock *Mockrequester
}

// NewMockrequester creates a new mock instance.
func NewMockrequester(ctrl *gomock.Controller) *Mockrequester {
	mock := &Mockrequester{ctrl: ctrl}
	mock.recorder = &MockrequesterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockrequester) EXPECT() *MockrequesterMockRecorder {
	return m.recorder
}

// Request mocks base method.
func (m *Mockrequester) Request(arg0 context.Context, arg1 p2p.Peer, arg2 []byte) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Request", arg0, arg1, arg2)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Request indicates an expected call of Request.
func (mr *MockrequesterMockRecorder) Request(arg0, arg1, arg2 any) *MockrequesterRequestCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Request", reflect.TypeOf((*Mockrequester)(nil).Request), arg0, arg1, arg2)
	return &MockrequesterRequestCall{Call: call}
}

// MockrequesterRequestCall wrap *gomock.Call
type MockrequesterRequestCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockrequesterRequestCall) Return(arg0 []byte, arg1 error) *MockrequesterRequestCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockrequesterRequestCall) Do(f func(context.Context, p2p.Peer, []byte) ([]byte, error)) *MockrequesterRequestCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockrequesterRequestCall) DoAndReturn(f func(context.Context, p2p.Peer, []byte) ([]byte, error)) *MockrequesterRequestCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Run mocks base method.
func (m *Mockrequester) Run(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Run", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Run indicates an expected call of Run.
func (mr *MockrequesterMockRecorder) Run(arg0 any) *MockrequesterRunCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*Mockrequester)(nil).Run), arg0)
	return &MockrequesterRunCall{Call: call}
}

// MockrequesterRunCall wrap *gomock.Call
type MockrequesterRunCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockrequesterRunCall) Return(arg0 error) *MockrequesterRunCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockrequesterRunCall) Do(f func(context.Context) error) *MockrequesterRunCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockrequesterRunCall) DoAndReturn(f func(context.Context) error) *MockrequesterRunCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockSyncValidator is a mock of SyncValidator interface.
type MockSyncValidator struct {
	ctrl     *gomock.Controller
	recorder *MockSyncValidatorMockRecorder
}

// MockSyncValidatorMockRecorder is the mock recorder for MockSyncValidator.
type MockSyncValidatorMockRecorder struct {
	mock *MockSyncValidator
}

// NewMockSyncValidator creates a new mock instance.
func NewMockSyncValidator(ctrl *gomock.Controller) *MockSyncValidator {
	mock := &MockSyncValidator{ctrl: ctrl}
	mock.recorder = &MockSyncValidatorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockSyncValidator) EXPECT() *MockSyncValidatorMockRecorder {
	return m.recorder
}

// HandleMessage mocks base method.
func (m *MockSyncValidator) HandleMessage(arg0 context.Context, arg1 types.Hash32, arg2 p2p.Peer, arg3 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandleMessage", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// HandleMessage indicates an expected call of HandleMessage.
func (mr *MockSyncValidatorMockRecorder) HandleMessage(arg0, arg1, arg2, arg3 any) *MockSyncValidatorHandleMessageCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleMessage", reflect.TypeOf((*MockSyncValidator)(nil).HandleMessage), arg0, arg1, arg2, arg3)
	return &MockSyncValidatorHandleMessageCall{Call: call}
}

// MockSyncValidatorHandleMessageCall wrap *gomock.Call
type MockSyncValidatorHandleMessageCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSyncValidatorHandleMessageCall) Return(arg0 error) *MockSyncValidatorHandleMessageCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSyncValidatorHandleMessageCall) Do(f func(context.Context, types.Hash32, p2p.Peer, []byte) error) *MockSyncValidatorHandleMessageCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSyncValidatorHandleMessageCall) DoAndReturn(f func(context.Context, types.Hash32, p2p.Peer, []byte) error) *MockSyncValidatorHandleMessageCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockPoetValidator is a mock of PoetValidator interface.
type MockPoetValidator struct {
	ctrl     *gomock.Controller
	recorder *MockPoetValidatorMockRecorder
}

// MockPoetValidatorMockRecorder is the mock recorder for MockPoetValidator.
type MockPoetValidatorMockRecorder struct {
	mock *MockPoetValidator
}

// NewMockPoetValidator creates a new mock instance.
func NewMockPoetValidator(ctrl *gomock.Controller) *MockPoetValidator {
	mock := &MockPoetValidator{ctrl: ctrl}
	mock.recorder = &MockPoetValidatorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPoetValidator) EXPECT() *MockPoetValidatorMockRecorder {
	return m.recorder
}

// ValidateAndStoreMsg mocks base method.
func (m *MockPoetValidator) ValidateAndStoreMsg(arg0 context.Context, arg1 types.Hash32, arg2 p2p.Peer, arg3 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ValidateAndStoreMsg", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// ValidateAndStoreMsg indicates an expected call of ValidateAndStoreMsg.
func (mr *MockPoetValidatorMockRecorder) ValidateAndStoreMsg(arg0, arg1, arg2, arg3 any) *MockPoetValidatorValidateAndStoreMsgCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ValidateAndStoreMsg", reflect.TypeOf((*MockPoetValidator)(nil).ValidateAndStoreMsg), arg0, arg1, arg2, arg3)
	return &MockPoetValidatorValidateAndStoreMsgCall{Call: call}
}

// MockPoetValidatorValidateAndStoreMsgCall wrap *gomock.Call
type MockPoetValidatorValidateAndStoreMsgCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockPoetValidatorValidateAndStoreMsgCall) Return(arg0 error) *MockPoetValidatorValidateAndStoreMsgCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockPoetValidatorValidateAndStoreMsgCall) Do(f func(context.Context, types.Hash32, p2p.Peer, []byte) error) *MockPoetValidatorValidateAndStoreMsgCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockPoetValidatorValidateAndStoreMsgCall) DoAndReturn(f func(context.Context, types.Hash32, p2p.Peer, []byte) error) *MockPoetValidatorValidateAndStoreMsgCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Mockhost is a mock of host interface.
type Mockhost struct {
	ctrl     *gomock.Controller
	recorder *MockhostMockRecorder
}

// MockhostMockRecorder is the mock recorder for Mockhost.
type MockhostMockRecorder struct {
	mock *Mockhost
}

// NewMockhost creates a new mock instance.
func NewMockhost(ctrl *gomock.Controller) *Mockhost {
	mock := &Mockhost{ctrl: ctrl}
	mock.recorder = &MockhostMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockhost) EXPECT() *MockhostMockRecorder {
	return m.recorder
}

// ID mocks base method.
func (m *Mockhost) ID() p2p.Peer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ID")
	ret0, _ := ret[0].(p2p.Peer)
	return ret0
}

// ID indicates an expected call of ID.
func (mr *MockhostMockRecorder) ID() *MockhostIDCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ID", reflect.TypeOf((*Mockhost)(nil).ID))
	return &MockhostIDCall{Call: call}
}

// MockhostIDCall wrap *gomock.Call
type MockhostIDCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockhostIDCall) Return(arg0 p2p.Peer) *MockhostIDCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockhostIDCall) Do(f func() p2p.Peer) *MockhostIDCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockhostIDCall) DoAndReturn(f func() p2p.Peer) *MockhostIDCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
