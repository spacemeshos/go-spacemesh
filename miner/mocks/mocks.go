// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

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

// GetProposalEligibility mocks base method.
func (m *MockproposalOracle) GetProposalEligibility(arg0 types.LayerID, arg1 types.Beacon) (types.ATXID, []types.ATXID, []types.VotingEligibilityProof, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProposalEligibility", arg0, arg1)
	ret0, _ := ret[0].(types.ATXID)
	ret1, _ := ret[1].([]types.ATXID)
	ret2, _ := ret[2].([]types.VotingEligibilityProof)
	ret3, _ := ret[3].(error)
	return ret0, ret1, ret2, ret3
}

// GetProposalEligibility indicates an expected call of GetProposalEligibility.
func (mr *MockproposalOracleMockRecorder) GetProposalEligibility(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposalEligibility", reflect.TypeOf((*MockproposalOracle)(nil).GetProposalEligibility), arg0, arg1)
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
