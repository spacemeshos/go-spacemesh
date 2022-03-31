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

// SelectTXsForProposal mocks base method.
func (m *MockconservativeState) SelectTXsForProposal(arg0 int) ([]types.TransactionID, []*types.Transaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SelectTXsForProposal", arg0)
	ret0, _ := ret[0].([]types.TransactionID)
	ret1, _ := ret[1].([]*types.Transaction)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// SelectTXsForProposal indicates an expected call of SelectTXsForProposal.
func (mr *MockconservativeStateMockRecorder) SelectTXsForProposal(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SelectTXsForProposal", reflect.TypeOf((*MockconservativeState)(nil).SelectTXsForProposal), arg0)
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
func (m *MockvotesEncoder) EncodeVotes(arg0 context.Context, arg1 ...tortoise.EncodeVotesOpts) (*types.Votes, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{arg0}
	for _, a := range arg1 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "EncodeVotes", varargs...)
	ret0, _ := ret[0].(*types.Votes)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// EncodeVotes indicates an expected call of EncodeVotes.
func (mr *MockvotesEncoderMockRecorder) EncodeVotes(arg0 interface{}, arg1 ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{arg0}, arg1...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "EncodeVotes", reflect.TypeOf((*MockvotesEncoder)(nil).EncodeVotes), varargs...)
}

// MockactivationDB is a mock of activationDB interface.
type MockactivationDB struct {
	ctrl     *gomock.Controller
	recorder *MockactivationDBMockRecorder
}

// MockactivationDBMockRecorder is the mock recorder for MockactivationDB.
type MockactivationDBMockRecorder struct {
	mock *MockactivationDB
}

// NewMockactivationDB creates a new mock instance.
func NewMockactivationDB(ctrl *gomock.Controller) *MockactivationDB {
	mock := &MockactivationDB{ctrl: ctrl}
	mock.recorder = &MockactivationDBMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockactivationDB) EXPECT() *MockactivationDBMockRecorder {
	return m.recorder
}

// GetAtxHeader mocks base method.
func (m *MockactivationDB) GetAtxHeader(arg0 types.ATXID) (*types.ActivationTxHeader, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAtxHeader", arg0)
	ret0, _ := ret[0].(*types.ActivationTxHeader)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAtxHeader indicates an expected call of GetAtxHeader.
func (mr *MockactivationDBMockRecorder) GetAtxHeader(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAtxHeader", reflect.TypeOf((*MockactivationDB)(nil).GetAtxHeader), arg0)
}

// GetEpochWeight mocks base method.
func (m *MockactivationDB) GetEpochWeight(arg0 types.EpochID) (uint64, []types.ATXID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetEpochWeight", arg0)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].([]types.ATXID)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetEpochWeight indicates an expected call of GetEpochWeight.
func (mr *MockactivationDBMockRecorder) GetEpochWeight(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetEpochWeight", reflect.TypeOf((*MockactivationDB)(nil).GetEpochWeight), arg0)
}

// GetNodeAtxIDForEpoch mocks base method.
func (m *MockactivationDB) GetNodeAtxIDForEpoch(nodeID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNodeAtxIDForEpoch", nodeID, targetEpoch)
	ret0, _ := ret[0].(types.ATXID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetNodeAtxIDForEpoch indicates an expected call of GetNodeAtxIDForEpoch.
func (mr *MockactivationDBMockRecorder) GetNodeAtxIDForEpoch(nodeID, targetEpoch interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNodeAtxIDForEpoch", reflect.TypeOf((*MockactivationDB)(nil).GetNodeAtxIDForEpoch), nodeID, targetEpoch)
}
