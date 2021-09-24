// Code generated by MockGen. DO NOT EDIT.
// Source: epochbeacon.go

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
)

// MockBeaconGetter is a mock of BeaconGetter interface.
type MockBeaconGetter struct {
	ctrl     *gomock.Controller
	recorder *MockBeaconGetterMockRecorder
}

// MockBeaconGetterMockRecorder is the mock recorder for MockBeaconGetter.
type MockBeaconGetterMockRecorder struct {
	mock *MockBeaconGetter
}

// NewMockBeaconGetter creates a new mock instance.
func NewMockBeaconGetter(ctrl *gomock.Controller) *MockBeaconGetter {
	mock := &MockBeaconGetter{ctrl: ctrl}
	mock.recorder = &MockBeaconGetterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockBeaconGetter) EXPECT() *MockBeaconGetterMockRecorder {
	return m.recorder
}

// GetBeacon mocks base method.
func (m *MockBeaconGetter) GetBeacon(epochNumber types.EpochID) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBeacon", epochNumber)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBeacon indicates an expected call of GetBeacon.
func (mr *MockBeaconGetterMockRecorder) GetBeacon(epochNumber interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBeacon", reflect.TypeOf((*MockBeaconGetter)(nil).GetBeacon), epochNumber)
}
