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
	gomock "go.uber.org/mock/gomock"
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

// CompleteHare mocks base method.
func (m *MocklayerPatrol) CompleteHare(arg0 types.LayerID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "CompleteHare", arg0)
}

// CompleteHare indicates an expected call of CompleteHare.
func (mr *MocklayerPatrolMockRecorder) CompleteHare(arg0 any) *MocklayerPatrolCompleteHareCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CompleteHare", reflect.TypeOf((*MocklayerPatrol)(nil).CompleteHare), arg0)
	return &MocklayerPatrolCompleteHareCall{Call: call}
}

// MocklayerPatrolCompleteHareCall wrap *gomock.Call
type MocklayerPatrolCompleteHareCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocklayerPatrolCompleteHareCall) Return() *MocklayerPatrolCompleteHareCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocklayerPatrolCompleteHareCall) Do(f func(types.LayerID)) *MocklayerPatrolCompleteHareCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocklayerPatrolCompleteHareCall) DoAndReturn(f func(types.LayerID)) *MocklayerPatrolCompleteHareCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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

// AddBlockWithTXs mocks base method.
func (m *MockmeshProvider) AddBlockWithTXs(arg0 context.Context, arg1 *types.Block) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddBlockWithTXs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddBlockWithTXs indicates an expected call of AddBlockWithTXs.
func (mr *MockmeshProviderMockRecorder) AddBlockWithTXs(arg0, arg1 any) *MockmeshProviderAddBlockWithTXsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddBlockWithTXs", reflect.TypeOf((*MockmeshProvider)(nil).AddBlockWithTXs), arg0, arg1)
	return &MockmeshProviderAddBlockWithTXsCall{Call: call}
}

// MockmeshProviderAddBlockWithTXsCall wrap *gomock.Call
type MockmeshProviderAddBlockWithTXsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshProviderAddBlockWithTXsCall) Return(arg0 error) *MockmeshProviderAddBlockWithTXsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshProviderAddBlockWithTXsCall) Do(f func(context.Context, *types.Block) error) *MockmeshProviderAddBlockWithTXsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshProviderAddBlockWithTXsCall) DoAndReturn(f func(context.Context, *types.Block) error) *MockmeshProviderAddBlockWithTXsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// ProcessLayerPerHareOutput mocks base method.
func (m *MockmeshProvider) ProcessLayerPerHareOutput(arg0 context.Context, arg1 types.LayerID, arg2 types.BlockID, arg3 bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessLayerPerHareOutput", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// ProcessLayerPerHareOutput indicates an expected call of ProcessLayerPerHareOutput.
func (mr *MockmeshProviderMockRecorder) ProcessLayerPerHareOutput(arg0, arg1, arg2, arg3 any) *MockmeshProviderProcessLayerPerHareOutputCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessLayerPerHareOutput", reflect.TypeOf((*MockmeshProvider)(nil).ProcessLayerPerHareOutput), arg0, arg1, arg2, arg3)
	return &MockmeshProviderProcessLayerPerHareOutputCall{Call: call}
}

// MockmeshProviderProcessLayerPerHareOutputCall wrap *gomock.Call
type MockmeshProviderProcessLayerPerHareOutputCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshProviderProcessLayerPerHareOutputCall) Return(arg0 error) *MockmeshProviderProcessLayerPerHareOutputCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshProviderProcessLayerPerHareOutputCall) Do(f func(context.Context, types.LayerID, types.BlockID, bool) error) *MockmeshProviderProcessLayerPerHareOutputCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshProviderProcessLayerPerHareOutputCall) DoAndReturn(f func(context.Context, types.LayerID, types.BlockID, bool) error) *MockmeshProviderProcessLayerPerHareOutputCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// ProcessedLayer mocks base method.
func (m *MockmeshProvider) ProcessedLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessedLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// ProcessedLayer indicates an expected call of ProcessedLayer.
func (mr *MockmeshProviderMockRecorder) ProcessedLayer() *MockmeshProviderProcessedLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessedLayer", reflect.TypeOf((*MockmeshProvider)(nil).ProcessedLayer))
	return &MockmeshProviderProcessedLayerCall{Call: call}
}

// MockmeshProviderProcessedLayerCall wrap *gomock.Call
type MockmeshProviderProcessedLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshProviderProcessedLayerCall) Return(arg0 types.LayerID) *MockmeshProviderProcessedLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshProviderProcessedLayerCall) Do(f func() types.LayerID) *MockmeshProviderProcessedLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshProviderProcessedLayerCall) DoAndReturn(f func() types.LayerID) *MockmeshProviderProcessedLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Mockexecutor is a mock of executor interface.
type Mockexecutor struct {
	ctrl     *gomock.Controller
	recorder *MockexecutorMockRecorder
}

// MockexecutorMockRecorder is the mock recorder for Mockexecutor.
type MockexecutorMockRecorder struct {
	mock *Mockexecutor
}

// NewMockexecutor creates a new mock instance.
func NewMockexecutor(ctrl *gomock.Controller) *Mockexecutor {
	mock := &Mockexecutor{ctrl: ctrl}
	mock.recorder = &MockexecutorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockexecutor) EXPECT() *MockexecutorMockRecorder {
	return m.recorder
}

// ExecuteOptimistic mocks base method.
func (m *Mockexecutor) ExecuteOptimistic(arg0 context.Context, arg1 types.LayerID, arg2 uint64, arg3 []types.AnyReward, arg4 []types.TransactionID) (*types.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ExecuteOptimistic", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(*types.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ExecuteOptimistic indicates an expected call of ExecuteOptimistic.
func (mr *MockexecutorMockRecorder) ExecuteOptimistic(arg0, arg1, arg2, arg3, arg4 any) *MockexecutorExecuteOptimisticCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ExecuteOptimistic", reflect.TypeOf((*Mockexecutor)(nil).ExecuteOptimistic), arg0, arg1, arg2, arg3, arg4)
	return &MockexecutorExecuteOptimisticCall{Call: call}
}

// MockexecutorExecuteOptimisticCall wrap *gomock.Call
type MockexecutorExecuteOptimisticCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockexecutorExecuteOptimisticCall) Return(arg0 *types.Block, arg1 error) *MockexecutorExecuteOptimisticCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockexecutorExecuteOptimisticCall) Do(f func(context.Context, types.LayerID, uint64, []types.AnyReward, []types.TransactionID) (*types.Block, error)) *MockexecutorExecuteOptimisticCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockexecutorExecuteOptimisticCall) DoAndReturn(f func(context.Context, types.LayerID, uint64, []types.AnyReward, []types.TransactionID) (*types.Block, error)) *MockexecutorExecuteOptimisticCall {
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

// AwaitLayer mocks base method.
func (m *MocklayerClock) AwaitLayer(layerID types.LayerID) <-chan struct{} {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AwaitLayer", layerID)
	ret0, _ := ret[0].(<-chan struct{})
	return ret0
}

// AwaitLayer indicates an expected call of AwaitLayer.
func (mr *MocklayerClockMockRecorder) AwaitLayer(layerID any) *MocklayerClockAwaitLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AwaitLayer", reflect.TypeOf((*MocklayerClock)(nil).AwaitLayer), layerID)
	return &MocklayerClockAwaitLayerCall{Call: call}
}

// MocklayerClockAwaitLayerCall wrap *gomock.Call
type MocklayerClockAwaitLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocklayerClockAwaitLayerCall) Return(arg0 <-chan struct{}) *MocklayerClockAwaitLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocklayerClockAwaitLayerCall) Do(f func(types.LayerID) <-chan struct{}) *MocklayerClockAwaitLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocklayerClockAwaitLayerCall) DoAndReturn(f func(types.LayerID) <-chan struct{}) *MocklayerClockAwaitLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (m *Mockcertifier) CertifyIfEligible(arg0 context.Context, arg1 types.LayerID, arg2 types.BlockID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CertifyIfEligible", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// CertifyIfEligible indicates an expected call of CertifyIfEligible.
func (mr *MockcertifierMockRecorder) CertifyIfEligible(arg0, arg1, arg2 any) *MockcertifierCertifyIfEligibleCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CertifyIfEligible", reflect.TypeOf((*Mockcertifier)(nil).CertifyIfEligible), arg0, arg1, arg2)
	return &MockcertifierCertifyIfEligibleCall{Call: call}
}

// MockcertifierCertifyIfEligibleCall wrap *gomock.Call
type MockcertifierCertifyIfEligibleCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockcertifierCertifyIfEligibleCall) Return(arg0 error) *MockcertifierCertifyIfEligibleCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockcertifierCertifyIfEligibleCall) Do(f func(context.Context, types.LayerID, types.BlockID) error) *MockcertifierCertifyIfEligibleCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockcertifierCertifyIfEligibleCall) DoAndReturn(f func(context.Context, types.LayerID, types.BlockID) error) *MockcertifierCertifyIfEligibleCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RegisterForCert mocks base method.
func (m *Mockcertifier) RegisterForCert(arg0 context.Context, arg1 types.LayerID, arg2 types.BlockID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterForCert", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterForCert indicates an expected call of RegisterForCert.
func (mr *MockcertifierMockRecorder) RegisterForCert(arg0, arg1, arg2 any) *MockcertifierRegisterForCertCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterForCert", reflect.TypeOf((*Mockcertifier)(nil).RegisterForCert), arg0, arg1, arg2)
	return &MockcertifierRegisterForCertCall{Call: call}
}

// MockcertifierRegisterForCertCall wrap *gomock.Call
type MockcertifierRegisterForCertCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockcertifierRegisterForCertCall) Return(arg0 error) *MockcertifierRegisterForCertCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockcertifierRegisterForCertCall) Do(f func(context.Context, types.LayerID, types.BlockID) error) *MockcertifierRegisterForCertCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockcertifierRegisterForCertCall) DoAndReturn(f func(context.Context, types.LayerID, types.BlockID) error) *MockcertifierRegisterForCertCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MocktortoiseProvider is a mock of tortoiseProvider interface.
type MocktortoiseProvider struct {
	ctrl     *gomock.Controller
	recorder *MocktortoiseProviderMockRecorder
}

// MocktortoiseProviderMockRecorder is the mock recorder for MocktortoiseProvider.
type MocktortoiseProviderMockRecorder struct {
	mock *MocktortoiseProvider
}

// NewMocktortoiseProvider creates a new mock instance.
func NewMocktortoiseProvider(ctrl *gomock.Controller) *MocktortoiseProvider {
	mock := &MocktortoiseProvider{ctrl: ctrl}
	mock.recorder = &MocktortoiseProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocktortoiseProvider) EXPECT() *MocktortoiseProviderMockRecorder {
	return m.recorder
}

// GetMissingActiveSet mocks base method.
func (m *MocktortoiseProvider) GetMissingActiveSet(arg0 types.EpochID, arg1 []types.ATXID) []types.ATXID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMissingActiveSet", arg0, arg1)
	ret0, _ := ret[0].([]types.ATXID)
	return ret0
}

// GetMissingActiveSet indicates an expected call of GetMissingActiveSet.
func (mr *MocktortoiseProviderMockRecorder) GetMissingActiveSet(arg0, arg1 any) *MocktortoiseProviderGetMissingActiveSetCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMissingActiveSet", reflect.TypeOf((*MocktortoiseProvider)(nil).GetMissingActiveSet), arg0, arg1)
	return &MocktortoiseProviderGetMissingActiveSetCall{Call: call}
}

// MocktortoiseProviderGetMissingActiveSetCall wrap *gomock.Call
type MocktortoiseProviderGetMissingActiveSetCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocktortoiseProviderGetMissingActiveSetCall) Return(arg0 []types.ATXID) *MocktortoiseProviderGetMissingActiveSetCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocktortoiseProviderGetMissingActiveSetCall) Do(f func(types.EpochID, []types.ATXID) []types.ATXID) *MocktortoiseProviderGetMissingActiveSetCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocktortoiseProviderGetMissingActiveSetCall) DoAndReturn(f func(types.EpochID, []types.ATXID) []types.ATXID) *MocktortoiseProviderGetMissingActiveSetCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
