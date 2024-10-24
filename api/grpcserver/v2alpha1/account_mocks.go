// Code generated by MockGen. DO NOT EDIT.
// Source: ./account.go
//
// Generated by this command:
//
//	mockgen -typed -package=v2alpha1 -destination=./account_mocks.go -source=./account.go
//

// Package v2alpha1 is a generated GoMock package.
package v2alpha1

import (
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	gomock "go.uber.org/mock/gomock"
)

// MockaccountConState is a mock of accountConState interface.
type MockaccountConState struct {
	ctrl     *gomock.Controller
	recorder *MockaccountConStateMockRecorder
}

// MockaccountConStateMockRecorder is the mock recorder for MockaccountConState.
type MockaccountConStateMockRecorder struct {
	mock *MockaccountConState
}

// NewMockaccountConState creates a new mock instance.
func NewMockaccountConState(ctrl *gomock.Controller) *MockaccountConState {
	mock := &MockaccountConState{ctrl: ctrl}
	mock.recorder = &MockaccountConStateMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockaccountConState) EXPECT() *MockaccountConStateMockRecorder {
	return m.recorder
}

// GetProjection mocks base method.
func (m *MockaccountConState) GetProjection(arg0 types.Address) (uint64, uint64) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProjection", arg0)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(uint64)
	return ret0, ret1
}

// GetProjection indicates an expected call of GetProjection.
func (mr *MockaccountConStateMockRecorder) GetProjection(arg0 any) *MockaccountConStateGetProjectionCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProjection", reflect.TypeOf((*MockaccountConState)(nil).GetProjection), arg0)
	return &MockaccountConStateGetProjectionCall{Call: call}
}

// MockaccountConStateGetProjectionCall wrap *gomock.Call
type MockaccountConStateGetProjectionCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockaccountConStateGetProjectionCall) Return(arg0, arg1 uint64) *MockaccountConStateGetProjectionCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockaccountConStateGetProjectionCall) Do(f func(types.Address) (uint64, uint64)) *MockaccountConStateGetProjectionCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockaccountConStateGetProjectionCall) DoAndReturn(f func(types.Address) (uint64, uint64)) *MockaccountConStateGetProjectionCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
