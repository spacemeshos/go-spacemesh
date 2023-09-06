// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

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
func (mr *MockconservativeStateMockRecorder) LinkTXsWithBlock(arg0, arg1, arg2 interface{}) *conservativeStateLinkTXsWithBlockCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LinkTXsWithBlock", reflect.TypeOf((*MockconservativeState)(nil).LinkTXsWithBlock), arg0, arg1, arg2)
	return &conservativeStateLinkTXsWithBlockCall{Call: call}
}

// conservativeStateLinkTXsWithBlockCall wrap *gomock.Call
type conservativeStateLinkTXsWithBlockCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateLinkTXsWithBlockCall) Return(arg0 error) *conservativeStateLinkTXsWithBlockCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateLinkTXsWithBlockCall) Do(f func(types.LayerID, types.BlockID, []types.TransactionID) error) *conservativeStateLinkTXsWithBlockCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateLinkTXsWithBlockCall) DoAndReturn(f func(types.LayerID, types.BlockID, []types.TransactionID) error) *conservativeStateLinkTXsWithBlockCall {
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
func (mr *MockconservativeStateMockRecorder) LinkTXsWithProposal(arg0, arg1, arg2 interface{}) *conservativeStateLinkTXsWithProposalCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LinkTXsWithProposal", reflect.TypeOf((*MockconservativeState)(nil).LinkTXsWithProposal), arg0, arg1, arg2)
	return &conservativeStateLinkTXsWithProposalCall{Call: call}
}

// conservativeStateLinkTXsWithProposalCall wrap *gomock.Call
type conservativeStateLinkTXsWithProposalCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateLinkTXsWithProposalCall) Return(arg0 error) *conservativeStateLinkTXsWithProposalCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateLinkTXsWithProposalCall) Do(f func(types.LayerID, types.ProposalID, []types.TransactionID) error) *conservativeStateLinkTXsWithProposalCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateLinkTXsWithProposalCall) DoAndReturn(f func(types.LayerID, types.ProposalID, []types.TransactionID) error) *conservativeStateLinkTXsWithProposalCall {
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
func (mr *MockconservativeStateMockRecorder) RevertCache(arg0 interface{}) *conservativeStateRevertCacheCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RevertCache", reflect.TypeOf((*MockconservativeState)(nil).RevertCache), arg0)
	return &conservativeStateRevertCacheCall{Call: call}
}

// conservativeStateRevertCacheCall wrap *gomock.Call
type conservativeStateRevertCacheCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateRevertCacheCall) Return(arg0 error) *conservativeStateRevertCacheCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateRevertCacheCall) Do(f func(types.LayerID) error) *conservativeStateRevertCacheCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateRevertCacheCall) DoAndReturn(f func(types.LayerID) error) *conservativeStateRevertCacheCall {
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
func (mr *MockconservativeStateMockRecorder) UpdateCache(arg0, arg1, arg2, arg3, arg4 interface{}) *conservativeStateUpdateCacheCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateCache", reflect.TypeOf((*MockconservativeState)(nil).UpdateCache), arg0, arg1, arg2, arg3, arg4)
	return &conservativeStateUpdateCacheCall{Call: call}
}

// conservativeStateUpdateCacheCall wrap *gomock.Call
type conservativeStateUpdateCacheCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateUpdateCacheCall) Return(arg0 error) *conservativeStateUpdateCacheCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateUpdateCacheCall) Do(f func(context.Context, types.LayerID, types.BlockID, []types.TransactionWithResult, []types.Transaction) error) *conservativeStateUpdateCacheCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateUpdateCacheCall) DoAndReturn(f func(context.Context, types.LayerID, types.BlockID, []types.TransactionWithResult, []types.Transaction) error) *conservativeStateUpdateCacheCall {
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
func (mr *MockvmStateMockRecorder) Apply(arg0, arg1, arg2 interface{}) *vmStateApplyCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Apply", reflect.TypeOf((*MockvmState)(nil).Apply), arg0, arg1, arg2)
	return &vmStateApplyCall{Call: call}
}

// vmStateApplyCall wrap *gomock.Call
type vmStateApplyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateApplyCall) Return(arg0 []types.Transaction, arg1 []types.TransactionWithResult, arg2 error) *vmStateApplyCall {
	c.Call = c.Call.Return(arg0, arg1, arg2)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateApplyCall) Do(f func(vm.ApplyContext, []types.Transaction, []types.CoinbaseReward) ([]types.Transaction, []types.TransactionWithResult, error)) *vmStateApplyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateApplyCall) DoAndReturn(f func(vm.ApplyContext, []types.Transaction, []types.CoinbaseReward) ([]types.Transaction, []types.TransactionWithResult, error)) *vmStateApplyCall {
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
func (mr *MockvmStateMockRecorder) GetStateRoot() *vmStateGetStateRootCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStateRoot", reflect.TypeOf((*MockvmState)(nil).GetStateRoot))
	return &vmStateGetStateRootCall{Call: call}
}

// vmStateGetStateRootCall wrap *gomock.Call
type vmStateGetStateRootCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateGetStateRootCall) Return(arg0 types.Hash32, arg1 error) *vmStateGetStateRootCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateGetStateRootCall) Do(f func() (types.Hash32, error)) *vmStateGetStateRootCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateGetStateRootCall) DoAndReturn(f func() (types.Hash32, error)) *vmStateGetStateRootCall {
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
func (mr *MockvmStateMockRecorder) Revert(arg0 interface{}) *vmStateRevertCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Revert", reflect.TypeOf((*MockvmState)(nil).Revert), arg0)
	return &vmStateRevertCall{Call: call}
}

// vmStateRevertCall wrap *gomock.Call
type vmStateRevertCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateRevertCall) Return(arg0 error) *vmStateRevertCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateRevertCall) Do(f func(types.LayerID) error) *vmStateRevertCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateRevertCall) DoAndReturn(f func(types.LayerID) error) *vmStateRevertCall {
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
func (mr *MocklayerClockMockRecorder) CurrentLayer() *layerClockCurrentLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentLayer", reflect.TypeOf((*MocklayerClock)(nil).CurrentLayer))
	return &layerClockCurrentLayerCall{Call: call}
}

// layerClockCurrentLayerCall wrap *gomock.Call
type layerClockCurrentLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *layerClockCurrentLayerCall) Return(arg0 types.LayerID) *layerClockCurrentLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *layerClockCurrentLayerCall) Do(f func() types.LayerID) *layerClockCurrentLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *layerClockCurrentLayerCall) DoAndReturn(f func() types.LayerID) *layerClockCurrentLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
