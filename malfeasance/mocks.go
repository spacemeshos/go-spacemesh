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

	types "github.com/spacemeshos/go-spacemesh/common/types"
	signing "github.com/spacemeshos/go-spacemesh/signing"
	shared "github.com/spacemeshos/post/shared"
	verifying "github.com/spacemeshos/post/verifying"
	gomock "go.uber.org/mock/gomock"
)

// MockSigVerifier is a mock of SigVerifier interface.
type MockSigVerifier struct {
	ctrl     *gomock.Controller
	recorder *MockSigVerifierMockRecorder
}

// MockSigVerifierMockRecorder is the mock recorder for MockSigVerifier.
type MockSigVerifierMockRecorder struct {
	mock *MockSigVerifier
}

// NewMockSigVerifier creates a new mock instance.
func NewMockSigVerifier(ctrl *gomock.Controller) *MockSigVerifier {
	mock := &MockSigVerifier{ctrl: ctrl}
	mock.recorder = &MockSigVerifierMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockSigVerifier) EXPECT() *MockSigVerifierMockRecorder {
	return m.recorder
}

// Verify mocks base method.
func (m *MockSigVerifier) Verify(arg0 signing.Domain, arg1 types.NodeID, arg2 []byte, arg3 types.EdSignature) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Verify", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *MockSigVerifierMockRecorder) Verify(arg0, arg1, arg2, arg3 any) *MockSigVerifierVerifyCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*MockSigVerifier)(nil).Verify), arg0, arg1, arg2, arg3)
	return &MockSigVerifierVerifyCall{Call: call}
}

// MockSigVerifierVerifyCall wrap *gomock.Call
type MockSigVerifierVerifyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSigVerifierVerifyCall) Return(arg0 bool) *MockSigVerifierVerifyCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSigVerifierVerifyCall) Do(f func(signing.Domain, types.NodeID, []byte, types.EdSignature) bool) *MockSigVerifierVerifyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSigVerifierVerifyCall) DoAndReturn(f func(signing.Domain, types.NodeID, []byte, types.EdSignature) bool) *MockSigVerifierVerifyCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Mocktortoise is a mock of tortoise interface.
type Mocktortoise struct {
	ctrl     *gomock.Controller
	recorder *MocktortoiseMockRecorder
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

// MockpostVerifier is a mock of postVerifier interface.
type MockpostVerifier struct {
	ctrl     *gomock.Controller
	recorder *MockpostVerifierMockRecorder
}

// MockpostVerifierMockRecorder is the mock recorder for MockpostVerifier.
type MockpostVerifierMockRecorder struct {
	mock *MockpostVerifier
}

// NewMockpostVerifier creates a new mock instance.
func NewMockpostVerifier(ctrl *gomock.Controller) *MockpostVerifier {
	mock := &MockpostVerifier{ctrl: ctrl}
	mock.recorder = &MockpostVerifierMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpostVerifier) EXPECT() *MockpostVerifierMockRecorder {
	return m.recorder
}

// Verify mocks base method.
func (m_2 *MockpostVerifier) Verify(ctx context.Context, p *shared.Proof, m *shared.ProofMetadata, opts ...verifying.OptionFunc) error {
	m_2.ctrl.T.Helper()
	varargs := []any{ctx, p, m}
	for _, a := range opts {
		varargs = append(varargs, a)
	}
	ret := m_2.ctrl.Call(m_2, "Verify", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *MockpostVerifierMockRecorder) Verify(ctx, p, m any, opts ...any) *MockpostVerifierVerifyCall {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{ctx, p, m}, opts...)
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*MockpostVerifier)(nil).Verify), varargs...)
	return &MockpostVerifierVerifyCall{Call: call}
}

// MockpostVerifierVerifyCall wrap *gomock.Call
type MockpostVerifierVerifyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpostVerifierVerifyCall) Return(arg0 error) *MockpostVerifierVerifyCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpostVerifierVerifyCall) Do(f func(context.Context, *shared.Proof, *shared.ProofMetadata, ...verifying.OptionFunc) error) *MockpostVerifierVerifyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpostVerifierVerifyCall) DoAndReturn(f func(context.Context, *shared.Proof, *shared.ProofMetadata, ...verifying.OptionFunc) error) *MockpostVerifierVerifyCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
