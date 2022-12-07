// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	log "github.com/spacemeshos/go-spacemesh/log"
)

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

// AddBlockWithTXs mocks base method.
func (m *MockmeshProvider) AddBlockWithTXs(arg0 context.Context, arg1 *types.Block) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddBlockWithTXs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddBlockWithTXs indicates an expected call of AddBlockWithTXs.
func (mr *MockmeshProviderMockRecorder) AddBlockWithTXs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddBlockWithTXs", reflect.TypeOf((*MockmeshProvider)(nil).AddBlockWithTXs), arg0, arg1)
}

// ProcessLayerPerHareOutput mocks base method.
func (m *MockmeshProvider) ProcessLayerPerHareOutput(arg0 context.Context, arg1 types.LayerID, arg2 types.BlockID, arg3 bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessLayerPerHareOutput", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// ProcessLayerPerHareOutput indicates an expected call of ProcessLayerPerHareOutput.
func (mr *MockmeshProviderMockRecorder) ProcessLayerPerHareOutput(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessLayerPerHareOutput", reflect.TypeOf((*MockmeshProvider)(nil).ProcessLayerPerHareOutput), arg0, arg1, arg2, arg3)
}

// MockconservativeState is a mock of conservativeState interface.
type MockconservativeState struct {
	ctrl     *gomock.Controller
	recorder *MockconservativeStateMockRecorder
}

// MockconservativeStateMockRecorder is the mock recorder for MockconservativeState.
type MockconservativeStateMockRecorder struct {
	mock *MockconservativeState
}

// NewMockconservativeState creates a new mock instance.
func NewMockconservativeState(ctrl *gomock.Controller) *MockconservativeState {
	mock := &MockconservativeState{ctrl: ctrl}
	mock.recorder = &MockconservativeStateMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockconservativeState) EXPECT() *MockconservativeStateMockRecorder {
	return m.recorder
}

// GenerateBlock mocks base method.
func (m *MockconservativeState) GenerateBlock(arg0 context.Context, arg1 types.LayerID, arg2 []*types.Proposal) (*types.Block, bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GenerateBlock", arg0, arg1, arg2)
	ret0, _ := ret[0].(*types.Block)
	ret1, _ := ret[1].(bool)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GenerateBlock indicates an expected call of GenerateBlock.
func (mr *MockconservativeStateMockRecorder) GenerateBlock(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GenerateBlock", reflect.TypeOf((*MockconservativeState)(nil).GenerateBlock), arg0, arg1, arg2)
}

// MocklayerClock is a mock of layerClock interface.
type MocklayerClock struct {
	ctrl     *gomock.Controller
	recorder *MocklayerClockMockRecorder
}

// MocklayerClockMockRecorder is the mock recorder for MocklayerClock.
type MocklayerClockMockRecorder struct {
	mock *MocklayerClock
}

// NewMocklayerClock creates a new mock instance.
func NewMocklayerClock(ctrl *gomock.Controller) *MocklayerClock {
	mock := &MocklayerClock{ctrl: ctrl}
	mock.recorder = &MocklayerClockMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocklayerClock) EXPECT() *MocklayerClockMockRecorder {
	return m.recorder
}

// AwaitLayer mocks base method.
func (m *MocklayerClock) AwaitLayer(layerID types.LayerID) chan struct{} {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AwaitLayer", layerID)
	ret0, _ := ret[0].(chan struct{})
	return ret0
}

// AwaitLayer indicates an expected call of AwaitLayer.
func (mr *MocklayerClockMockRecorder) AwaitLayer(layerID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AwaitLayer", reflect.TypeOf((*MocklayerClock)(nil).AwaitLayer), layerID)
}

// GetCurrentLayer mocks base method.
func (m *MocklayerClock) GetCurrentLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCurrentLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// GetCurrentLayer indicates an expected call of GetCurrentLayer.
func (mr *MocklayerClockMockRecorder) GetCurrentLayer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCurrentLayer", reflect.TypeOf((*MocklayerClock)(nil).GetCurrentLayer))
}

// Mockcertifier is a mock of certifier interface.
type Mockcertifier struct {
	ctrl     *gomock.Controller
	recorder *MockcertifierMockRecorder
}

// MockcertifierMockRecorder is the mock recorder for Mockcertifier.
type MockcertifierMockRecorder struct {
	mock *Mockcertifier
}

// NewMockcertifier creates a new mock instance.
func NewMockcertifier(ctrl *gomock.Controller) *Mockcertifier {
	mock := &Mockcertifier{ctrl: ctrl}
	mock.recorder = &MockcertifierMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockcertifier) EXPECT() *MockcertifierMockRecorder {
	return m.recorder
}

// CertifyIfEligible mocks base method.
func (m *Mockcertifier) CertifyIfEligible(arg0 context.Context, arg1 log.Log, arg2 types.LayerID, arg3 types.BlockID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CertifyIfEligible", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// CertifyIfEligible indicates an expected call of CertifyIfEligible.
func (mr *MockcertifierMockRecorder) CertifyIfEligible(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CertifyIfEligible", reflect.TypeOf((*Mockcertifier)(nil).CertifyIfEligible), arg0, arg1, arg2, arg3)
}

// RegisterForCert mocks base method.
func (m *Mockcertifier) RegisterForCert(arg0 context.Context, arg1 types.LayerID, arg2 types.BlockID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterForCert", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterForCert indicates an expected call of RegisterForCert.
func (mr *MockcertifierMockRecorder) RegisterForCert(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterForCert", reflect.TypeOf((*Mockcertifier)(nil).RegisterForCert), arg0, arg1, arg2)
}
