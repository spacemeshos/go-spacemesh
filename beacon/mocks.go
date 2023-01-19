// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package beacon is a generated GoMock package.
package beacon

import (
	context "context"
	reflect "reflect"
	time "time"

	gomock "github.com/golang/mock/gomock"
	weakcoin "github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	p2p "github.com/spacemeshos/go-spacemesh/p2p"
	pubsub "github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	signing "github.com/spacemeshos/go-spacemesh/signing"
	timesync "github.com/spacemeshos/go-spacemesh/timesync"
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
func (mr *MockcoinMockRecorder) FinishEpoch(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FinishEpoch", reflect.TypeOf((*Mockcoin)(nil).FinishEpoch), arg0, arg1)
}

// FinishRound mocks base method.
func (m *Mockcoin) FinishRound(arg0 context.Context) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "FinishRound", arg0)
}

// FinishRound indicates an expected call of FinishRound.
func (mr *MockcoinMockRecorder) FinishRound(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FinishRound", reflect.TypeOf((*Mockcoin)(nil).FinishRound), arg0)
}

// Get mocks base method.
func (m *Mockcoin) Get(arg0 context.Context, arg1 types.EpochID, arg2 types.RoundID) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0, arg1, arg2)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Get indicates an expected call of Get.
func (mr *MockcoinMockRecorder) Get(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*Mockcoin)(nil).Get), arg0, arg1, arg2)
}

// HandleProposal mocks base method.
func (m *Mockcoin) HandleProposal(arg0 context.Context, arg1 p2p.Peer, arg2 []byte) pubsub.ValidationResult {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandleProposal", arg0, arg1, arg2)
	ret0, _ := ret[0].(pubsub.ValidationResult)
	return ret0
}

// HandleProposal indicates an expected call of HandleProposal.
func (mr *MockcoinMockRecorder) HandleProposal(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleProposal", reflect.TypeOf((*Mockcoin)(nil).HandleProposal), arg0, arg1, arg2)
}

// StartEpoch mocks base method.
func (m *Mockcoin) StartEpoch(arg0 context.Context, arg1 types.EpochID, arg2 weakcoin.UnitAllowances) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "StartEpoch", arg0, arg1, arg2)
}

// StartEpoch indicates an expected call of StartEpoch.
func (mr *MockcoinMockRecorder) StartEpoch(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StartEpoch", reflect.TypeOf((*Mockcoin)(nil).StartEpoch), arg0, arg1, arg2)
}

// StartRound mocks base method.
func (m *Mockcoin) StartRound(arg0 context.Context, arg1 types.RoundID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StartRound", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// StartRound indicates an expected call of StartRound.
func (mr *MockcoinMockRecorder) StartRound(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StartRound", reflect.TypeOf((*Mockcoin)(nil).StartRound), arg0, arg1)
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

// IsProposalEligible mocks base method.
func (m *MockeligibilityChecker) IsProposalEligible(arg0 []byte) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsProposalEligible", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsProposalEligible indicates an expected call of IsProposalEligible.
func (mr *MockeligibilityCheckerMockRecorder) IsProposalEligible(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsProposalEligible", reflect.TypeOf((*MockeligibilityChecker)(nil).IsProposalEligible), arg0)
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

// GetCurrentLayer mocks base method.
func (m *MocklayerClock) GetCurrentLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCurrentLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// GetCurrentLayer indicates an expected call of GetCurrentLayer.
func (mr *MocklayerClockMockRecorder) GetCurrentLayer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCurrentLayer", reflect.TypeOf((*MocklayerClock)(nil).GetCurrentLayer))
}

// LayerToTime mocks base method.
func (m *MocklayerClock) LayerToTime(arg0 types.LayerID) time.Time {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LayerToTime", arg0)
	ret0, _ := ret[0].(time.Time)
	return ret0
}

// LayerToTime indicates an expected call of LayerToTime.
func (mr *MocklayerClockMockRecorder) LayerToTime(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LayerToTime", reflect.TypeOf((*MocklayerClock)(nil).LayerToTime), arg0)
}

// Subscribe mocks base method.
func (m *MocklayerClock) Subscribe() timesync.LayerTimer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Subscribe")
	ret0, _ := ret[0].(timesync.LayerTimer)
	return ret0
}

// Subscribe indicates an expected call of Subscribe.
func (mr *MocklayerClockMockRecorder) Subscribe() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Subscribe", reflect.TypeOf((*MocklayerClock)(nil).Subscribe))
}

// Unsubscribe mocks base method.
func (m *MocklayerClock) Unsubscribe(arg0 timesync.LayerTimer) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Unsubscribe", arg0)
}

// Unsubscribe indicates an expected call of Unsubscribe.
func (mr *MocklayerClockMockRecorder) Unsubscribe(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Unsubscribe", reflect.TypeOf((*MocklayerClock)(nil).Unsubscribe), arg0)
}

// Mocksigner is a mock of signer interface.
type Mocksigner struct {
	ctrl     *gomock.Controller
	recorder *MocksignerMockRecorder
}

// MocksignerMockRecorder is the mock recorder for Mocksigner.
type MocksignerMockRecorder struct {
	mock *Mocksigner
}

// NewMocksigner creates a new mock instance.
func NewMocksigner(ctrl *gomock.Controller) *Mocksigner {
	mock := &Mocksigner{ctrl: ctrl}
	mock.recorder = &MocksignerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mocksigner) EXPECT() *MocksignerMockRecorder {
	return m.recorder
}

// NodeID mocks base method.
func (m *Mocksigner) NodeID() types.NodeID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NodeID")
	ret0, _ := ret[0].(types.NodeID)
	return ret0
}

// NodeID indicates an expected call of NodeID.
func (mr *MocksignerMockRecorder) NodeID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NodeID", reflect.TypeOf((*Mocksigner)(nil).NodeID))
}

// PublicKey mocks base method.
func (m *Mocksigner) PublicKey() *signing.PublicKey {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PublicKey")
	ret0, _ := ret[0].(*signing.PublicKey)
	return ret0
}

// PublicKey indicates an expected call of PublicKey.
func (mr *MocksignerMockRecorder) PublicKey() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PublicKey", reflect.TypeOf((*Mocksigner)(nil).PublicKey))
}

// Sign mocks base method.
func (m *Mocksigner) Sign(msg []byte) []byte {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Sign", msg)
	ret0, _ := ret[0].([]byte)
	return ret0
}

// Sign indicates an expected call of Sign.
func (mr *MocksignerMockRecorder) Sign(msg interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Sign", reflect.TypeOf((*Mocksigner)(nil).Sign), msg)
}

// MockpubKeyExtractor is a mock of pubKeyExtractor interface.
type MockpubKeyExtractor struct {
	ctrl     *gomock.Controller
	recorder *MockpubKeyExtractorMockRecorder
}

// MockpubKeyExtractorMockRecorder is the mock recorder for MockpubKeyExtractor.
type MockpubKeyExtractorMockRecorder struct {
	mock *MockpubKeyExtractor
}

// NewMockpubKeyExtractor creates a new mock instance.
func NewMockpubKeyExtractor(ctrl *gomock.Controller) *MockpubKeyExtractor {
	mock := &MockpubKeyExtractor{ctrl: ctrl}
	mock.recorder = &MockpubKeyExtractorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpubKeyExtractor) EXPECT() *MockpubKeyExtractorMockRecorder {
	return m.recorder
}

// Extract mocks base method.
func (m *MockpubKeyExtractor) Extract(arg0, arg1 []byte) (*signing.PublicKey, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Extract", arg0, arg1)
	ret0, _ := ret[0].(*signing.PublicKey)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Extract indicates an expected call of Extract.
func (mr *MockpubKeyExtractorMockRecorder) Extract(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Extract", reflect.TypeOf((*MockpubKeyExtractor)(nil).Extract), arg0, arg1)
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
func (mr *MockvrfSignerMockRecorder) LittleEndian() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LittleEndian", reflect.TypeOf((*MockvrfSigner)(nil).LittleEndian))
}

// NodeID mocks base method.
func (m *MockvrfSigner) NodeID() types.NodeID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NodeID")
	ret0, _ := ret[0].(types.NodeID)
	return ret0
}

// NodeID indicates an expected call of NodeID.
func (mr *MockvrfSignerMockRecorder) NodeID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NodeID", reflect.TypeOf((*MockvrfSigner)(nil).NodeID))
}

// PublicKey mocks base method.
func (m *MockvrfSigner) PublicKey() *signing.PublicKey {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PublicKey")
	ret0, _ := ret[0].(*signing.PublicKey)
	return ret0
}

// PublicKey indicates an expected call of PublicKey.
func (mr *MockvrfSignerMockRecorder) PublicKey() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PublicKey", reflect.TypeOf((*MockvrfSigner)(nil).PublicKey))
}

// Sign mocks base method.
func (m *MockvrfSigner) Sign(msg []byte) []byte {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Sign", msg)
	ret0, _ := ret[0].([]byte)
	return ret0
}

// Sign indicates an expected call of Sign.
func (mr *MockvrfSignerMockRecorder) Sign(msg interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Sign", reflect.TypeOf((*MockvrfSigner)(nil).Sign), msg)
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
func (m *MockvrfVerifier) Verify(nodeID types.NodeID, msg, sig []byte) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Verify", nodeID, msg, sig)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *MockvrfVerifierMockRecorder) Verify(nodeID, msg, sig interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*MockvrfVerifier)(nil).Verify), nodeID, msg, sig)
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
func (mr *MocknonceFetcherMockRecorder) VRFNonce(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VRFNonce", reflect.TypeOf((*MocknonceFetcher)(nil).VRFNonce), arg0, arg1)
}
