// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	log "github.com/spacemeshos/go-spacemesh/log"
	system "github.com/spacemeshos/go-spacemesh/system"
	types0 "github.com/spacemeshos/go-spacemesh/txs/types"
)

// MocktxGetter is a mock of txGetter interface.
type MocktxGetter struct {
	ctrl     *gomock.Controller
	recorder *MocktxGetterMockRecorder
}

// MocktxGetterMockRecorder is the mock recorder for MocktxGetter.
type MocktxGetterMockRecorder struct {
	mock *MocktxGetter
}

// NewMocktxGetter creates a new mock instance.
func NewMocktxGetter(ctrl *gomock.Controller) *MocktxGetter {
	mock := &MocktxGetter{ctrl: ctrl}
	mock.recorder = &MocktxGetterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocktxGetter) EXPECT() *MocktxGetterMockRecorder {
	return m.recorder
}

// GetMeshTransaction mocks base method.
func (m *MocktxGetter) GetMeshTransaction(arg0 types.TransactionID) (*types.MeshTransaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMeshTransaction", arg0)
	ret0, _ := ret[0].(*types.MeshTransaction)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetMeshTransaction indicates an expected call of GetMeshTransaction.
func (mr *MocktxGetterMockRecorder) GetMeshTransaction(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMeshTransaction", reflect.TypeOf((*MocktxGetter)(nil).GetMeshTransaction), arg0)
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

// AddToCache mocks base method.
func (m *MockconservativeState) AddToCache(arg0 context.Context, arg1 *types.Transaction) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddToCache", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddToCache indicates an expected call of AddToCache.
func (mr *MockconservativeStateMockRecorder) AddToCache(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddToCache", reflect.TypeOf((*MockconservativeState)(nil).AddToCache), arg0, arg1)
}

// AddToDB mocks base method.
func (m *MockconservativeState) AddToDB(arg0 *types.Transaction) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddToDB", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddToDB indicates an expected call of AddToDB.
func (mr *MockconservativeStateMockRecorder) AddToDB(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddToDB", reflect.TypeOf((*MockconservativeState)(nil).AddToDB), arg0)
}

// GetMeshTransaction mocks base method.
func (m *MockconservativeState) GetMeshTransaction(arg0 types.TransactionID) (*types.MeshTransaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMeshTransaction", arg0)
	ret0, _ := ret[0].(*types.MeshTransaction)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetMeshTransaction indicates an expected call of GetMeshTransaction.
func (mr *MockconservativeStateMockRecorder) GetMeshTransaction(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMeshTransaction", reflect.TypeOf((*MockconservativeState)(nil).GetMeshTransaction), arg0)
}

// HasTx mocks base method.
func (m *MockconservativeState) HasTx(arg0 types.TransactionID) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasTx", arg0)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// HasTx indicates an expected call of HasTx.
func (mr *MockconservativeStateMockRecorder) HasTx(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasTx", reflect.TypeOf((*MockconservativeState)(nil).HasTx), arg0)
}

// Validation mocks base method.
func (m *MockconservativeState) Validation(arg0 types.RawTx) system.ValidationRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validation", arg0)
	ret0, _ := ret[0].(system.ValidationRequest)
	return ret0
}

// Validation indicates an expected call of Validation.
func (mr *MockconservativeStateMockRecorder) Validation(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validation", reflect.TypeOf((*MockconservativeState)(nil).Validation), arg0)
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
func (m *MockvmState) Apply(arg0 vm.ApplyContext, arg1 []types.ExecutableTx, arg2 []types.AnyReward) ([]types.Transaction, []types.TransactionWithResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Apply", arg0, arg1, arg2)
	ret0, _ := ret[0].([]types.Transaction)
	ret1, _ := ret[1].([]types.TransactionWithResult)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// Apply indicates an expected call of Apply.
func (mr *MockvmStateMockRecorder) Apply(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Apply", reflect.TypeOf((*MockvmState)(nil).Apply), arg0, arg1, arg2)
}

// GetAllAccounts mocks base method.
func (m *MockvmState) GetAllAccounts() ([]*types.Account, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllAccounts")
	ret0, _ := ret[0].([]*types.Account)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllAccounts indicates an expected call of GetAllAccounts.
func (mr *MockvmStateMockRecorder) GetAllAccounts() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllAccounts", reflect.TypeOf((*MockvmState)(nil).GetAllAccounts))
}

// GetBalance mocks base method.
func (m *MockvmState) GetBalance(arg0 types.Address) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBalance", arg0)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBalance indicates an expected call of GetBalance.
func (mr *MockvmStateMockRecorder) GetBalance(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBalance", reflect.TypeOf((*MockvmState)(nil).GetBalance), arg0)
}

// GetLayerApplied mocks base method.
func (m *MockvmState) GetLayerApplied(arg0 types.TransactionID) (types.LayerID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayerApplied", arg0)
	ret0, _ := ret[0].(types.LayerID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLayerApplied indicates an expected call of GetLayerApplied.
func (mr *MockvmStateMockRecorder) GetLayerApplied(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerApplied", reflect.TypeOf((*MockvmState)(nil).GetLayerApplied), arg0)
}

// GetLayerStateRoot mocks base method.
func (m *MockvmState) GetLayerStateRoot(arg0 types.LayerID) (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayerStateRoot", arg0)
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLayerStateRoot indicates an expected call of GetLayerStateRoot.
func (mr *MockvmStateMockRecorder) GetLayerStateRoot(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerStateRoot", reflect.TypeOf((*MockvmState)(nil).GetLayerStateRoot), arg0)
}

// GetNonce mocks base method.
func (m *MockvmState) GetNonce(arg0 types.Address) (types.Nonce, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNonce", arg0)
	ret0, _ := ret[0].(types.Nonce)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetNonce indicates an expected call of GetNonce.
func (mr *MockvmStateMockRecorder) GetNonce(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNonce", reflect.TypeOf((*MockvmState)(nil).GetNonce), arg0)
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
func (mr *MockvmStateMockRecorder) GetStateRoot() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStateRoot", reflect.TypeOf((*MockvmState)(nil).GetStateRoot))
}

// Revert mocks base method.
func (m *MockvmState) Revert(arg0 types.LayerID) (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Revert", arg0)
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Revert indicates an expected call of Revert.
func (mr *MockvmStateMockRecorder) Revert(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Revert", reflect.TypeOf((*MockvmState)(nil).Revert), arg0)
}

// Validation mocks base method.
func (m *MockvmState) Validation(arg0 types.RawTx) system.ValidationRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validation", arg0)
	ret0, _ := ret[0].(system.ValidationRequest)
	return ret0
}

// Validation indicates an expected call of Validation.
func (mr *MockvmStateMockRecorder) Validation(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validation", reflect.TypeOf((*MockvmState)(nil).Validation), arg0)
}

// MockconStateCache is a mock of conStateCache interface.
type MockconStateCache struct {
	ctrl     *gomock.Controller
	recorder *MockconStateCacheMockRecorder
}

// MockconStateCacheMockRecorder is the mock recorder for MockconStateCache.
type MockconStateCacheMockRecorder struct {
	mock *MockconStateCache
}

// NewMockconStateCache creates a new mock instance.
func NewMockconStateCache(ctrl *gomock.Controller) *MockconStateCache {
	mock := &MockconStateCache{ctrl: ctrl}
	mock.recorder = &MockconStateCacheMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockconStateCache) EXPECT() *MockconStateCacheMockRecorder {
	return m.recorder
}

// GetMempool mocks base method.
func (m *MockconStateCache) GetMempool(arg0 log.Log) map[types.Address][]*types0.NanoTX {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMempool", arg0)
	ret0, _ := ret[0].(map[types.Address][]*types0.NanoTX)
	return ret0
}

// GetMempool indicates an expected call of GetMempool.
func (mr *MockconStateCacheMockRecorder) GetMempool(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMempool", reflect.TypeOf((*MockconStateCache)(nil).GetMempool), arg0)
}
