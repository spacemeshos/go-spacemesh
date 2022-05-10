// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	layerfetcher "github.com/spacemeshos/go-spacemesh/layerfetcher"
)

// MocklayerTicker is a mock of layerTicker interface.
type MocklayerTicker struct {
	ctrl     *gomock.Controller
	recorder *MocklayerTickerMockRecorder
}

// MocklayerTickerMockRecorder is the mock recorder for MocklayerTicker.
type MocklayerTickerMockRecorder struct {
	mock *MocklayerTicker
}

// NewMocklayerTicker creates a new mock instance.
func NewMocklayerTicker(ctrl *gomock.Controller) *MocklayerTicker {
	mock := &MocklayerTicker{ctrl: ctrl}
	mock.recorder = &MocklayerTickerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocklayerTicker) EXPECT() *MocklayerTickerMockRecorder {
	return m.recorder
}

// GetCurrentLayer mocks base method.
func (m *MocklayerTicker) GetCurrentLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCurrentLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// GetCurrentLayer indicates an expected call of GetCurrentLayer.
func (mr *MocklayerTickerMockRecorder) GetCurrentLayer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCurrentLayer", reflect.TypeOf((*MocklayerTicker)(nil).GetCurrentLayer))
}

// MocklayerFetcher is a mock of layerFetcher interface.
type MocklayerFetcher struct {
	ctrl     *gomock.Controller
	recorder *MocklayerFetcherMockRecorder
}

// MocklayerFetcherMockRecorder is the mock recorder for MocklayerFetcher.
type MocklayerFetcherMockRecorder struct {
	mock *MocklayerFetcher
}

// NewMocklayerFetcher creates a new mock instance.
func NewMocklayerFetcher(ctrl *gomock.Controller) *MocklayerFetcher {
	mock := &MocklayerFetcher{ctrl: ctrl}
	mock.recorder = &MocklayerFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocklayerFetcher) EXPECT() *MocklayerFetcherMockRecorder {
	return m.recorder
}

// GetEpochATXs mocks base method.
func (m *MocklayerFetcher) GetEpochATXs(arg0 context.Context, arg1 types.EpochID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetEpochATXs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetEpochATXs indicates an expected call of GetEpochATXs.
func (mr *MocklayerFetcherMockRecorder) GetEpochATXs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetEpochATXs", reflect.TypeOf((*MocklayerFetcher)(nil).GetEpochATXs), arg0, arg1)
}

// PollLayerContent mocks base method.
func (m *MocklayerFetcher) PollLayerContent(arg0 context.Context, arg1 types.LayerID) chan layerfetcher.LayerPromiseResult {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PollLayerContent", arg0, arg1)
	ret0, _ := ret[0].(chan layerfetcher.LayerPromiseResult)
	return ret0
}

// PollLayerContent indicates an expected call of PollLayerContent.
func (mr *MocklayerFetcherMockRecorder) PollLayerContent(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PollLayerContent", reflect.TypeOf((*MocklayerFetcher)(nil).PollLayerContent), arg0, arg1)
}

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

// IsHareInCharge mocks base method.
func (m *MocklayerPatrol) IsHareInCharge(arg0 types.LayerID) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsHareInCharge", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsHareInCharge indicates an expected call of IsHareInCharge.
func (mr *MocklayerPatrolMockRecorder) IsHareInCharge(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsHareInCharge", reflect.TypeOf((*MocklayerPatrol)(nil).IsHareInCharge), arg0)
}

// MocklayerProcessor is a mock of layerProcessor interface.
type MocklayerProcessor struct {
	ctrl     *gomock.Controller
	recorder *MocklayerProcessorMockRecorder
}

// MocklayerProcessorMockRecorder is the mock recorder for MocklayerProcessor.
type MocklayerProcessorMockRecorder struct {
	mock *MocklayerProcessor
}

// NewMocklayerProcessor creates a new mock instance.
func NewMocklayerProcessor(ctrl *gomock.Controller) *MocklayerProcessor {
	mock := &MocklayerProcessor{ctrl: ctrl}
	mock.recorder = &MocklayerProcessorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocklayerProcessor) EXPECT() *MocklayerProcessorMockRecorder {
	return m.recorder
}

// ProcessLayer mocks base method.
func (m *MocklayerProcessor) ProcessLayer(arg0 context.Context, arg1 types.LayerID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessLayer", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// ProcessLayer indicates an expected call of ProcessLayer.
func (mr *MocklayerProcessorMockRecorder) ProcessLayer(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessLayer", reflect.TypeOf((*MocklayerProcessor)(nil).ProcessLayer), arg0, arg1)
}
