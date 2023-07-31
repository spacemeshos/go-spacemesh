// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package miner is a generated GoMock package.
package miner

import (
	context "context"
	reflect "reflect"
	time "time"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	tortoise "github.com/spacemeshos/go-spacemesh/tortoise"
)

// MockproposalOracle is a mock of proposalOracle interface.
type MockproposalOracle struct {
	ctrl     *gomock.Controller
	recorder *MockproposalOracleMockRecorder
}

// MockproposalOracleMockRecorder is the mock recorder for MockproposalOracle.
type MockproposalOracleMockRecorder struct {
	mock *MockproposalOracle
}

// NewMockproposalOracle creates a new mock instance.
func NewMockproposalOracle(ctrl *gomock.Controller) *MockproposalOracle {
	mock := &MockproposalOracle{ctrl: ctrl}
	mock.recorder = &MockproposalOracleMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockproposalOracle) EXPECT() *MockproposalOracleMockRecorder {
	return m.recorder
}

// ProposalEligibility mocks base method.
func (m *MockproposalOracle) ProposalEligibility(arg0 types.LayerID, arg1 types.Beacon, arg2 types.VRFPostIndex) (*EpochEligibility, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProposalEligibility", arg0, arg1, arg2)
	ret0, _ := ret[0].(*EpochEligibility)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ProposalEligibility indicates an expected call of ProposalEligibility.
func (mr *MockproposalOracleMockRecorder) ProposalEligibility(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProposalEligibility", reflect.TypeOf((*MockproposalOracle)(nil).ProposalEligibility), arg0, arg1, arg2)
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

// SelectProposalTXs mocks base method.
func (m *MockconservativeState) SelectProposalTXs(arg0 types.LayerID, arg1 int) []types.TransactionID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SelectProposalTXs", arg0, arg1)
	ret0, _ := ret[0].([]types.TransactionID)
	return ret0
}

// SelectProposalTXs indicates an expected call of SelectProposalTXs.
func (mr *MockconservativeStateMockRecorder) SelectProposalTXs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SelectProposalTXs", reflect.TypeOf((*MockconservativeState)(nil).SelectProposalTXs), arg0, arg1)
}

// MockvotesEncoder is a mock of votesEncoder interface.
type MockvotesEncoder struct {
	ctrl     *gomock.Controller
	recorder *MockvotesEncoderMockRecorder
}

// MockvotesEncoderMockRecorder is the mock recorder for MockvotesEncoder.
type MockvotesEncoderMockRecorder struct {
	mock *MockvotesEncoder
}

// NewMockvotesEncoder creates a new mock instance.
func NewMockvotesEncoder(ctrl *gomock.Controller) *MockvotesEncoder {
	mock := &MockvotesEncoder{ctrl: ctrl}
	mock.recorder = &MockvotesEncoderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockvotesEncoder) EXPECT() *MockvotesEncoderMockRecorder {
	return m.recorder
}

// EncodeVotes mocks base method.
func (m *MockvotesEncoder) EncodeVotes(arg0 context.Context, arg1 ...tortoise.EncodeVotesOpts) (*types.Opinion, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{arg0}
	for _, a := range arg1 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "EncodeVotes", varargs...)
	ret0, _ := ret[0].(*types.Opinion)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// EncodeVotes indicates an expected call of EncodeVotes.
func (mr *MockvotesEncoderMockRecorder) EncodeVotes(arg0 interface{}, arg1 ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{arg0}, arg1...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "EncodeVotes", reflect.TypeOf((*MockvotesEncoder)(nil).EncodeVotes), varargs...)
}

// LatestComplete mocks base method.
func (m *MockvotesEncoder) LatestComplete() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LatestComplete")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// LatestComplete indicates an expected call of LatestComplete.
func (mr *MockvotesEncoderMockRecorder) LatestComplete() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LatestComplete", reflect.TypeOf((*MockvotesEncoder)(nil).LatestComplete))
}

// TallyVotes mocks base method.
func (m *MockvotesEncoder) TallyVotes(arg0 context.Context, arg1 types.LayerID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "TallyVotes", arg0, arg1)
}

// TallyVotes indicates an expected call of TallyVotes.
func (mr *MockvotesEncoderMockRecorder) TallyVotes(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TallyVotes", reflect.TypeOf((*MockvotesEncoder)(nil).TallyVotes), arg0, arg1)
}

// MocknonceFetcher is a mock of nonceFetcher interface.
type MocknonceFetcher struct {
	ctrl     *gomock.Controller
	recorder *MocknonceFetcherMockRecorder
}

// MocknonceFetcherMockRecorder is the mock recorder for MocknonceFetcher.
type MocknonceFetcherMockRecorder struct {
	mock *MocknonceFetcher
}

// NewMocknonceFetcher creates a new mock instance.
func NewMocknonceFetcher(ctrl *gomock.Controller) *MocknonceFetcher {
	mock := &MocknonceFetcher{ctrl: ctrl}
	mock.recorder = &MocknonceFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocknonceFetcher) EXPECT() *MocknonceFetcherMockRecorder {
	return m.recorder
}

// VRFNonce mocks base method.
func (m *MocknonceFetcher) VRFNonce(arg0 types.NodeID, arg1 types.EpochID) (types.VRFPostIndex, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VRFNonce", arg0, arg1)
	ret0, _ := ret[0].(types.VRFPostIndex)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// VRFNonce indicates an expected call of VRFNonce.
func (mr *MocknonceFetcherMockRecorder) VRFNonce(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VRFNonce", reflect.TypeOf((*MocknonceFetcher)(nil).VRFNonce), arg0, arg1)
}

// Mockmesh is a mock of mesh interface.
type Mockmesh struct {
	ctrl     *gomock.Controller
	recorder *MockmeshMockRecorder
}

// MockmeshMockRecorder is the mock recorder for Mockmesh.
type MockmeshMockRecorder struct {
	mock *Mockmesh
}

// NewMockmesh creates a new mock instance.
func NewMockmesh(ctrl *gomock.Controller) *Mockmesh {
	mock := &Mockmesh{ctrl: ctrl}
	mock.recorder = &MockmeshMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockmesh) EXPECT() *MockmeshMockRecorder {
	return m.recorder
}

// GetMalfeasanceProof mocks base method.
func (m *Mockmesh) GetMalfeasanceProof(nodeID types.NodeID) (*types.MalfeasanceProof, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMalfeasanceProof", nodeID)
	ret0, _ := ret[0].(*types.MalfeasanceProof)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetMalfeasanceProof indicates an expected call of GetMalfeasanceProof.
func (mr *MockmeshMockRecorder) GetMalfeasanceProof(nodeID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMalfeasanceProof", reflect.TypeOf((*Mockmesh)(nil).GetMalfeasanceProof), nodeID)
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
func (m *MocklayerClock) AwaitLayer(layerID types.LayerID) <-chan struct{} {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AwaitLayer", layerID)
	ret0, _ := ret[0].(<-chan struct{})
	return ret0
}

// AwaitLayer indicates an expected call of AwaitLayer.
func (mr *MocklayerClockMockRecorder) AwaitLayer(layerID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AwaitLayer", reflect.TypeOf((*MocklayerClock)(nil).AwaitLayer), layerID)
}

// CurrentLayer mocks base method.
func (m *MocklayerClock) CurrentLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// CurrentLayer indicates an expected call of CurrentLayer.
func (mr *MocklayerClockMockRecorder) CurrentLayer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentLayer", reflect.TypeOf((*MocklayerClock)(nil).CurrentLayer))
}

// LayerToTime mocks base method.
func (m *MocklayerClock) LayerToTime(arg0 types.LayerID) time.Time {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LayerToTime", arg0)
	ret0, _ := ret[0].(time.Time)
	return ret0
}

// LayerToTime indicates an expected call of LayerToTime.
func (mr *MocklayerClockMockRecorder) LayerToTime(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LayerToTime", reflect.TypeOf((*MocklayerClock)(nil).LayerToTime), arg0)
}
