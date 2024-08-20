// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/spacemeshos/go-spacemesh/sql (interfaces: Executor)
//
// Generated by this command:
//
//	mockgen -typed -package=mocks -destination=./mocks/mocks.go github.com/spacemeshos/go-spacemesh/sql Executor
//

// Package mocks is a generated GoMock package.
package mocks

import (
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
