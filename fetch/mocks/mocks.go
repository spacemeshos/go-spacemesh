// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	p2p "github.com/spacemeshos/go-spacemesh/p2p"
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
func (m *Mockrequester) Request(arg0 context.Context, arg1 p2p.Peer, arg2 []byte, arg3 func([]byte), arg4 func(error)) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Request", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(error)
	return ret0
}

// Request indicates an expected call of Request.
func (mr *MockrequesterMockRecorder) Request(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Request", reflect.TypeOf((*Mockrequester)(nil).Request), arg0, arg1, arg2, arg3, arg4)
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
func (m *MockSyncValidator) HandleMessage(arg0 context.Context, arg1 p2p.Peer, arg2 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandleMessage", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// HandleMessage indicates an expected call of HandleMessage.
func (mr *MockSyncValidatorMockRecorder) HandleMessage(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleMessage", reflect.TypeOf((*MockSyncValidator)(nil).HandleMessage), arg0, arg1, arg2)
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
func (m *MockPoetValidator) ValidateAndStoreMsg(arg0 context.Context, arg1 p2p.Peer, arg2 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ValidateAndStoreMsg", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// ValidateAndStoreMsg indicates an expected call of ValidateAndStoreMsg.
func (mr *MockPoetValidatorMockRecorder) ValidateAndStoreMsg(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ValidateAndStoreMsg", reflect.TypeOf((*MockPoetValidator)(nil).ValidateAndStoreMsg), arg0, arg1, arg2)
}

// MockmeshProvider is a mock of meshProvider interface.
type MockmeshProvider struct {
	ctrl     *gomock.Controller
	recorder *MockmeshProviderMockRecorder
}

// MockmeshProviderMockRecorder is the mock recorder for MockmeshProvider.
type MockmeshProviderMockRecorder struct {
	mock *MockmeshProvider
}

// NewMockmeshProvider creates a new mock instance.
func NewMockmeshProvider(ctrl *gomock.Controller) *MockmeshProvider {
	mock := &MockmeshProvider{ctrl: ctrl}
	mock.recorder = &MockmeshProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockmeshProvider) EXPECT() *MockmeshProviderMockRecorder {
	return m.recorder
}

// LastVerified mocks base method.
func (m *MockmeshProvider) LastVerified() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LastVerified")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// LastVerified indicates an expected call of LastVerified.
func (mr *MockmeshProviderMockRecorder) LastVerified() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LastVerified", reflect.TypeOf((*MockmeshProvider)(nil).LastVerified))
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

// Close mocks base method.
func (m *Mockhost) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockhostMockRecorder) Close() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*Mockhost)(nil).Close))
}

// GetPeers mocks base method.
func (m *Mockhost) GetPeers() []p2p.Peer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPeers")
	ret0, _ := ret[0].([]p2p.Peer)
	return ret0
}

// GetPeers indicates an expected call of GetPeers.
func (mr *MockhostMockRecorder) GetPeers() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPeers", reflect.TypeOf((*Mockhost)(nil).GetPeers))
}

// ID mocks base method.
func (m *Mockhost) ID() p2p.Peer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ID")
	ret0, _ := ret[0].(p2p.Peer)
	return ret0
}

// ID indicates an expected call of ID.
func (mr *MockhostMockRecorder) ID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ID", reflect.TypeOf((*Mockhost)(nil).ID))
}
