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

// MockblockDataProvider is a mock of blockDataProvider interface.
type MockblockDataProvider struct {
	ctrl     *gomock.Controller
	recorder *MockblockDataProviderMockRecorder
}

// MockblockDataProviderMockRecorder is the mock recorder for MockblockDataProvider.
type MockblockDataProviderMockRecorder struct {
	mock *MockblockDataProvider
}

// NewMockblockDataProvider creates a new mock instance.
func NewMockblockDataProvider(ctrl *gomock.Controller) *MockblockDataProvider {
	mock := &MockblockDataProvider{ctrl: ctrl}
	mock.recorder = &MockblockDataProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockblockDataProvider) EXPECT() *MockblockDataProviderMockRecorder {
	return m.recorder
}

// GetBallot mocks base method.
func (m *MockblockDataProvider) GetBallot(id types.BallotID) (*types.Ballot, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBallot", id)
	ret0, _ := ret[0].(*types.Ballot)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBallot indicates an expected call of GetBallot.
func (mr *MockblockDataProviderMockRecorder) GetBallot(id interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBallot", reflect.TypeOf((*MockblockDataProvider)(nil).GetBallot), id)
}

// GetBlock mocks base method.
func (m *MockblockDataProvider) GetBlock(arg0 types.BlockID) (*types.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlock", arg0)
	ret0, _ := ret[0].(*types.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBlock indicates an expected call of GetBlock.
func (mr *MockblockDataProviderMockRecorder) GetBlock(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlock", reflect.TypeOf((*MockblockDataProvider)(nil).GetBlock), arg0)
}

// GetCoinflip mocks base method.
func (m *MockblockDataProvider) GetCoinflip(arg0 context.Context, arg1 types.LayerID) (bool, bool) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCoinflip", arg0, arg1)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(bool)
	return ret0, ret1
}

// GetCoinflip indicates an expected call of GetCoinflip.
func (mr *MockblockDataProviderMockRecorder) GetCoinflip(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCoinflip", reflect.TypeOf((*MockblockDataProvider)(nil).GetCoinflip), arg0, arg1)
}

// GetHareConsensusOutput mocks base method.
func (m *MockblockDataProvider) GetHareConsensusOutput(arg0 types.LayerID) (types.BlockID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetHareConsensusOutput", arg0)
	ret0, _ := ret[0].(types.BlockID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetHareConsensusOutput indicates an expected call of GetHareConsensusOutput.
func (mr *MockblockDataProviderMockRecorder) GetHareConsensusOutput(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetHareConsensusOutput", reflect.TypeOf((*MockblockDataProvider)(nil).GetHareConsensusOutput), arg0)
}

// LayerBallots mocks base method.
func (m *MockblockDataProvider) LayerBallots(arg0 types.LayerID) ([]*types.Ballot, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LayerBallots", arg0)
	ret0, _ := ret[0].([]*types.Ballot)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LayerBallots indicates an expected call of LayerBallots.
func (mr *MockblockDataProviderMockRecorder) LayerBallots(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LayerBallots", reflect.TypeOf((*MockblockDataProvider)(nil).LayerBallots), arg0)
}

// LayerBlockIds mocks base method.
func (m *MockblockDataProvider) LayerBlockIds(layerID types.LayerID) ([]types.BlockID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LayerBlockIds", layerID)
	ret0, _ := ret[0].([]types.BlockID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LayerBlockIds indicates an expected call of LayerBlockIds.
func (mr *MockblockDataProviderMockRecorder) LayerBlockIds(layerID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LayerBlockIds", reflect.TypeOf((*MockblockDataProvider)(nil).LayerBlockIds), layerID)
}

// LayerContextualValidity mocks base method.
func (m *MockblockDataProvider) LayerContextualValidity(arg0 types.LayerID) ([]types.BlockContextualValidity, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LayerContextualValidity", arg0)
	ret0, _ := ret[0].([]types.BlockContextualValidity)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LayerContextualValidity indicates an expected call of LayerContextualValidity.
func (mr *MockblockDataProviderMockRecorder) LayerContextualValidity(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LayerContextualValidity", reflect.TypeOf((*MockblockDataProvider)(nil).LayerContextualValidity), arg0)
}

// MockblockValidityUpdater is a mock of blockValidityUpdater interface.
type MockblockValidityUpdater struct {
	ctrl     *gomock.Controller
	recorder *MockblockValidityUpdaterMockRecorder
}

// MockblockValidityUpdaterMockRecorder is the mock recorder for MockblockValidityUpdater.
type MockblockValidityUpdaterMockRecorder struct {
	mock *MockblockValidityUpdater
}

// NewMockblockValidityUpdater creates a new mock instance.
func NewMockblockValidityUpdater(ctrl *gomock.Controller) *MockblockValidityUpdater {
	mock := &MockblockValidityUpdater{ctrl: ctrl}
	mock.recorder = &MockblockValidityUpdaterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockblockValidityUpdater) EXPECT() *MockblockValidityUpdaterMockRecorder {
	return m.recorder
}

// UpdateBlockValidity mocks base method.
func (m *MockblockValidityUpdater) UpdateBlockValidity(arg0 types.BlockID, arg1 types.LayerID, arg2 bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateBlockValidity", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateBlockValidity indicates an expected call of UpdateBlockValidity.
func (mr *MockblockValidityUpdaterMockRecorder) UpdateBlockValidity(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateBlockValidity", reflect.TypeOf((*MockblockValidityUpdater)(nil).UpdateBlockValidity), arg0, arg1, arg2)
}

// MockatxDataProvider is a mock of atxDataProvider interface.
type MockatxDataProvider struct {
	ctrl     *gomock.Controller
	recorder *MockatxDataProviderMockRecorder
}

// MockatxDataProviderMockRecorder is the mock recorder for MockatxDataProvider.
type MockatxDataProviderMockRecorder struct {
	mock *MockatxDataProvider
}

// NewMockatxDataProvider creates a new mock instance.
func NewMockatxDataProvider(ctrl *gomock.Controller) *MockatxDataProvider {
	mock := &MockatxDataProvider{ctrl: ctrl}
	mock.recorder = &MockatxDataProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockatxDataProvider) EXPECT() *MockatxDataProviderMockRecorder {
	return m.recorder
}

// GetAtxHeader mocks base method.
func (m *MockatxDataProvider) GetAtxHeader(arg0 types.ATXID) (*types.ActivationTxHeader, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAtxHeader", arg0)
	ret0, _ := ret[0].(*types.ActivationTxHeader)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAtxHeader indicates an expected call of GetAtxHeader.
func (mr *MockatxDataProviderMockRecorder) GetAtxHeader(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAtxHeader", reflect.TypeOf((*MockatxDataProvider)(nil).GetAtxHeader), arg0)
}

// GetEpochWeight mocks base method.
func (m *MockatxDataProvider) GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetEpochWeight", epochID)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].([]types.ATXID)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetEpochWeight indicates an expected call of GetEpochWeight.
func (mr *MockatxDataProviderMockRecorder) GetEpochWeight(epochID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetEpochWeight", reflect.TypeOf((*MockatxDataProvider)(nil).GetEpochWeight), epochID)
}
