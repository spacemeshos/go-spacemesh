// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go
//
// Generated by this command:
//
//	mockgen -typed -package=eligibility -destination=./mocks.go -source=./interface.go
//
// Package eligibility is a generated GoMock package.
package eligibility

import (
	context "context"
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	signing "github.com/spacemeshos/go-spacemesh/signing"
	gomock "go.uber.org/mock/gomock"
)

// MockactiveSetCache is a mock of activeSetCache interface.
type MockactiveSetCache struct {
	ctrl     *gomock.Controller
	recorder *MockactiveSetCacheMockRecorder
}

// MockactiveSetCacheMockRecorder is the mock recorder for MockactiveSetCache.
type MockactiveSetCacheMockRecorder struct {
	mock *MockactiveSetCache
}

// NewMockactiveSetCache creates a new mock instance.
func NewMockactiveSetCache(ctrl *gomock.Controller) *MockactiveSetCache {
	mock := &MockactiveSetCache{ctrl: ctrl}
	mock.recorder = &MockactiveSetCacheMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockactiveSetCache) EXPECT() *MockactiveSetCacheMockRecorder {
	return m.recorder
}

// Add mocks base method.
func (m *MockactiveSetCache) Add(key types.EpochID, value *cachedActiveSet) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Add", key, value)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Add indicates an expected call of Add.
func (mr *MockactiveSetCacheMockRecorder) Add(key, value any) *activeSetCacheAddCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Add", reflect.TypeOf((*MockactiveSetCache)(nil).Add), key, value)
	return &activeSetCacheAddCall{Call: call}
}

// activeSetCacheAddCall wrap *gomock.Call
type activeSetCacheAddCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *activeSetCacheAddCall) Return(evicted bool) *activeSetCacheAddCall {
	c.Call = c.Call.Return(evicted)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *activeSetCacheAddCall) Do(f func(types.EpochID, *cachedActiveSet) bool) *activeSetCacheAddCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *activeSetCacheAddCall) DoAndReturn(f func(types.EpochID, *cachedActiveSet) bool) *activeSetCacheAddCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Get mocks base method.
func (m *MockactiveSetCache) Get(key types.EpochID) (*cachedActiveSet, bool) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", key)
	ret0, _ := ret[0].(*cachedActiveSet)
	ret1, _ := ret[1].(bool)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockactiveSetCacheMockRecorder) Get(key any) *activeSetCacheGetCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockactiveSetCache)(nil).Get), key)
	return &activeSetCacheGetCall{Call: call}
}

// activeSetCacheGetCall wrap *gomock.Call
type activeSetCacheGetCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *activeSetCacheGetCall) Return(value *cachedActiveSet, ok bool) *activeSetCacheGetCall {
	c.Call = c.Call.Return(value, ok)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *activeSetCacheGetCall) Do(f func(types.EpochID) (*cachedActiveSet, bool)) *activeSetCacheGetCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *activeSetCacheGetCall) DoAndReturn(f func(types.EpochID) (*cachedActiveSet, bool)) *activeSetCacheGetCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockvrfVerifier is a mock of vrfVerifier interface.
type MockvrfVerifier struct {
	ctrl     *gomock.Controller
	recorder *MockvrfVerifierMockRecorder
}

// MockvrfVerifierMockRecorder is the mock recorder for MockvrfVerifier.
type MockvrfVerifierMockRecorder struct {
	mock *MockvrfVerifier
}

// NewMockvrfVerifier creates a new mock instance.
func NewMockvrfVerifier(ctrl *gomock.Controller) *MockvrfVerifier {
	mock := &MockvrfVerifier{ctrl: ctrl}
	mock.recorder = &MockvrfVerifierMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockvrfVerifier) EXPECT() *MockvrfVerifierMockRecorder {
	return m.recorder
}

// Verify mocks base method.
func (m *MockvrfVerifier) Verify(nodeID types.NodeID, msg []byte, sig types.VrfSignature) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Verify", nodeID, msg, sig)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *MockvrfVerifierMockRecorder) Verify(nodeID, msg, sig any) *vrfVerifierVerifyCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*MockvrfVerifier)(nil).Verify), nodeID, msg, sig)
	return &vrfVerifierVerifyCall{Call: call}
}

// vrfVerifierVerifyCall wrap *gomock.Call
type vrfVerifierVerifyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vrfVerifierVerifyCall) Return(arg0 bool) *vrfVerifierVerifyCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vrfVerifierVerifyCall) Do(f func(types.NodeID, []byte, types.VrfSignature) bool) *vrfVerifierVerifyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vrfVerifierVerifyCall) DoAndReturn(f func(types.NodeID, []byte, types.VrfSignature) bool) *vrfVerifierVerifyCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockRolacle is a mock of Rolacle interface.
type MockRolacle struct {
	ctrl     *gomock.Controller
	recorder *MockRolacleMockRecorder
}

// MockRolacleMockRecorder is the mock recorder for MockRolacle.
type MockRolacleMockRecorder struct {
	mock *MockRolacle
}

// NewMockRolacle creates a new mock instance.
func NewMockRolacle(ctrl *gomock.Controller) *MockRolacle {
	mock := &MockRolacle{ctrl: ctrl}
	mock.recorder = &MockRolacleMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRolacle) EXPECT() *MockRolacleMockRecorder {
	return m.recorder
}

// CalcEligibility mocks base method.
func (m *MockRolacle) CalcEligibility(arg0 context.Context, arg1 types.LayerID, arg2 uint32, arg3 int, arg4 types.NodeID, arg5 types.VrfSignature) (uint16, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CalcEligibility", arg0, arg1, arg2, arg3, arg4, arg5)
	ret0, _ := ret[0].(uint16)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CalcEligibility indicates an expected call of CalcEligibility.
func (mr *MockRolacleMockRecorder) CalcEligibility(arg0, arg1, arg2, arg3, arg4, arg5 any) *RolacleCalcEligibilityCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CalcEligibility", reflect.TypeOf((*MockRolacle)(nil).CalcEligibility), arg0, arg1, arg2, arg3, arg4, arg5)
	return &RolacleCalcEligibilityCall{Call: call}
}

// RolacleCalcEligibilityCall wrap *gomock.Call
type RolacleCalcEligibilityCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *RolacleCalcEligibilityCall) Return(arg0 uint16, arg1 error) *RolacleCalcEligibilityCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *RolacleCalcEligibilityCall) Do(f func(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature) (uint16, error)) *RolacleCalcEligibilityCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *RolacleCalcEligibilityCall) DoAndReturn(f func(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature) (uint16, error)) *RolacleCalcEligibilityCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// IsIdentityActiveOnConsensusView mocks base method.
func (m *MockRolacle) IsIdentityActiveOnConsensusView(arg0 context.Context, arg1 types.NodeID, arg2 types.LayerID) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsIdentityActiveOnConsensusView", arg0, arg1, arg2)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// IsIdentityActiveOnConsensusView indicates an expected call of IsIdentityActiveOnConsensusView.
func (mr *MockRolacleMockRecorder) IsIdentityActiveOnConsensusView(arg0, arg1, arg2 any) *RolacleIsIdentityActiveOnConsensusViewCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsIdentityActiveOnConsensusView", reflect.TypeOf((*MockRolacle)(nil).IsIdentityActiveOnConsensusView), arg0, arg1, arg2)
	return &RolacleIsIdentityActiveOnConsensusViewCall{Call: call}
}

// RolacleIsIdentityActiveOnConsensusViewCall wrap *gomock.Call
type RolacleIsIdentityActiveOnConsensusViewCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *RolacleIsIdentityActiveOnConsensusViewCall) Return(arg0 bool, arg1 error) *RolacleIsIdentityActiveOnConsensusViewCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *RolacleIsIdentityActiveOnConsensusViewCall) Do(f func(context.Context, types.NodeID, types.LayerID) (bool, error)) *RolacleIsIdentityActiveOnConsensusViewCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *RolacleIsIdentityActiveOnConsensusViewCall) DoAndReturn(f func(context.Context, types.NodeID, types.LayerID) (bool, error)) *RolacleIsIdentityActiveOnConsensusViewCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Proof mocks base method.
func (m *MockRolacle) Proof(arg0 context.Context, arg1 *signing.VRFSigner, arg2 types.LayerID, arg3 uint32) (types.VrfSignature, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Proof", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(types.VrfSignature)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Proof indicates an expected call of Proof.
func (mr *MockRolacleMockRecorder) Proof(arg0, arg1, arg2, arg3 any) *RolacleProofCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Proof", reflect.TypeOf((*MockRolacle)(nil).Proof), arg0, arg1, arg2, arg3)
	return &RolacleProofCall{Call: call}
}

// RolacleProofCall wrap *gomock.Call
type RolacleProofCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *RolacleProofCall) Return(arg0 types.VrfSignature, arg1 error) *RolacleProofCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *RolacleProofCall) Do(f func(context.Context, *signing.VRFSigner, types.LayerID, uint32) (types.VrfSignature, error)) *RolacleProofCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *RolacleProofCall) DoAndReturn(f func(context.Context, *signing.VRFSigner, types.LayerID, uint32) (types.VrfSignature, error)) *RolacleProofCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Validate mocks base method.
func (m *MockRolacle) Validate(arg0 context.Context, arg1 types.LayerID, arg2 uint32, arg3 int, arg4 types.NodeID, arg5 types.VrfSignature, arg6 uint16) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validate", arg0, arg1, arg2, arg3, arg4, arg5, arg6)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Validate indicates an expected call of Validate.
func (mr *MockRolacleMockRecorder) Validate(arg0, arg1, arg2, arg3, arg4, arg5, arg6 any) *RolacleValidateCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validate", reflect.TypeOf((*MockRolacle)(nil).Validate), arg0, arg1, arg2, arg3, arg4, arg5, arg6)
	return &RolacleValidateCall{Call: call}
}

// RolacleValidateCall wrap *gomock.Call
type RolacleValidateCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *RolacleValidateCall) Return(arg0 bool, arg1 error) *RolacleValidateCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *RolacleValidateCall) Do(f func(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature, uint16) (bool, error)) *RolacleValidateCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *RolacleValidateCall) DoAndReturn(f func(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature, uint16) (bool, error)) *RolacleValidateCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
