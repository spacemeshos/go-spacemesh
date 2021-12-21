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

// Mockstate is a mock of state interface.
type Mockstate struct {
	ctrl     *gomock.Controller
	recorder *MockstateMockRecorder
}

// MockstateMockRecorder is the mock recorder for Mockstate.
type MockstateMockRecorder struct {
	mock *Mockstate
}

// NewMockstate creates a new mock instance.
func NewMockstate(ctrl *gomock.Controller) *Mockstate {
	mock := &Mockstate{ctrl: ctrl}
	mock.recorder = &MockstateMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockstate) EXPECT() *MockstateMockRecorder {
	return m.recorder
}

// AddressExists mocks base method.
func (m *Mockstate) AddressExists(arg0 types.Address) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddressExists", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// AddressExists indicates an expected call of AddressExists.
func (mr *MockstateMockRecorder) AddressExists(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddressExists", reflect.TypeOf((*Mockstate)(nil).AddressExists), arg0)
}

// ApplyLayer mocks base method.
func (m *Mockstate) ApplyLayer(layer types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ApplyLayer", layer, txs, rewards)
	ret0, _ := ret[0].([]*types.Transaction)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ApplyLayer indicates an expected call of ApplyLayer.
func (mr *MockstateMockRecorder) ApplyLayer(layer, txs, rewards interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ApplyLayer", reflect.TypeOf((*Mockstate)(nil).ApplyLayer), layer, txs, rewards)
}

// GetAllAccounts mocks base method.
func (m *Mockstate) GetAllAccounts() (*types.MultipleAccountsState, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllAccounts")
	ret0, _ := ret[0].(*types.MultipleAccountsState)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllAccounts indicates an expected call of GetAllAccounts.
func (mr *MockstateMockRecorder) GetAllAccounts() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllAccounts", reflect.TypeOf((*Mockstate)(nil).GetAllAccounts))
}

// GetBalance mocks base method.
func (m *Mockstate) GetBalance(arg0 types.Address) uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBalance", arg0)
	ret0, _ := ret[0].(uint64)
	return ret0
}

// GetBalance indicates an expected call of GetBalance.
func (mr *MockstateMockRecorder) GetBalance(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBalance", reflect.TypeOf((*Mockstate)(nil).GetBalance), arg0)
}

// GetLayerApplied mocks base method.
func (m *Mockstate) GetLayerApplied(arg0 types.TransactionID) *types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayerApplied", arg0)
	ret0, _ := ret[0].(*types.LayerID)
	return ret0
}

// GetLayerApplied indicates an expected call of GetLayerApplied.
func (mr *MockstateMockRecorder) GetLayerApplied(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerApplied", reflect.TypeOf((*Mockstate)(nil).GetLayerApplied), arg0)
}

// GetLayerStateRoot mocks base method.
func (m *Mockstate) GetLayerStateRoot(arg0 types.LayerID) (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayerStateRoot", arg0)
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLayerStateRoot indicates an expected call of GetLayerStateRoot.
func (mr *MockstateMockRecorder) GetLayerStateRoot(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerStateRoot", reflect.TypeOf((*Mockstate)(nil).GetLayerStateRoot), arg0)
}

// GetNonce mocks base method.
func (m *Mockstate) GetNonce(arg0 types.Address) uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNonce", arg0)
	ret0, _ := ret[0].(uint64)
	return ret0
}

// GetNonce indicates an expected call of GetNonce.
func (mr *MockstateMockRecorder) GetNonce(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNonce", reflect.TypeOf((*Mockstate)(nil).GetNonce), arg0)
}

// GetStateRoot mocks base method.
func (m *Mockstate) GetStateRoot() types.Hash32 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetStateRoot")
	ret0, _ := ret[0].(types.Hash32)
	return ret0
}

// GetStateRoot indicates an expected call of GetStateRoot.
func (mr *MockstateMockRecorder) GetStateRoot() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStateRoot", reflect.TypeOf((*Mockstate)(nil).GetStateRoot))
}

// Rewind mocks base method.
func (m *Mockstate) Rewind(layer types.LayerID) (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Rewind", layer)
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Rewind indicates an expected call of Rewind.
func (mr *MockstateMockRecorder) Rewind(layer interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Rewind", reflect.TypeOf((*Mockstate)(nil).Rewind), layer)
}

// AddTxToPool mocks base method.
func (m *Mockstate) AddTxToPool(tx *types.Transaction) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddTxToPool", tx)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddTxToPool indicates an expected call of AddTxToPool.
func (mr *MockstateMockRecorder) AddTxToPool(tx interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddTxToPool", reflect.TypeOf((*Mockstate)(nil).AddTxToPool), tx)
}

// ValidateNonceAndBalance mocks base method.
func (m *Mockstate) ValidateNonceAndBalance(arg0 *types.Transaction) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ValidateNonceAndBalance", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// ValidateNonceAndBalance indicates an expected call of ValidateNonceAndBalance.
func (mr *MockstateMockRecorder) ValidateNonceAndBalance(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ValidateNonceAndBalance", reflect.TypeOf((*Mockstate)(nil).ValidateNonceAndBalance), arg0)
}

// Mocktortoise is a mock of tortoise interface.
type Mocktortoise struct {
	ctrl     *gomock.Controller
	recorder *MocktortoiseMockRecorder
}

// MocktortoiseMockRecorder is the mock recorder for Mocktortoise.
type MocktortoiseMockRecorder struct {
	mock *Mocktortoise
}

// NewMocktortoise creates a new mock instance.
func NewMocktortoise(ctrl *gomock.Controller) *Mocktortoise {
	mock := &Mocktortoise{ctrl: ctrl}
	mock.recorder = &MocktortoiseMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mocktortoise) EXPECT() *MocktortoiseMockRecorder {
	return m.recorder
}

// HandleIncomingLayer mocks base method.
func (m *Mocktortoise) HandleIncomingLayer(arg0 context.Context, arg1 types.LayerID) (types.LayerID, types.LayerID, bool) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandleIncomingLayer", arg0, arg1)
	ret0, _ := ret[0].(types.LayerID)
	ret1, _ := ret[1].(types.LayerID)
	ret2, _ := ret[2].(bool)
	return ret0, ret1, ret2
}

// HandleIncomingLayer indicates an expected call of HandleIncomingLayer.
func (mr *MocktortoiseMockRecorder) HandleIncomingLayer(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleIncomingLayer", reflect.TypeOf((*Mocktortoise)(nil).HandleIncomingLayer), arg0, arg1)
}
