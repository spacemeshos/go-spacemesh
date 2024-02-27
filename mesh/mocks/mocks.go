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
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	gomock "go.uber.org/mock/gomock"
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

// LinkTXsWithBlock mocks base method.
func (m *MockconservativeState) LinkTXsWithBlock(arg0 types.LayerID, arg1 types.BlockID, arg2 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LinkTXsWithBlock", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// LinkTXsWithBlock indicates an expected call of LinkTXsWithBlock.
func (mr *MockconservativeStateMockRecorder) LinkTXsWithBlock(arg0, arg1, arg2 any) *MockconservativeStateLinkTXsWithBlockCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LinkTXsWithBlock", reflect.TypeOf((*MockconservativeState)(nil).LinkTXsWithBlock), arg0, arg1, arg2)
	return &MockconservativeStateLinkTXsWithBlockCall{Call: call}
}

// MockconservativeStateLinkTXsWithBlockCall wrap *gomock.Call
type MockconservativeStateLinkTXsWithBlockCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockconservativeStateLinkTXsWithBlockCall) Return(arg0 error) *MockconservativeStateLinkTXsWithBlockCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockconservativeStateLinkTXsWithBlockCall) Do(f func(types.LayerID, types.BlockID, []types.TransactionID) error) *MockconservativeStateLinkTXsWithBlockCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockconservativeStateLinkTXsWithBlockCall) DoAndReturn(f func(types.LayerID, types.BlockID, []types.TransactionID) error) *MockconservativeStateLinkTXsWithBlockCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// LinkTXsWithProposal mocks base method.
func (m *MockconservativeState) LinkTXsWithProposal(arg0 types.LayerID, arg1 types.ProposalID, arg2 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LinkTXsWithProposal", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// LinkTXsWithProposal indicates an expected call of LinkTXsWithProposal.
func (mr *MockconservativeStateMockRecorder) LinkTXsWithProposal(arg0, arg1, arg2 any) *MockconservativeStateLinkTXsWithProposalCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LinkTXsWithProposal", reflect.TypeOf((*MockconservativeState)(nil).LinkTXsWithProposal), arg0, arg1, arg2)
	return &MockconservativeStateLinkTXsWithProposalCall{Call: call}
}

// MockconservativeStateLinkTXsWithProposalCall wrap *gomock.Call
type MockconservativeStateLinkTXsWithProposalCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockconservativeStateLinkTXsWithProposalCall) Return(arg0 error) *MockconservativeStateLinkTXsWithProposalCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockconservativeStateLinkTXsWithProposalCall) Do(f func(types.LayerID, types.ProposalID, []types.TransactionID) error) *MockconservativeStateLinkTXsWithProposalCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockconservativeStateLinkTXsWithProposalCall) DoAndReturn(f func(types.LayerID, types.ProposalID, []types.TransactionID) error) *MockconservativeStateLinkTXsWithProposalCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RevertCache mocks base method.
func (m *MockconservativeState) RevertCache(arg0 types.LayerID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RevertCache", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// RevertCache indicates an expected call of RevertCache.
func (mr *MockconservativeStateMockRecorder) RevertCache(arg0 any) *MockconservativeStateRevertCacheCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RevertCache", reflect.TypeOf((*MockconservativeState)(nil).RevertCache), arg0)
	return &MockconservativeStateRevertCacheCall{Call: call}
}

// MockconservativeStateRevertCacheCall wrap *gomock.Call
type MockconservativeStateRevertCacheCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockconservativeStateRevertCacheCall) Return(arg0 error) *MockconservativeStateRevertCacheCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockconservativeStateRevertCacheCall) Do(f func(types.LayerID) error) *MockconservativeStateRevertCacheCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockconservativeStateRevertCacheCall) DoAndReturn(f func(types.LayerID) error) *MockconservativeStateRevertCacheCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// UpdateCache mocks base method.
func (m *MockconservativeState) UpdateCache(arg0 context.Context, arg1 types.LayerID, arg2 types.BlockID, arg3 []types.TransactionWithResult, arg4 []types.Transaction) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateCache", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateCache indicates an expected call of UpdateCache.
func (mr *MockconservativeStateMockRecorder) UpdateCache(arg0, arg1, arg2, arg3, arg4 any) *MockconservativeStateUpdateCacheCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateCache", reflect.TypeOf((*MockconservativeState)(nil).UpdateCache), arg0, arg1, arg2, arg3, arg4)
	return &MockconservativeStateUpdateCacheCall{Call: call}
}

// MockconservativeStateUpdateCacheCall wrap *gomock.Call
type MockconservativeStateUpdateCacheCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockconservativeStateUpdateCacheCall) Return(arg0 error) *MockconservativeStateUpdateCacheCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockconservativeStateUpdateCacheCall) Do(f func(context.Context, types.LayerID, types.BlockID, []types.TransactionWithResult, []types.Transaction) error) *MockconservativeStateUpdateCacheCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockconservativeStateUpdateCacheCall) DoAndReturn(f func(context.Context, types.LayerID, types.BlockID, []types.TransactionWithResult, []types.Transaction) error) *MockconservativeStateUpdateCacheCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockvmState is a mock of vmState interface.
type MockvmState struct {
	ctrl     *gomock.Controller
	recorder *MockvmStateMockRecorder
}

// MockvmStateMockRecorder is the mock recorder for MockvmState.
type MockvmStateMockRecorder struct {
	mock *MockvmState
}

// NewMockvmState creates a new mock instance.
func NewMockvmState(ctrl *gomock.Controller) *MockvmState {
	mock := &MockvmState{ctrl: ctrl}
	mock.recorder = &MockvmStateMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockvmState) EXPECT() *MockvmStateMockRecorder {
	return m.recorder
}

// Apply mocks base method.
func (m *MockvmState) Apply(arg0 vm.ApplyContext, arg1 []types.Transaction, arg2 []types.CoinbaseReward) ([]types.Transaction, []types.TransactionWithResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Apply", arg0, arg1, arg2)
	ret0, _ := ret[0].([]types.Transaction)
	ret1, _ := ret[1].([]types.TransactionWithResult)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// Apply indicates an expected call of Apply.
func (mr *MockvmStateMockRecorder) Apply(arg0, arg1, arg2 any) *MockvmStateApplyCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Apply", reflect.TypeOf((*MockvmState)(nil).Apply), arg0, arg1, arg2)
	return &MockvmStateApplyCall{Call: call}
}

// MockvmStateApplyCall wrap *gomock.Call
type MockvmStateApplyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockvmStateApplyCall) Return(arg0 []types.Transaction, arg1 []types.TransactionWithResult, arg2 error) *MockvmStateApplyCall {
	c.Call = c.Call.Return(arg0, arg1, arg2)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockvmStateApplyCall) Do(f func(vm.ApplyContext, []types.Transaction, []types.CoinbaseReward) ([]types.Transaction, []types.TransactionWithResult, error)) *MockvmStateApplyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockvmStateApplyCall) DoAndReturn(f func(vm.ApplyContext, []types.Transaction, []types.CoinbaseReward) ([]types.Transaction, []types.TransactionWithResult, error)) *MockvmStateApplyCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetStateRoot mocks base method.
func (m *MockvmState) GetStateRoot() (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetStateRoot")
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetStateRoot indicates an expected call of GetStateRoot.
func (mr *MockvmStateMockRecorder) GetStateRoot() *MockvmStateGetStateRootCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStateRoot", reflect.TypeOf((*MockvmState)(nil).GetStateRoot))
	return &MockvmStateGetStateRootCall{Call: call}
}

// MockvmStateGetStateRootCall wrap *gomock.Call
type MockvmStateGetStateRootCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockvmStateGetStateRootCall) Return(arg0 types.Hash32, arg1 error) *MockvmStateGetStateRootCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockvmStateGetStateRootCall) Do(f func() (types.Hash32, error)) *MockvmStateGetStateRootCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockvmStateGetStateRootCall) DoAndReturn(f func() (types.Hash32, error)) *MockvmStateGetStateRootCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Revert mocks base method.
func (m *MockvmState) Revert(arg0 types.LayerID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Revert", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Revert indicates an expected call of Revert.
func (mr *MockvmStateMockRecorder) Revert(arg0 any) *MockvmStateRevertCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Revert", reflect.TypeOf((*MockvmState)(nil).Revert), arg0)
	return &MockvmStateRevertCall{Call: call}
}

// MockvmStateRevertCall wrap *gomock.Call
type MockvmStateRevertCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockvmStateRevertCall) Return(arg0 error) *MockvmStateRevertCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockvmStateRevertCall) Do(f func(types.LayerID) error) *MockvmStateRevertCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockvmStateRevertCall) DoAndReturn(f func(types.LayerID) error) *MockvmStateRevertCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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

// CurrentLayer mocks base method.
func (m *MocklayerClock) CurrentLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// CurrentLayer indicates an expected call of CurrentLayer.
func (mr *MocklayerClockMockRecorder) CurrentLayer() *MocklayerClockCurrentLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentLayer", reflect.TypeOf((*MocklayerClock)(nil).CurrentLayer))
	return &MocklayerClockCurrentLayerCall{Call: call}
}

// MocklayerClockCurrentLayerCall wrap *gomock.Call
type MocklayerClockCurrentLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocklayerClockCurrentLayerCall) Return(arg0 types.LayerID) *MocklayerClockCurrentLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocklayerClockCurrentLayerCall) Do(f func() types.LayerID) *MocklayerClockCurrentLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocklayerClockCurrentLayerCall) DoAndReturn(f func() types.LayerID) *MocklayerClockCurrentLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
