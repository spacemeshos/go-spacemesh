// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
)

// MockpoetValidatorPersistor is a mock of poetValidatorPersistor interface.
type MockpoetValidatorPersistor struct {
	ctrl     *gomock.Controller
	recorder *MockpoetValidatorPersistorMockRecorder
}

// MockpoetValidatorPersistorMockRecorder is the mock recorder for MockpoetValidatorPersistor.
type MockpoetValidatorPersistorMockRecorder struct {
	mock *MockpoetValidatorPersistor
}

// NewMockpoetValidatorPersistor creates a new mock instance.
func NewMockpoetValidatorPersistor(ctrl *gomock.Controller) *MockpoetValidatorPersistor {
	mock := &MockpoetValidatorPersistor{ctrl: ctrl}
	mock.recorder = &MockpoetValidatorPersistorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpoetValidatorPersistor) EXPECT() *MockpoetValidatorPersistorMockRecorder {
	return m.recorder
}

// HasProof mocks base method.
func (m *MockpoetValidatorPersistor) HasProof(arg0 []byte) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasProof", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// HasProof indicates an expected call of HasProof.
func (mr *MockpoetValidatorPersistorMockRecorder) HasProof(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasProof", reflect.TypeOf((*MockpoetValidatorPersistor)(nil).HasProof), arg0)
}

// StoreProof mocks base method.
func (m *MockpoetValidatorPersistor) StoreProof(arg0 []byte, arg1 *types.PoetProofMessage) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StoreProof", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// StoreProof indicates an expected call of StoreProof.
func (mr *MockpoetValidatorPersistorMockRecorder) StoreProof(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StoreProof", reflect.TypeOf((*MockpoetValidatorPersistor)(nil).StoreProof), arg0, arg1)
}

// Validate mocks base method.
func (m *MockpoetValidatorPersistor) Validate(arg0 types.PoetProof, arg1 []byte, arg2 string, arg3 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validate", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// Validate indicates an expected call of Validate.
func (mr *MockpoetValidatorPersistorMockRecorder) Validate(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validate", reflect.TypeOf((*MockpoetValidatorPersistor)(nil).Validate), arg0, arg1, arg2, arg3)
}
