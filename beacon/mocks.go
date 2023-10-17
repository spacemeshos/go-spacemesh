// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go
//
// Generated by this command:
//
//	mockgen -typed -package=beacon -destination=./mocks.go -source=./interface.go
//
// Package beacon is a generated GoMock package.
package beacon

import (
	context "context"
	reflect "reflect"
	time "time"

	weakcoin "github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	p2p "github.com/spacemeshos/go-spacemesh/p2p"
	gomock "go.uber.org/mock/gomock"
)

// Mockcoin is a mock of coin interface.
type Mockcoin struct {
	ctrl     *gomock.Controller
	recorder *MockcoinMockRecorder
}

// MockcoinMockRecorder is the mock recorder for Mockcoin.
type MockcoinMockRecorder struct {
	mock *Mockcoin
}

// NewMockcoin creates a new mock instance.
func NewMockcoin(ctrl *gomock.Controller) *Mockcoin {
	mock := &Mockcoin{ctrl: ctrl}
	mock.recorder = &MockcoinMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockcoin) EXPECT() *MockcoinMockRecorder {
	return m.recorder
}

// FinishEpoch mocks base method.
func (m *Mockcoin) FinishEpoch(arg0 context.Context, arg1 types.EpochID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "FinishEpoch", arg0, arg1)
}

// FinishEpoch indicates an expected call of FinishEpoch.
func (mr *MockcoinMockRecorder) FinishEpoch(arg0, arg1 any) *coinFinishEpochCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FinishEpoch", reflect.TypeOf((*Mockcoin)(nil).FinishEpoch), arg0, arg1)
	return &coinFinishEpochCall{Call: call}
}

// coinFinishEpochCall wrap *gomock.Call
type coinFinishEpochCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *coinFinishEpochCall) Return() *coinFinishEpochCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *coinFinishEpochCall) Do(f func(context.Context, types.EpochID)) *coinFinishEpochCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *coinFinishEpochCall) DoAndReturn(f func(context.Context, types.EpochID)) *coinFinishEpochCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// FinishRound mocks base method.
func (m *Mockcoin) FinishRound(arg0 context.Context) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "FinishRound", arg0)
}

// FinishRound indicates an expected call of FinishRound.
func (mr *MockcoinMockRecorder) FinishRound(arg0 any) *coinFinishRoundCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FinishRound", reflect.TypeOf((*Mockcoin)(nil).FinishRound), arg0)
	return &coinFinishRoundCall{Call: call}
}

// coinFinishRoundCall wrap *gomock.Call
type coinFinishRoundCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *coinFinishRoundCall) Return() *coinFinishRoundCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *coinFinishRoundCall) Do(f func(context.Context)) *coinFinishRoundCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *coinFinishRoundCall) DoAndReturn(f func(context.Context)) *coinFinishRoundCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Get mocks base method.
func (m *Mockcoin) Get(arg0 context.Context, arg1 types.EpochID, arg2 types.RoundID) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0, arg1, arg2)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockcoinMockRecorder) Get(arg0, arg1, arg2 any) *coinGetCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*Mockcoin)(nil).Get), arg0, arg1, arg2)
	return &coinGetCall{Call: call}
}

// coinGetCall wrap *gomock.Call
type coinGetCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *coinGetCall) Return(arg0 bool, arg1 error) *coinGetCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *coinGetCall) Do(f func(context.Context, types.EpochID, types.RoundID) (bool, error)) *coinGetCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *coinGetCall) DoAndReturn(f func(context.Context, types.EpochID, types.RoundID) (bool, error)) *coinGetCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// HandleProposal mocks base method.
func (m *Mockcoin) HandleProposal(arg0 context.Context, arg1 p2p.Peer, arg2 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandleProposal", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// HandleProposal indicates an expected call of HandleProposal.
func (mr *MockcoinMockRecorder) HandleProposal(arg0, arg1, arg2 any) *coinHandleProposalCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleProposal", reflect.TypeOf((*Mockcoin)(nil).HandleProposal), arg0, arg1, arg2)
	return &coinHandleProposalCall{Call: call}
}

// coinHandleProposalCall wrap *gomock.Call
type coinHandleProposalCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *coinHandleProposalCall) Return(arg0 error) *coinHandleProposalCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *coinHandleProposalCall) Do(f func(context.Context, p2p.Peer, []byte) error) *coinHandleProposalCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *coinHandleProposalCall) DoAndReturn(f func(context.Context, p2p.Peer, []byte) error) *coinHandleProposalCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// StartEpoch mocks base method.
func (m *Mockcoin) StartEpoch(arg0 context.Context, arg1 types.EpochID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "StartEpoch", arg0, arg1)
}

// StartEpoch indicates an expected call of StartEpoch.
func (mr *MockcoinMockRecorder) StartEpoch(arg0, arg1 any) *coinStartEpochCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StartEpoch", reflect.TypeOf((*Mockcoin)(nil).StartEpoch), arg0, arg1)
	return &coinStartEpochCall{Call: call}
}

// coinStartEpochCall wrap *gomock.Call
type coinStartEpochCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *coinStartEpochCall) Return() *coinStartEpochCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *coinStartEpochCall) Do(f func(context.Context, types.EpochID)) *coinStartEpochCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *coinStartEpochCall) DoAndReturn(f func(context.Context, types.EpochID)) *coinStartEpochCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// StartRound mocks base method.
func (m *Mockcoin) StartRound(arg0 context.Context, arg1 types.RoundID, arg2 []weakcoin.Participant) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "StartRound", arg0, arg1, arg2)
}

// StartRound indicates an expected call of StartRound.
func (mr *MockcoinMockRecorder) StartRound(arg0, arg1, arg2 any) *coinStartRoundCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StartRound", reflect.TypeOf((*Mockcoin)(nil).StartRound), arg0, arg1, arg2)
	return &coinStartRoundCall{Call: call}
}

// coinStartRoundCall wrap *gomock.Call
type coinStartRoundCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *coinStartRoundCall) Return() *coinStartRoundCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *coinStartRoundCall) Do(f func(context.Context, types.RoundID, []weakcoin.Participant)) *coinStartRoundCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *coinStartRoundCall) DoAndReturn(f func(context.Context, types.RoundID, []weakcoin.Participant)) *coinStartRoundCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockeligibilityChecker is a mock of eligibilityChecker interface.
type MockeligibilityChecker struct {
	ctrl     *gomock.Controller
	recorder *MockeligibilityCheckerMockRecorder
}

// MockeligibilityCheckerMockRecorder is the mock recorder for MockeligibilityChecker.
type MockeligibilityCheckerMockRecorder struct {
	mock *MockeligibilityChecker
}

// NewMockeligibilityChecker creates a new mock instance.
func NewMockeligibilityChecker(ctrl *gomock.Controller) *MockeligibilityChecker {
	mock := &MockeligibilityChecker{ctrl: ctrl}
	mock.recorder = &MockeligibilityCheckerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockeligibilityChecker) EXPECT() *MockeligibilityCheckerMockRecorder {
	return m.recorder
}

// PassStrictThreshold mocks base method.
func (m *MockeligibilityChecker) PassStrictThreshold(arg0 types.VrfSignature) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PassStrictThreshold", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// PassStrictThreshold indicates an expected call of PassStrictThreshold.
func (mr *MockeligibilityCheckerMockRecorder) PassStrictThreshold(arg0 any) *eligibilityCheckerPassStrictThresholdCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PassStrictThreshold", reflect.TypeOf((*MockeligibilityChecker)(nil).PassStrictThreshold), arg0)
	return &eligibilityCheckerPassStrictThresholdCall{Call: call}
}

// eligibilityCheckerPassStrictThresholdCall wrap *gomock.Call
type eligibilityCheckerPassStrictThresholdCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *eligibilityCheckerPassStrictThresholdCall) Return(arg0 bool) *eligibilityCheckerPassStrictThresholdCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *eligibilityCheckerPassStrictThresholdCall) Do(f func(types.VrfSignature) bool) *eligibilityCheckerPassStrictThresholdCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *eligibilityCheckerPassStrictThresholdCall) DoAndReturn(f func(types.VrfSignature) bool) *eligibilityCheckerPassStrictThresholdCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// PassThreshold mocks base method.
func (m *MockeligibilityChecker) PassThreshold(arg0 types.VrfSignature) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PassThreshold", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// PassThreshold indicates an expected call of PassThreshold.
func (mr *MockeligibilityCheckerMockRecorder) PassThreshold(arg0 any) *eligibilityCheckerPassThresholdCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PassThreshold", reflect.TypeOf((*MockeligibilityChecker)(nil).PassThreshold), arg0)
	return &eligibilityCheckerPassThresholdCall{Call: call}
}

// eligibilityCheckerPassThresholdCall wrap *gomock.Call
type eligibilityCheckerPassThresholdCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *eligibilityCheckerPassThresholdCall) Return(arg0 bool) *eligibilityCheckerPassThresholdCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *eligibilityCheckerPassThresholdCall) Do(f func(types.VrfSignature) bool) *eligibilityCheckerPassThresholdCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *eligibilityCheckerPassThresholdCall) DoAndReturn(f func(types.VrfSignature) bool) *eligibilityCheckerPassThresholdCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MocklayerClock is a mock of layerClock interface.
type MocklayerClock struct {
	ctrl     *gomock.Controller
	recorder *MocklayerClockMockRecorder
}

// MocklayerClockMockRecorder is the mock recorder for MocklayerClock.
type MocklayerClockMockRecorder struct {
	mock *MocklayerClock
}

// NewMocklayerClock creates a new mock instance.
func NewMocklayerClock(ctrl *gomock.Controller) *MocklayerClock {
	mock := &MocklayerClock{ctrl: ctrl}
	mock.recorder = &MocklayerClockMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocklayerClock) EXPECT() *MocklayerClockMockRecorder {
	return m.recorder
}

// AwaitLayer mocks base method.
func (m *MocklayerClock) AwaitLayer(arg0 types.LayerID) <-chan struct{} {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AwaitLayer", arg0)
	ret0, _ := ret[0].(<-chan struct{})
	return ret0
}

// AwaitLayer indicates an expected call of AwaitLayer.
func (mr *MocklayerClockMockRecorder) AwaitLayer(arg0 any) *layerClockAwaitLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AwaitLayer", reflect.TypeOf((*MocklayerClock)(nil).AwaitLayer), arg0)
	return &layerClockAwaitLayerCall{Call: call}
}

// layerClockAwaitLayerCall wrap *gomock.Call
type layerClockAwaitLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *layerClockAwaitLayerCall) Return(arg0 <-chan struct{}) *layerClockAwaitLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *layerClockAwaitLayerCall) Do(f func(types.LayerID) <-chan struct{}) *layerClockAwaitLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *layerClockAwaitLayerCall) DoAndReturn(f func(types.LayerID) <-chan struct{}) *layerClockAwaitLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// CurrentLayer mocks base method.
func (m *MocklayerClock) CurrentLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// CurrentLayer indicates an expected call of CurrentLayer.
func (mr *MocklayerClockMockRecorder) CurrentLayer() *layerClockCurrentLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentLayer", reflect.TypeOf((*MocklayerClock)(nil).CurrentLayer))
	return &layerClockCurrentLayerCall{Call: call}
}

// layerClockCurrentLayerCall wrap *gomock.Call
type layerClockCurrentLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *layerClockCurrentLayerCall) Return(arg0 types.LayerID) *layerClockCurrentLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *layerClockCurrentLayerCall) Do(f func() types.LayerID) *layerClockCurrentLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *layerClockCurrentLayerCall) DoAndReturn(f func() types.LayerID) *layerClockCurrentLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// LayerToTime mocks base method.
func (m *MocklayerClock) LayerToTime(arg0 types.LayerID) time.Time {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LayerToTime", arg0)
	ret0, _ := ret[0].(time.Time)
	return ret0
}

// LayerToTime indicates an expected call of LayerToTime.
func (mr *MocklayerClockMockRecorder) LayerToTime(arg0 any) *layerClockLayerToTimeCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LayerToTime", reflect.TypeOf((*MocklayerClock)(nil).LayerToTime), arg0)
	return &layerClockLayerToTimeCall{Call: call}
}

// layerClockLayerToTimeCall wrap *gomock.Call
type layerClockLayerToTimeCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *layerClockLayerToTimeCall) Return(arg0 time.Time) *layerClockLayerToTimeCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *layerClockLayerToTimeCall) Do(f func(types.LayerID) time.Time) *layerClockLayerToTimeCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *layerClockLayerToTimeCall) DoAndReturn(f func(types.LayerID) time.Time) *layerClockLayerToTimeCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockvrfSigner is a mock of vrfSigner interface.
type MockvrfSigner struct {
	ctrl     *gomock.Controller
	recorder *MockvrfSignerMockRecorder
}

// MockvrfSignerMockRecorder is the mock recorder for MockvrfSigner.
type MockvrfSignerMockRecorder struct {
	mock *MockvrfSigner
}

// NewMockvrfSigner creates a new mock instance.
func NewMockvrfSigner(ctrl *gomock.Controller) *MockvrfSigner {
	mock := &MockvrfSigner{ctrl: ctrl}
	mock.recorder = &MockvrfSignerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockvrfSigner) EXPECT() *MockvrfSignerMockRecorder {
	return m.recorder
}

// LittleEndian mocks base method.
func (m *MockvrfSigner) LittleEndian() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LittleEndian")
	ret0, _ := ret[0].(bool)
	return ret0
}

// LittleEndian indicates an expected call of LittleEndian.
func (mr *MockvrfSignerMockRecorder) LittleEndian() *vrfSignerLittleEndianCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LittleEndian", reflect.TypeOf((*MockvrfSigner)(nil).LittleEndian))
	return &vrfSignerLittleEndianCall{Call: call}
}

// vrfSignerLittleEndianCall wrap *gomock.Call
type vrfSignerLittleEndianCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vrfSignerLittleEndianCall) Return(arg0 bool) *vrfSignerLittleEndianCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vrfSignerLittleEndianCall) Do(f func() bool) *vrfSignerLittleEndianCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vrfSignerLittleEndianCall) DoAndReturn(f func() bool) *vrfSignerLittleEndianCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// NodeID mocks base method.
func (m *MockvrfSigner) NodeID() types.NodeID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NodeID")
	ret0, _ := ret[0].(types.NodeID)
	return ret0
}

// NodeID indicates an expected call of NodeID.
func (mr *MockvrfSignerMockRecorder) NodeID() *vrfSignerNodeIDCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NodeID", reflect.TypeOf((*MockvrfSigner)(nil).NodeID))
	return &vrfSignerNodeIDCall{Call: call}
}

// vrfSignerNodeIDCall wrap *gomock.Call
type vrfSignerNodeIDCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vrfSignerNodeIDCall) Return(arg0 types.NodeID) *vrfSignerNodeIDCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vrfSignerNodeIDCall) Do(f func() types.NodeID) *vrfSignerNodeIDCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vrfSignerNodeIDCall) DoAndReturn(f func() types.NodeID) *vrfSignerNodeIDCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Sign mocks base method.
func (m *MockvrfSigner) Sign(msg []byte) types.VrfSignature {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Sign", msg)
	ret0, _ := ret[0].(types.VrfSignature)
	return ret0
}

// Sign indicates an expected call of Sign.
func (mr *MockvrfSignerMockRecorder) Sign(msg any) *vrfSignerSignCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Sign", reflect.TypeOf((*MockvrfSigner)(nil).Sign), msg)
	return &vrfSignerSignCall{Call: call}
}

// vrfSignerSignCall wrap *gomock.Call
type vrfSignerSignCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *vrfSignerSignCall) Return(arg0 types.VrfSignature) *vrfSignerSignCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *vrfSignerSignCall) Do(f func([]byte) types.VrfSignature) *vrfSignerSignCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *vrfSignerSignCall) DoAndReturn(f func([]byte) types.VrfSignature) *vrfSignerSignCall {
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

// MocknonceFetcher is a mock of nonceFetcher interface.
type MocknonceFetcher struct {
	ctrl     *gomock.Controller
	recorder *MocknonceFetcherMockRecorder
}

// MocknonceFetcherMockRecorder is the mock recorder for MocknonceFetcher.
type MocknonceFetcherMockRecorder struct {
	mock *MocknonceFetcher
}

// NewMocknonceFetcher creates a new mock instance.
func NewMocknonceFetcher(ctrl *gomock.Controller) *MocknonceFetcher {
	mock := &MocknonceFetcher{ctrl: ctrl}
	mock.recorder = &MocknonceFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocknonceFetcher) EXPECT() *MocknonceFetcherMockRecorder {
	return m.recorder
}

// VRFNonce mocks base method.
func (m *MocknonceFetcher) VRFNonce(arg0 types.NodeID, arg1 types.EpochID) (types.VRFPostIndex, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VRFNonce", arg0, arg1)
	ret0, _ := ret[0].(types.VRFPostIndex)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// VRFNonce indicates an expected call of VRFNonce.
func (mr *MocknonceFetcherMockRecorder) VRFNonce(arg0, arg1 any) *nonceFetcherVRFNonceCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VRFNonce", reflect.TypeOf((*MocknonceFetcher)(nil).VRFNonce), arg0, arg1)
	return &nonceFetcherVRFNonceCall{Call: call}
}

// nonceFetcherVRFNonceCall wrap *gomock.Call
type nonceFetcherVRFNonceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *nonceFetcherVRFNonceCall) Return(arg0 types.VRFPostIndex, arg1 error) *nonceFetcherVRFNonceCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *nonceFetcherVRFNonceCall) Do(f func(types.NodeID, types.EpochID) (types.VRFPostIndex, error)) *nonceFetcherVRFNonceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *nonceFetcherVRFNonceCall) DoAndReturn(f func(types.NodeID, types.EpochID) (types.VRFPostIndex, error)) *nonceFetcherVRFNonceCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
