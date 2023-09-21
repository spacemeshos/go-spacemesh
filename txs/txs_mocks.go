// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go
//
// Generated by this command:
//
//	mockgen -typed -package=txs -destination=./txs_mocks.go -source=./interface.go
//
// Package txs is a generated GoMock package.
package txs

import (
	context "context"
	reflect "reflect"
	time "time"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	log "github.com/spacemeshos/go-spacemesh/log"
	system "github.com/spacemeshos/go-spacemesh/system"
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

// AddToCache mocks base method.
func (m *MockconservativeState) AddToCache(arg0 context.Context, arg1 *types.Transaction, arg2 time.Time) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddToCache", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddToCache indicates an expected call of AddToCache.
func (mr *MockconservativeStateMockRecorder) AddToCache(arg0, arg1, arg2 any) *conservativeStateAddToCacheCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddToCache", reflect.TypeOf((*MockconservativeState)(nil).AddToCache), arg0, arg1, arg2)
	return &conservativeStateAddToCacheCall{Call: call}
}

// conservativeStateAddToCacheCall wrap *gomock.Call
type conservativeStateAddToCacheCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateAddToCacheCall) Return(arg0 error) *conservativeStateAddToCacheCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateAddToCacheCall) Do(f func(context.Context, *types.Transaction, time.Time) error) *conservativeStateAddToCacheCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateAddToCacheCall) DoAndReturn(f func(context.Context, *types.Transaction, time.Time) error) *conservativeStateAddToCacheCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// AddToDB mocks base method.
func (m *MockconservativeState) AddToDB(arg0 *types.Transaction) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddToDB", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddToDB indicates an expected call of AddToDB.
func (mr *MockconservativeStateMockRecorder) AddToDB(arg0 any) *conservativeStateAddToDBCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddToDB", reflect.TypeOf((*MockconservativeState)(nil).AddToDB), arg0)
	return &conservativeStateAddToDBCall{Call: call}
}

// conservativeStateAddToDBCall wrap *gomock.Call
type conservativeStateAddToDBCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateAddToDBCall) Return(arg0 error) *conservativeStateAddToDBCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateAddToDBCall) Do(f func(*types.Transaction) error) *conservativeStateAddToDBCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateAddToDBCall) DoAndReturn(f func(*types.Transaction) error) *conservativeStateAddToDBCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockconservativeStateMockRecorder) GetMeshTransaction(arg0 any) *conservativeStateGetMeshTransactionCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMeshTransaction", reflect.TypeOf((*MockconservativeState)(nil).GetMeshTransaction), arg0)
	return &conservativeStateGetMeshTransactionCall{Call: call}
}

// conservativeStateGetMeshTransactionCall wrap *gomock.Call
type conservativeStateGetMeshTransactionCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateGetMeshTransactionCall) Return(arg0 *types.MeshTransaction, arg1 error) *conservativeStateGetMeshTransactionCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateGetMeshTransactionCall) Do(f func(types.TransactionID) (*types.MeshTransaction, error)) *conservativeStateGetMeshTransactionCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateGetMeshTransactionCall) DoAndReturn(f func(types.TransactionID) (*types.MeshTransaction, error)) *conservativeStateGetMeshTransactionCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockconservativeStateMockRecorder) HasTx(arg0 any) *conservativeStateHasTxCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasTx", reflect.TypeOf((*MockconservativeState)(nil).HasTx), arg0)
	return &conservativeStateHasTxCall{Call: call}
}

// conservativeStateHasTxCall wrap *gomock.Call
type conservativeStateHasTxCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateHasTxCall) Return(arg0 bool, arg1 error) *conservativeStateHasTxCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateHasTxCall) Do(f func(types.TransactionID) (bool, error)) *conservativeStateHasTxCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateHasTxCall) DoAndReturn(f func(types.TransactionID) (bool, error)) *conservativeStateHasTxCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Validation mocks base method.
func (m *MockconservativeState) Validation(arg0 types.RawTx) system.ValidationRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validation", arg0)
	ret0, _ := ret[0].(system.ValidationRequest)
	return ret0
}

// Validation indicates an expected call of Validation.
func (mr *MockconservativeStateMockRecorder) Validation(arg0 any) *conservativeStateValidationCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validation", reflect.TypeOf((*MockconservativeState)(nil).Validation), arg0)
	return &conservativeStateValidationCall{Call: call}
}

// conservativeStateValidationCall wrap *gomock.Call
type conservativeStateValidationCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conservativeStateValidationCall) Return(arg0 system.ValidationRequest) *conservativeStateValidationCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conservativeStateValidationCall) Do(f func(types.RawTx) system.ValidationRequest) *conservativeStateValidationCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conservativeStateValidationCall) DoAndReturn(f func(types.RawTx) system.ValidationRequest) *conservativeStateValidationCall {
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

// GetAllAccounts mocks base method.
func (m *MockvmState) GetAllAccounts() ([]*types.Account, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllAccounts")
	ret0, _ := ret[0].([]*types.Account)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllAccounts indicates an expected call of GetAllAccounts.
func (mr *MockvmStateMockRecorder) GetAllAccounts() *vmStateGetAllAccountsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllAccounts", reflect.TypeOf((*MockvmState)(nil).GetAllAccounts))
	return &vmStateGetAllAccountsCall{Call: call}
}

// vmStateGetAllAccountsCall wrap *gomock.Call
type vmStateGetAllAccountsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateGetAllAccountsCall) Return(arg0 []*types.Account, arg1 error) *vmStateGetAllAccountsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateGetAllAccountsCall) Do(f func() ([]*types.Account, error)) *vmStateGetAllAccountsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateGetAllAccountsCall) DoAndReturn(f func() ([]*types.Account, error)) *vmStateGetAllAccountsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockvmStateMockRecorder) GetBalance(arg0 any) *vmStateGetBalanceCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBalance", reflect.TypeOf((*MockvmState)(nil).GetBalance), arg0)
	return &vmStateGetBalanceCall{Call: call}
}

// vmStateGetBalanceCall wrap *gomock.Call
type vmStateGetBalanceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateGetBalanceCall) Return(arg0 uint64, arg1 error) *vmStateGetBalanceCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateGetBalanceCall) Do(f func(types.Address) (uint64, error)) *vmStateGetBalanceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateGetBalanceCall) DoAndReturn(f func(types.Address) (uint64, error)) *vmStateGetBalanceCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockvmStateMockRecorder) GetLayerApplied(arg0 any) *vmStateGetLayerAppliedCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerApplied", reflect.TypeOf((*MockvmState)(nil).GetLayerApplied), arg0)
	return &vmStateGetLayerAppliedCall{Call: call}
}

// vmStateGetLayerAppliedCall wrap *gomock.Call
type vmStateGetLayerAppliedCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateGetLayerAppliedCall) Return(arg0 types.LayerID, arg1 error) *vmStateGetLayerAppliedCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateGetLayerAppliedCall) Do(f func(types.TransactionID) (types.LayerID, error)) *vmStateGetLayerAppliedCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateGetLayerAppliedCall) DoAndReturn(f func(types.TransactionID) (types.LayerID, error)) *vmStateGetLayerAppliedCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockvmStateMockRecorder) GetLayerStateRoot(arg0 any) *vmStateGetLayerStateRootCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerStateRoot", reflect.TypeOf((*MockvmState)(nil).GetLayerStateRoot), arg0)
	return &vmStateGetLayerStateRootCall{Call: call}
}

// vmStateGetLayerStateRootCall wrap *gomock.Call
type vmStateGetLayerStateRootCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateGetLayerStateRootCall) Return(arg0 types.Hash32, arg1 error) *vmStateGetLayerStateRootCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateGetLayerStateRootCall) Do(f func(types.LayerID) (types.Hash32, error)) *vmStateGetLayerStateRootCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateGetLayerStateRootCall) DoAndReturn(f func(types.LayerID) (types.Hash32, error)) *vmStateGetLayerStateRootCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockvmStateMockRecorder) GetNonce(arg0 any) *vmStateGetNonceCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNonce", reflect.TypeOf((*MockvmState)(nil).GetNonce), arg0)
	return &vmStateGetNonceCall{Call: call}
}

// vmStateGetNonceCall wrap *gomock.Call
type vmStateGetNonceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateGetNonceCall) Return(arg0 types.Nonce, arg1 error) *vmStateGetNonceCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateGetNonceCall) Do(f func(types.Address) (types.Nonce, error)) *vmStateGetNonceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateGetNonceCall) DoAndReturn(f func(types.Address) (types.Nonce, error)) *vmStateGetNonceCall {
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

// Validation mocks base method.
func (m *MockvmState) Validation(arg0 types.RawTx) system.ValidationRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validation", arg0)
	ret0, _ := ret[0].(system.ValidationRequest)
	return ret0
}

// Validation indicates an expected call of Validation.
func (mr *MockvmStateMockRecorder) Validation(arg0 any) *vmStateValidationCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validation", reflect.TypeOf((*MockvmState)(nil).Validation), arg0)
	return &vmStateValidationCall{Call: call}
}

// vmStateValidationCall wrap *gomock.Call
type vmStateValidationCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vmStateValidationCall) Return(arg0 system.ValidationRequest) *vmStateValidationCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vmStateValidationCall) Do(f func(types.RawTx) system.ValidationRequest) *vmStateValidationCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vmStateValidationCall) DoAndReturn(f func(types.RawTx) system.ValidationRequest) *vmStateValidationCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (m *MockconStateCache) GetMempool(arg0 log.Log) map[types.Address][]*NanoTX {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMempool", arg0)
	ret0, _ := ret[0].(map[types.Address][]*NanoTX)
	return ret0
}

// GetMempool indicates an expected call of GetMempool.
func (mr *MockconStateCacheMockRecorder) GetMempool(arg0 any) *conStateCacheGetMempoolCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMempool", reflect.TypeOf((*MockconStateCache)(nil).GetMempool), arg0)
	return &conStateCacheGetMempoolCall{Call: call}
}

// conStateCacheGetMempoolCall wrap *gomock.Call
type conStateCacheGetMempoolCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *conStateCacheGetMempoolCall) Return(arg0 map[types.Address][]*NanoTX) *conStateCacheGetMempoolCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *conStateCacheGetMempoolCall) Do(f func(log.Log) map[types.Address][]*NanoTX) *conStateCacheGetMempoolCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *conStateCacheGetMempoolCall) DoAndReturn(f func(log.Log) map[types.Address][]*NanoTX) *conStateCacheGetMempoolCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
