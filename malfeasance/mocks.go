// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go
//
// Generated by this command:
//
//	mockgen -typed -package=malfeasance -destination=./mocks.go -source=./interface.go
//

// Package malfeasance is a generated GoMock package.
package malfeasance

import (
	context "context"
	reflect "reflect"

	prometheus "github.com/prometheus/client_golang/prometheus"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	wire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	gomock "go.uber.org/mock/gomock"
)

// Mocktortoise is a mock of tortoise interface.
type Mocktortoise struct {
	ctrl     *gomock.Controller
	recorder *MocktortoiseMockRecorder
	isgomock struct{}
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

// OnMalfeasance mocks base method.
func (m *Mocktortoise) OnMalfeasance(arg0 types.NodeID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnMalfeasance", arg0)
}

// OnMalfeasance indicates an expected call of OnMalfeasance.
func (mr *MocktortoiseMockRecorder) OnMalfeasance(arg0 any) *MocktortoiseOnMalfeasanceCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnMalfeasance", reflect.TypeOf((*Mocktortoise)(nil).OnMalfeasance), arg0)
	return &MocktortoiseOnMalfeasanceCall{Call: call}
}

// MocktortoiseOnMalfeasanceCall wrap *gomock.Call
type MocktortoiseOnMalfeasanceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocktortoiseOnMalfeasanceCall) Return() *MocktortoiseOnMalfeasanceCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocktortoiseOnMalfeasanceCall) Do(f func(types.NodeID)) *MocktortoiseOnMalfeasanceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocktortoiseOnMalfeasanceCall) DoAndReturn(f func(types.NodeID)) *MocktortoiseOnMalfeasanceCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockMalfeasanceHandler is a mock of MalfeasanceHandler interface.
type MockMalfeasanceHandler struct {
	ctrl     *gomock.Controller
	recorder *MockMalfeasanceHandlerMockRecorder
	isgomock struct{}
}

// MockMalfeasanceHandlerMockRecorder is the mock recorder for MockMalfeasanceHandler.
type MockMalfeasanceHandlerMockRecorder struct {
	mock *MockMalfeasanceHandler
}

// NewMockMalfeasanceHandler creates a new mock instance.
func NewMockMalfeasanceHandler(ctrl *gomock.Controller) *MockMalfeasanceHandler {
	mock := &MockMalfeasanceHandler{ctrl: ctrl}
	mock.recorder = &MockMalfeasanceHandlerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockMalfeasanceHandler) EXPECT() *MockMalfeasanceHandlerMockRecorder {
	return m.recorder
}

// Info mocks base method.
func (m *MockMalfeasanceHandler) Info(data wire.ProofData) (map[string]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Info", data)
	ret0, _ := ret[0].(map[string]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Info indicates an expected call of Info.
func (mr *MockMalfeasanceHandlerMockRecorder) Info(data any) *MockMalfeasanceHandlerInfoCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Info", reflect.TypeOf((*MockMalfeasanceHandler)(nil).Info), data)
	return &MockMalfeasanceHandlerInfoCall{Call: call}
}

// MockMalfeasanceHandlerInfoCall wrap *gomock.Call
type MockMalfeasanceHandlerInfoCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockMalfeasanceHandlerInfoCall) Return(arg0 map[string]string, arg1 error) *MockMalfeasanceHandlerInfoCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockMalfeasanceHandlerInfoCall) Do(f func(wire.ProofData) (map[string]string, error)) *MockMalfeasanceHandlerInfoCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockMalfeasanceHandlerInfoCall) DoAndReturn(f func(wire.ProofData) (map[string]string, error)) *MockMalfeasanceHandlerInfoCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// ReportInvalidProof mocks base method.
func (m *MockMalfeasanceHandler) ReportInvalidProof(vec *prometheus.CounterVec) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "ReportInvalidProof", vec)
}

// ReportInvalidProof indicates an expected call of ReportInvalidProof.
func (mr *MockMalfeasanceHandlerMockRecorder) ReportInvalidProof(vec any) *MockMalfeasanceHandlerReportInvalidProofCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReportInvalidProof", reflect.TypeOf((*MockMalfeasanceHandler)(nil).ReportInvalidProof), vec)
	return &MockMalfeasanceHandlerReportInvalidProofCall{Call: call}
}

// MockMalfeasanceHandlerReportInvalidProofCall wrap *gomock.Call
type MockMalfeasanceHandlerReportInvalidProofCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockMalfeasanceHandlerReportInvalidProofCall) Return() *MockMalfeasanceHandlerReportInvalidProofCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockMalfeasanceHandlerReportInvalidProofCall) Do(f func(*prometheus.CounterVec)) *MockMalfeasanceHandlerReportInvalidProofCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockMalfeasanceHandlerReportInvalidProofCall) DoAndReturn(f func(*prometheus.CounterVec)) *MockMalfeasanceHandlerReportInvalidProofCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// ReportProof mocks base method.
func (m *MockMalfeasanceHandler) ReportProof(vec *prometheus.CounterVec) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "ReportProof", vec)
}

// ReportProof indicates an expected call of ReportProof.
func (mr *MockMalfeasanceHandlerMockRecorder) ReportProof(vec any) *MockMalfeasanceHandlerReportProofCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReportProof", reflect.TypeOf((*MockMalfeasanceHandler)(nil).ReportProof), vec)
	return &MockMalfeasanceHandlerReportProofCall{Call: call}
}

// MockMalfeasanceHandlerReportProofCall wrap *gomock.Call
type MockMalfeasanceHandlerReportProofCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockMalfeasanceHandlerReportProofCall) Return() *MockMalfeasanceHandlerReportProofCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockMalfeasanceHandlerReportProofCall) Do(f func(*prometheus.CounterVec)) *MockMalfeasanceHandlerReportProofCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockMalfeasanceHandlerReportProofCall) DoAndReturn(f func(*prometheus.CounterVec)) *MockMalfeasanceHandlerReportProofCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Validate mocks base method.
func (m *MockMalfeasanceHandler) Validate(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validate", ctx, data)
	ret0, _ := ret[0].(types.NodeID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Validate indicates an expected call of Validate.
func (mr *MockMalfeasanceHandlerMockRecorder) Validate(ctx, data any) *MockMalfeasanceHandlerValidateCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validate", reflect.TypeOf((*MockMalfeasanceHandler)(nil).Validate), ctx, data)
	return &MockMalfeasanceHandlerValidateCall{Call: call}
}

// MockMalfeasanceHandlerValidateCall wrap *gomock.Call
type MockMalfeasanceHandlerValidateCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockMalfeasanceHandlerValidateCall) Return(arg0 types.NodeID, arg1 error) *MockMalfeasanceHandlerValidateCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockMalfeasanceHandlerValidateCall) Do(f func(context.Context, wire.ProofData) (types.NodeID, error)) *MockMalfeasanceHandlerValidateCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockMalfeasanceHandlerValidateCall) DoAndReturn(f func(context.Context, wire.ProofData) (types.NodeID, error)) *MockMalfeasanceHandlerValidateCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
