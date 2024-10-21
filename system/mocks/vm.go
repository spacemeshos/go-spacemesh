// Code generated by MockGen. DO NOT EDIT.
// Source: ./vm.go
//
// Generated by this command:
//
//	mockgen -typed -package=mocks -destination=./mocks/vm.go -source=./vm.go
//

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	gomock "go.uber.org/mock/gomock"
)

// MockValidationRequest is a mock of ValidationRequest interface.
type MockValidationRequest struct {
	ctrl     *gomock.Controller
	recorder *MockValidationRequestMockRecorder
	isgomock struct{}
}

// MockValidationRequestMockRecorder is the mock recorder for MockValidationRequest.
type MockValidationRequestMockRecorder struct {
	mock *MockValidationRequest
}

// NewMockValidationRequest creates a new mock instance.
func NewMockValidationRequest(ctrl *gomock.Controller) *MockValidationRequest {
	mock := &MockValidationRequest{ctrl: ctrl}
	mock.recorder = &MockValidationRequestMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockValidationRequest) EXPECT() *MockValidationRequestMockRecorder {
	return m.recorder
}

// Parse mocks base method.
func (m *MockValidationRequest) Parse() (*types.TxHeader, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Parse")
	ret0, _ := ret[0].(*types.TxHeader)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Parse indicates an expected call of Parse.
func (mr *MockValidationRequestMockRecorder) Parse() *MockValidationRequestParseCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Parse", reflect.TypeOf((*MockValidationRequest)(nil).Parse))
	return &MockValidationRequestParseCall{Call: call}
}

// MockValidationRequestParseCall wrap *gomock.Call
type MockValidationRequestParseCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockValidationRequestParseCall) Return(arg0 *types.TxHeader, arg1 error) *MockValidationRequestParseCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockValidationRequestParseCall) Do(f func() (*types.TxHeader, error)) *MockValidationRequestParseCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockValidationRequestParseCall) DoAndReturn(f func() (*types.TxHeader, error)) *MockValidationRequestParseCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Verify mocks base method.
func (m *MockValidationRequest) Verify() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Verify")
	ret0, _ := ret[0].(bool)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *MockValidationRequestMockRecorder) Verify() *MockValidationRequestVerifyCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*MockValidationRequest)(nil).Verify))
	return &MockValidationRequestVerifyCall{Call: call}
}

// MockValidationRequestVerifyCall wrap *gomock.Call
type MockValidationRequestVerifyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockValidationRequestVerifyCall) Return(arg0 bool) *MockValidationRequestVerifyCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockValidationRequestVerifyCall) Do(f func() bool) *MockValidationRequestVerifyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockValidationRequestVerifyCall) DoAndReturn(f func() bool) *MockValidationRequestVerifyCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
