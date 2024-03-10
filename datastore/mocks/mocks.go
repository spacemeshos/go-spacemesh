// Code generated by MockGen. DO NOT EDIT.
// Source: ./store.go
//
// Generated by this command:
//
//	mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./store.go
//

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	sql "github.com/spacemeshos/go-spacemesh/sql"
	gomock "go.uber.org/mock/gomock"
)

// MockExecutor is a mock of Executor interface.
type MockExecutor struct {
	ctrl     *gomock.Controller
	recorder *MockExecutorMockRecorder
}

// MockExecutorMockRecorder is the mock recorder for MockExecutor.
type MockExecutorMockRecorder struct {
	mock *MockExecutor
}

// NewMockExecutor creates a new mock instance.
func NewMockExecutor(ctrl *gomock.Controller) *MockExecutor {
	mock := &MockExecutor{ctrl: ctrl}
	mock.recorder = &MockExecutorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockExecutor) EXPECT() *MockExecutorMockRecorder {
	return m.recorder
}

// Exec mocks base method.
func (m *MockExecutor) Exec(arg0 string, arg1 sql.Encoder, arg2 sql.Decoder) (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Exec", arg0, arg1, arg2)
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Exec indicates an expected call of Exec.
func (mr *MockExecutorMockRecorder) Exec(arg0, arg1, arg2 any) *MockExecutorExecCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Exec", reflect.TypeOf((*MockExecutor)(nil).Exec), arg0, arg1, arg2)
	return &MockExecutorExecCall{Call: call}
}

// MockExecutorExecCall wrap *gomock.Call
type MockExecutorExecCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockExecutorExecCall) Return(arg0 int, arg1 error) *MockExecutorExecCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockExecutorExecCall) Do(f func(string, sql.Encoder, sql.Decoder) (int, error)) *MockExecutorExecCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockExecutorExecCall) DoAndReturn(f func(string, sql.Encoder, sql.Decoder) (int, error)) *MockExecutorExecCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// QueryCache mocks base method.
func (m *MockExecutor) QueryCache() sql.QueryCache {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "QueryCache")
	ret0, _ := ret[0].(sql.QueryCache)
	return ret0
}

// QueryCache indicates an expected call of QueryCache.
func (mr *MockExecutorMockRecorder) QueryCache() *MockExecutorQueryCacheCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "QueryCache", reflect.TypeOf((*MockExecutor)(nil).QueryCache))
	return &MockExecutorQueryCacheCall{Call: call}
}

// MockExecutorQueryCacheCall wrap *gomock.Call
type MockExecutorQueryCacheCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockExecutorQueryCacheCall) Return(arg0 sql.QueryCache) *MockExecutorQueryCacheCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockExecutorQueryCacheCall) Do(f func() sql.QueryCache) *MockExecutorQueryCacheCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockExecutorQueryCacheCall) DoAndReturn(f func() sql.QueryCache) *MockExecutorQueryCacheCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// WithTx mocks base method.
func (m *MockExecutor) WithTx(arg0 context.Context, arg1 func(*sql.Tx) error) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WithTx", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// WithTx indicates an expected call of WithTx.
func (mr *MockExecutorMockRecorder) WithTx(arg0, arg1 any) *MockExecutorWithTxCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WithTx", reflect.TypeOf((*MockExecutor)(nil).WithTx), arg0, arg1)
	return &MockExecutorWithTxCall{Call: call}
}

// MockExecutorWithTxCall wrap *gomock.Call
type MockExecutorWithTxCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockExecutorWithTxCall) Return(arg0 error) *MockExecutorWithTxCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockExecutorWithTxCall) Do(f func(context.Context, func(*sql.Tx) error) error) *MockExecutorWithTxCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockExecutorWithTxCall) DoAndReturn(f func(context.Context, func(*sql.Tx) error) error) *MockExecutorWithTxCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
