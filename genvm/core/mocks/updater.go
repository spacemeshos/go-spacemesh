// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/spacemeshos/go-spacemesh/genvm/core (interfaces: AccountUpdater)
//
// Generated by this command:
//
//	mockgen -typed -package=mocks -destination=./mocks/updater.go github.com/spacemeshos/go-spacemesh/genvm/core AccountUpdater
//

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	gomock "go.uber.org/mock/gomock"
)

// MockAccountUpdater is a mock of AccountUpdater interface.
type MockAccountUpdater struct {
	ctrl     *gomock.Controller
	recorder *MockAccountUpdaterMockRecorder
}

// MockAccountUpdaterMockRecorder is the mock recorder for MockAccountUpdater.
type MockAccountUpdaterMockRecorder struct {
	mock *MockAccountUpdater
}

// NewMockAccountUpdater creates a new mock instance.
func NewMockAccountUpdater(ctrl *gomock.Controller) *MockAccountUpdater {
	mock := &MockAccountUpdater{ctrl: ctrl}
	mock.recorder = &MockAccountUpdaterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAccountUpdater) EXPECT() *MockAccountUpdaterMockRecorder {
	return m.recorder
}

// Update mocks base method.
func (m *MockAccountUpdater) Update(arg0 types.Account) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Update", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Update indicates an expected call of Update.
func (mr *MockAccountUpdaterMockRecorder) Update(arg0 any) *MockAccountUpdaterUpdateCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Update", reflect.TypeOf((*MockAccountUpdater)(nil).Update), arg0)
	return &MockAccountUpdaterUpdateCall{Call: call}
}

// MockAccountUpdaterUpdateCall wrap *gomock.Call
type MockAccountUpdaterUpdateCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockAccountUpdaterUpdateCall) Return(arg0 error) *MockAccountUpdaterUpdateCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockAccountUpdaterUpdateCall) Do(f func(types.Account) error) *MockAccountUpdaterUpdateCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockAccountUpdaterUpdateCall) DoAndReturn(f func(types.Account) error) *MockAccountUpdaterUpdateCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
