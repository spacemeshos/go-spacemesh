// Code generated by MockGen. DO NOT EDIT.
// Source: ./beacons.go

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
)

// MockBeaconCollector is a mock of BeaconCollector interface.
type MockBeaconCollector struct {
	ctrl     *gomock.Controller
	recorder *MockBeaconCollectorMockRecorder
}

// MockBeaconCollectorMockRecorder is the mock recorder for MockBeaconCollector.
type MockBeaconCollectorMockRecorder struct {
	mock *MockBeaconCollector
}

// NewMockBeaconCollector creates a new mock instance.
func NewMockBeaconCollector(ctrl *gomock.Controller) *MockBeaconCollector {
	mock := &MockBeaconCollector{ctrl: ctrl}
	mock.recorder = &MockBeaconCollectorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockBeaconCollector) EXPECT() *MockBeaconCollectorMockRecorder {
	return m.recorder
}

// ReportBeaconFromBallot mocks base method.
func (m *MockBeaconCollector) ReportBeaconFromBallot(arg0 types.EpochID, arg1 types.BallotID, arg2 types.Beacon, arg3 uint64) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "ReportBeaconFromBallot", arg0, arg1, arg2, arg3)
}

// ReportBeaconFromBallot indicates an expected call of ReportBeaconFromBallot.
func (mr *MockBeaconCollectorMockRecorder) ReportBeaconFromBallot(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReportBeaconFromBallot", reflect.TypeOf((*MockBeaconCollector)(nil).ReportBeaconFromBallot), arg0, arg1, arg2, arg3)
}
