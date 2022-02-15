// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
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

// AddTxToMempool mocks base method.
func (m *MockconservativeState) AddTxToMemPool(tx *types.Transaction, checkValidity bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddTxToMemPool", tx, checkValidity)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddTxToMempool indicates an expected call of AddTxToMempool.
func (mr *MockconservativeStateMockRecorder) AddTxToMempool(tx, checkValidity interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddTxToMemPool", reflect.TypeOf((*MockconservativeState)(nil).AddTxToMemPool), tx, checkValidity)
}

// AddressExists mocks base method.
func (m *MockconservativeState) AddressExists(addr types.Address) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddressExists", addr)
	ret0, _ := ret[0].(bool)
	return ret0
}

// AddressExists indicates an expected call of AddressExists.
func (mr *MockconservativeStateMockRecorder) AddressExists(addr interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddressExists", reflect.TypeOf((*MockconservativeState)(nil).AddressExists), addr)
}

// MocksvmState is a mock of svmState interface.
type MocksvmState struct {
	ctrl     *gomock.Controller
	recorder *MocksvmStateMockRecorder
}

// MocksvmStateMockRecorder is the mock recorder for MocksvmState.
type MocksvmStateMockRecorder struct {
	mock *MocksvmState
}

// NewMocksvmState creates a new mock instance.
func NewMocksvmState(ctrl *gomock.Controller) *MocksvmState {
	mock := &MocksvmState{ctrl: ctrl}
	mock.recorder = &MocksvmStateMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocksvmState) EXPECT() *MocksvmStateMockRecorder {
	return m.recorder
}

// AddressExists mocks base method.
func (m *MocksvmState) AddressExists(arg0 types.Address) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddressExists", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// AddressExists indicates an expected call of AddressExists.
func (mr *MocksvmStateMockRecorder) AddressExists(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddressExists", reflect.TypeOf((*MocksvmState)(nil).AddressExists), arg0)
}

// ApplyLayer mocks base method.
func (m *MocksvmState) ApplyLayer(arg0 types.LayerID, arg1 []*types.Transaction, arg2 map[types.Address]uint64) ([]*types.Transaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ApplyLayer", arg0, arg1, arg2)
	ret0, _ := ret[0].([]*types.Transaction)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ApplyLayer indicates an expected call of ApplyLayer.
func (mr *MocksvmStateMockRecorder) ApplyLayer(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ApplyLayer", reflect.TypeOf((*MocksvmState)(nil).ApplyLayer), arg0, arg1, arg2)
}

// GetAllAccounts mocks base method.
func (m *MocksvmState) GetAllAccounts() (*types.MultipleAccountsState, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllAccounts")
	ret0, _ := ret[0].(*types.MultipleAccountsState)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllAccounts indicates an expected call of GetAllAccounts.
func (mr *MocksvmStateMockRecorder) GetAllAccounts() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllAccounts", reflect.TypeOf((*MocksvmState)(nil).GetAllAccounts))
}

// GetBalance mocks base method.
func (m *MocksvmState) GetBalance(arg0 types.Address) uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBalance", arg0)
	ret0, _ := ret[0].(uint64)
	return ret0
}

// GetBalance indicates an expected call of GetBalance.
func (mr *MocksvmStateMockRecorder) GetBalance(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBalance", reflect.TypeOf((*MocksvmState)(nil).GetBalance), arg0)
}

// GetLayerApplied mocks base method.
func (m *MocksvmState) GetLayerApplied(arg0 types.TransactionID) *types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayerApplied", arg0)
	ret0, _ := ret[0].(*types.LayerID)
	return ret0
}

// GetLayerApplied indicates an expected call of GetLayerApplied.
func (mr *MocksvmStateMockRecorder) GetLayerApplied(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerApplied", reflect.TypeOf((*MocksvmState)(nil).GetLayerApplied), arg0)
}

// GetLayerStateRoot mocks base method.
func (m *MocksvmState) GetLayerStateRoot(arg0 types.LayerID) (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayerStateRoot", arg0)
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLayerStateRoot indicates an expected call of GetLayerStateRoot.
func (mr *MocksvmStateMockRecorder) GetLayerStateRoot(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerStateRoot", reflect.TypeOf((*MocksvmState)(nil).GetLayerStateRoot), arg0)
}

// GetNonce mocks base method.
func (m *MocksvmState) GetNonce(arg0 types.Address) uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNonce", arg0)
	ret0, _ := ret[0].(uint64)
	return ret0
}

// GetNonce indicates an expected call of GetNonce.
func (mr *MocksvmStateMockRecorder) GetNonce(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNonce", reflect.TypeOf((*MocksvmState)(nil).GetNonce), arg0)
}

// GetStateRoot mocks base method.
func (m *MocksvmState) GetStateRoot() types.Hash32 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetStateRoot")
	ret0, _ := ret[0].(types.Hash32)
	return ret0
}

// GetStateRoot indicates an expected call of GetStateRoot.
func (mr *MocksvmStateMockRecorder) GetStateRoot() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStateRoot", reflect.TypeOf((*MocksvmState)(nil).GetStateRoot))
}

// Rewind mocks base method.
func (m *MocksvmState) Rewind(arg0 types.LayerID) (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Rewind", arg0)
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Rewind indicates an expected call of Rewind.
func (mr *MocksvmStateMockRecorder) Rewind(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Rewind", reflect.TypeOf((*MocksvmState)(nil).Rewind), arg0)
}
