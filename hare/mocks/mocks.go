// Code generated by MockGen. DO NOT EDIT.
// Source: ./interfaces.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
)

// MocklayerPatrol is a mock of layerPatrol interface.
type MocklayerPatrol struct {
	ctrl     *gomock.Controller
	recorder *MocklayerPatrolMockRecorder
}

// MocklayerPatrolMockRecorder is the mock recorder for MocklayerPatrol.
type MocklayerPatrolMockRecorder struct {
	mock *MocklayerPatrol
}

// NewMocklayerPatrol creates a new mock instance.
func NewMocklayerPatrol(ctrl *gomock.Controller) *MocklayerPatrol {
	mock := &MocklayerPatrol{ctrl: ctrl}
	mock.recorder = &MocklayerPatrolMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocklayerPatrol) EXPECT() *MocklayerPatrolMockRecorder {
	return m.recorder
}

// SetHareInCharge mocks base method.
func (m *MocklayerPatrol) SetHareInCharge(arg0 types.LayerID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetHareInCharge", arg0)
}

// SetHareInCharge indicates an expected call of SetHareInCharge.
func (mr *MocklayerPatrolMockRecorder) SetHareInCharge(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetHareInCharge", reflect.TypeOf((*MocklayerPatrol)(nil).SetHareInCharge), arg0)
}

// MockRolacle is a mock of Rolacle interface.
type MockRolacle struct {
	ctrl     *gomock.Controller
	recorder *MockRolacleMockRecorder
}

// MockRolacleMockRecorder is the mock recorder for MockRolacle.
type MockRolacleMockRecorder struct {
	mock *MockRolacle
}

// NewMockRolacle creates a new mock instance.
func NewMockRolacle(ctrl *gomock.Controller) *MockRolacle {
	mock := &MockRolacle{ctrl: ctrl}
	mock.recorder = &MockRolacleMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRolacle) EXPECT() *MockRolacleMockRecorder {
	return m.recorder
}

// CalcEligibility mocks base method.
func (m *MockRolacle) CalcEligibility(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, sig []byte) (uint16, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CalcEligibility", ctx, layer, round, committeeSize, id, sig)
	ret0, _ := ret[0].(uint16)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CalcEligibility indicates an expected call of CalcEligibility.
func (mr *MockRolacleMockRecorder) CalcEligibility(ctx, layer, round, committeeSize, id, sig interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CalcEligibility", reflect.TypeOf((*MockRolacle)(nil).CalcEligibility), ctx, layer, round, committeeSize, id, sig)
}

// IsIdentityActiveOnConsensusView mocks base method.
func (m *MockRolacle) IsIdentityActiveOnConsensusView(ctx context.Context, edID string, layer types.LayerID) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsIdentityActiveOnConsensusView", ctx, edID, layer)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// IsIdentityActiveOnConsensusView indicates an expected call of IsIdentityActiveOnConsensusView.
func (mr *MockRolacleMockRecorder) IsIdentityActiveOnConsensusView(ctx, edID, layer interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsIdentityActiveOnConsensusView", reflect.TypeOf((*MockRolacle)(nil).IsIdentityActiveOnConsensusView), ctx, edID, layer)
}

// Proof mocks base method.
func (m *MockRolacle) Proof(ctx context.Context, layer types.LayerID, round uint32) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Proof", ctx, layer, round)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Proof indicates an expected call of Proof.
func (mr *MockRolacleMockRecorder) Proof(ctx, layer, round interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Proof", reflect.TypeOf((*MockRolacle)(nil).Proof), ctx, layer, round)
}

// Validate mocks base method.
func (m *MockRolacle) Validate(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, sig []byte, eligibilityCount uint16) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validate", ctx, layer, round, committeeSize, id, sig, eligibilityCount)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Validate indicates an expected call of Validate.
func (mr *MockRolacleMockRecorder) Validate(ctx, layer, round, committeeSize, id, sig, eligibilityCount interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validate", reflect.TypeOf((*MockRolacle)(nil).Validate), ctx, layer, round, committeeSize, id, sig, eligibilityCount)
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

// GetBlock mocks base method.
func (m *MockmeshProvider) GetBlock(arg0 types.BlockID) (*types.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlock", arg0)
	ret0, _ := ret[0].(*types.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBlock indicates an expected call of GetBlock.
func (mr *MockmeshProviderMockRecorder) GetBlock(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlock", reflect.TypeOf((*MockmeshProvider)(nil).GetBlock), arg0)
}

// HandleValidatedLayer mocks base method.
func (m *MockmeshProvider) HandleValidatedLayer(ctx context.Context, validatedLayer types.LayerID, layer []types.BlockID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "HandleValidatedLayer", ctx, validatedLayer, layer)
}

// HandleValidatedLayer indicates an expected call of HandleValidatedLayer.
func (mr *MockmeshProviderMockRecorder) HandleValidatedLayer(ctx, validatedLayer, layer interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleValidatedLayer", reflect.TypeOf((*MockmeshProvider)(nil).HandleValidatedLayer), ctx, validatedLayer, layer)
}

// InvalidateLayer mocks base method.
func (m *MockmeshProvider) InvalidateLayer(ctx context.Context, layerID types.LayerID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "InvalidateLayer", ctx, layerID)
}

// InvalidateLayer indicates an expected call of InvalidateLayer.
func (mr *MockmeshProviderMockRecorder) InvalidateLayer(ctx, layerID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InvalidateLayer", reflect.TypeOf((*MockmeshProvider)(nil).InvalidateLayer), ctx, layerID)
}

// LayerBlocks mocks base method.
func (m *MockmeshProvider) LayerBlocks(arg0 types.LayerID) ([]*types.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LayerBlocks", arg0)
	ret0, _ := ret[0].([]*types.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LayerBlocks indicates an expected call of LayerBlocks.
func (mr *MockmeshProviderMockRecorder) LayerBlocks(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LayerBlocks", reflect.TypeOf((*MockmeshProvider)(nil).LayerBlocks), arg0)
}

// RecordCoinflip mocks base method.
func (m *MockmeshProvider) RecordCoinflip(ctx context.Context, layerID types.LayerID, coinflip bool) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RecordCoinflip", ctx, layerID, coinflip)
}

// RecordCoinflip indicates an expected call of RecordCoinflip.
func (mr *MockmeshProviderMockRecorder) RecordCoinflip(ctx, layerID, coinflip interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RecordCoinflip", reflect.TypeOf((*MockmeshProvider)(nil).RecordCoinflip), ctx, layerID, coinflip)
}
