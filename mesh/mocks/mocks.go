// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
)

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

// ApplyLayer mocks base method.
func (m *MockconservativeState) ApplyLayer(arg0 context.Context, arg1 types.LayerID, arg2 *types.Block) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ApplyLayer", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// ApplyLayer indicates an expected call of ApplyLayer.
func (mr *MockconservativeStateMockRecorder) ApplyLayer(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ApplyLayer", reflect.TypeOf((*MockconservativeState)(nil).ApplyLayer), arg0, arg1, arg2)
}

// GetStateRoot mocks base method.
func (m *MockconservativeState) GetStateRoot() (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetStateRoot")
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetStateRoot indicates an expected call of GetStateRoot.
func (mr *MockconservativeStateMockRecorder) GetStateRoot() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStateRoot", reflect.TypeOf((*MockconservativeState)(nil).GetStateRoot))
}

// LinkTXsWithBlock mocks base method.
func (m *MockconservativeState) LinkTXsWithBlock(arg0 types.LayerID, arg1 types.BlockID, arg2 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LinkTXsWithBlock", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// LinkTXsWithBlock indicates an expected call of LinkTXsWithBlock.
func (mr *MockconservativeStateMockRecorder) LinkTXsWithBlock(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LinkTXsWithBlock", reflect.TypeOf((*MockconservativeState)(nil).LinkTXsWithBlock), arg0, arg1, arg2)
}

// LinkTXsWithProposal mocks base method.
func (m *MockconservativeState) LinkTXsWithProposal(arg0 types.LayerID, arg1 types.ProposalID, arg2 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LinkTXsWithProposal", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// LinkTXsWithProposal indicates an expected call of LinkTXsWithProposal.
func (mr *MockconservativeStateMockRecorder) LinkTXsWithProposal(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LinkTXsWithProposal", reflect.TypeOf((*MockconservativeState)(nil).LinkTXsWithProposal), arg0, arg1, arg2)
}

// RevertState mocks base method.
func (m *MockconservativeState) RevertState(arg0 types.LayerID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RevertState", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// RevertState indicates an expected call of RevertState.
func (mr *MockconservativeStateMockRecorder) RevertState(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RevertState", reflect.TypeOf((*MockconservativeState)(nil).RevertState), arg0)
}
