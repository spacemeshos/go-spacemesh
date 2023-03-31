// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/spacemeshos/go-spacemesh/api (interfaces: NetworkIdentity,AtxProvider,PostSetupProvider,ChallengeVerifier)

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	peer "github.com/libp2p/go-libp2p/core/peer"
	activation "github.com/spacemeshos/go-spacemesh/activation"
	types "github.com/spacemeshos/go-spacemesh/common/types"
)

// MockNetworkIdentity is a mock of NetworkIdentity interface.
type MockNetworkIdentity struct {
	ctrl     *gomock.Controller
	recorder *MockNetworkIdentityMockRecorder
}

// MockNetworkIdentityMockRecorder is the mock recorder for MockNetworkIdentity.
type MockNetworkIdentityMockRecorder struct {
	mock *MockNetworkIdentity
}

// NewMockNetworkIdentity creates a new mock instance.
func NewMockNetworkIdentity(ctrl *gomock.Controller) *MockNetworkIdentity {
	mock := &MockNetworkIdentity{ctrl: ctrl}
	mock.recorder = &MockNetworkIdentityMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockNetworkIdentity) EXPECT() *MockNetworkIdentityMockRecorder {
	return m.recorder
}

// ID mocks base method.
func (m *MockNetworkIdentity) ID() peer.ID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ID")
	ret0, _ := ret[0].(peer.ID)
	return ret0
}

// ID indicates an expected call of ID.
func (mr *MockNetworkIdentityMockRecorder) ID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ID", reflect.TypeOf((*MockNetworkIdentity)(nil).ID))
}

// MockAtxProvider is a mock of AtxProvider interface.
type MockAtxProvider struct {
	ctrl     *gomock.Controller
	recorder *MockAtxProviderMockRecorder
}

// MockAtxProviderMockRecorder is the mock recorder for MockAtxProvider.
type MockAtxProviderMockRecorder struct {
	mock *MockAtxProvider
}

// NewMockAtxProvider creates a new mock instance.
func NewMockAtxProvider(ctrl *gomock.Controller) *MockAtxProvider {
	mock := &MockAtxProvider{ctrl: ctrl}
	mock.recorder = &MockAtxProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAtxProvider) EXPECT() *MockAtxProviderMockRecorder {
	return m.recorder
}

// GetFullAtx mocks base method.
func (m *MockAtxProvider) GetFullAtx(arg0 types.ATXID) (*types.VerifiedActivationTx, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetFullAtx", arg0)
	ret0, _ := ret[0].(*types.VerifiedActivationTx)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetFullAtx indicates an expected call of GetFullAtx.
func (mr *MockAtxProviderMockRecorder) GetFullAtx(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetFullAtx", reflect.TypeOf((*MockAtxProvider)(nil).GetFullAtx), arg0)
}

// MockPostSetupProvider is a mock of PostSetupProvider interface.
type MockPostSetupProvider struct {
	ctrl     *gomock.Controller
	recorder *MockPostSetupProviderMockRecorder
}

// MockPostSetupProviderMockRecorder is the mock recorder for MockPostSetupProvider.
type MockPostSetupProviderMockRecorder struct {
	mock *MockPostSetupProvider
}

// NewMockPostSetupProvider creates a new mock instance.
func NewMockPostSetupProvider(ctrl *gomock.Controller) *MockPostSetupProvider {
	mock := &MockPostSetupProvider{ctrl: ctrl}
	mock.recorder = &MockPostSetupProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPostSetupProvider) EXPECT() *MockPostSetupProviderMockRecorder {
	return m.recorder
}

// Benchmark mocks base method.
func (m *MockPostSetupProvider) Benchmark(arg0 activation.PostSetupComputeProvider) (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Benchmark", arg0)
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Benchmark indicates an expected call of Benchmark.
func (mr *MockPostSetupProviderMockRecorder) Benchmark(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Benchmark", reflect.TypeOf((*MockPostSetupProvider)(nil).Benchmark), arg0)
}

// ComputeProviders mocks base method.
func (m *MockPostSetupProvider) ComputeProviders() []activation.PostSetupComputeProvider {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ComputeProviders")
	ret0, _ := ret[0].([]activation.PostSetupComputeProvider)
	return ret0
}

// ComputeProviders indicates an expected call of ComputeProviders.
func (mr *MockPostSetupProviderMockRecorder) ComputeProviders() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ComputeProviders", reflect.TypeOf((*MockPostSetupProvider)(nil).ComputeProviders))
}

// Config mocks base method.
func (m *MockPostSetupProvider) Config() activation.PostConfig {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Config")
	ret0, _ := ret[0].(activation.PostConfig)
	return ret0
}

// Config indicates an expected call of Config.
func (mr *MockPostSetupProviderMockRecorder) Config() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Config", reflect.TypeOf((*MockPostSetupProvider)(nil).Config))
}

// Status mocks base method.
func (m *MockPostSetupProvider) Status() *activation.PostSetupStatus {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Status")
	ret0, _ := ret[0].(*activation.PostSetupStatus)
	return ret0
}

// Status indicates an expected call of Status.
func (mr *MockPostSetupProviderMockRecorder) Status() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Status", reflect.TypeOf((*MockPostSetupProvider)(nil).Status))
}

// MockChallengeVerifier is a mock of ChallengeVerifier interface.
type MockChallengeVerifier struct {
	ctrl     *gomock.Controller
	recorder *MockChallengeVerifierMockRecorder
}

// MockChallengeVerifierMockRecorder is the mock recorder for MockChallengeVerifier.
type MockChallengeVerifierMockRecorder struct {
	mock *MockChallengeVerifier
}

// NewMockChallengeVerifier creates a new mock instance.
func NewMockChallengeVerifier(ctrl *gomock.Controller) *MockChallengeVerifier {
	mock := &MockChallengeVerifier{ctrl: ctrl}
	mock.recorder = &MockChallengeVerifierMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockChallengeVerifier) EXPECT() *MockChallengeVerifierMockRecorder {
	return m.recorder
}

// Verify mocks base method.
func (m *MockChallengeVerifier) Verify(arg0 context.Context, arg1 []byte, arg2 types.EdSignature) (*activation.ChallengeVerificationResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Verify", arg0, arg1, arg2)
	ret0, _ := ret[0].(*activation.ChallengeVerificationResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Verify indicates an expected call of Verify.
func (mr *MockChallengeVerifierMockRecorder) Verify(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*MockChallengeVerifier)(nil).Verify), arg0, arg1, arg2)
}
