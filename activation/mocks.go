// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package activation is a generated GoMock package.
package activation

import (
	context "context"
	reflect "reflect"
	time "time"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	shared "github.com/spacemeshos/post/shared"
	verifying "github.com/spacemeshos/post/verifying"
)

// MockAtxReceiver is a mock of AtxReceiver interface.
type MockAtxReceiver struct {
	ctrl     *gomock.Controller
	recorder *MockAtxReceiverMockRecorder
}

// MockAtxReceiverMockRecorder is the mock recorder for MockAtxReceiver.
type MockAtxReceiverMockRecorder struct {
	mock *MockAtxReceiver
}

// NewMockAtxReceiver creates a new mock instance.
func NewMockAtxReceiver(ctrl *gomock.Controller) *MockAtxReceiver {
	mock := &MockAtxReceiver{ctrl: ctrl}
	mock.recorder = &MockAtxReceiverMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAtxReceiver) EXPECT() *MockAtxReceiverMockRecorder {
	return m.recorder
}

// OnAtx mocks base method.
func (m *MockAtxReceiver) OnAtx(arg0 *types.ActivationTxHeader) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnAtx", arg0)
}

// OnAtx indicates an expected call of OnAtx.
func (mr *MockAtxReceiverMockRecorder) OnAtx(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnAtx", reflect.TypeOf((*MockAtxReceiver)(nil).OnAtx), arg0)
}

// MockPostVerifier is a mock of PostVerifier interface.
type MockPostVerifier struct {
	ctrl     *gomock.Controller
	recorder *MockPostVerifierMockRecorder
}

// MockPostVerifierMockRecorder is the mock recorder for MockPostVerifier.
type MockPostVerifierMockRecorder struct {
	mock *MockPostVerifier
}

// NewMockPostVerifier creates a new mock instance.
func NewMockPostVerifier(ctrl *gomock.Controller) *MockPostVerifier {
	mock := &MockPostVerifier{ctrl: ctrl}
	mock.recorder = &MockPostVerifierMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPostVerifier) EXPECT() *MockPostVerifierMockRecorder {
	return m.recorder
}

// Verify mocks base method.
func (m_2 *MockPostVerifier) Verify(ctx context.Context, p *shared.Proof, m *shared.ProofMetadata, opts ...verifying.OptionFunc) error {
	m_2.ctrl.T.Helper()
	varargs := []interface{}{ctx, p, m}
	for _, a := range opts {
		varargs = append(varargs, a)
	}
	ret := m_2.ctrl.Call(m_2, "Verify", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// Verify indicates an expected call of Verify.
func (mr *MockPostVerifierMockRecorder) Verify(ctx, p, m interface{}, opts ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{ctx, p, m}, opts...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Verify", reflect.TypeOf((*MockPostVerifier)(nil).Verify), varargs...)
}

// MocknipostValidator is a mock of nipostValidator interface.
type MocknipostValidator struct {
	ctrl     *gomock.Controller
	recorder *MocknipostValidatorMockRecorder
}

// MocknipostValidatorMockRecorder is the mock recorder for MocknipostValidator.
type MocknipostValidatorMockRecorder struct {
	mock *MocknipostValidator
}

// NewMocknipostValidator creates a new mock instance.
func NewMocknipostValidator(ctrl *gomock.Controller) *MocknipostValidator {
	mock := &MocknipostValidator{ctrl: ctrl}
	mock.recorder = &MocknipostValidatorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocknipostValidator) EXPECT() *MocknipostValidatorMockRecorder {
	return m.recorder
}

// InitialNIPostChallenge mocks base method.
func (m *MocknipostValidator) InitialNIPostChallenge(challenge *types.NIPostChallenge, atxs atxProvider, goldenATXID types.ATXID, expectedPostIndices []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "InitialNIPostChallenge", challenge, atxs, goldenATXID, expectedPostIndices)
	ret0, _ := ret[0].(error)
	return ret0
}

// InitialNIPostChallenge indicates an expected call of InitialNIPostChallenge.
func (mr *MocknipostValidatorMockRecorder) InitialNIPostChallenge(challenge, atxs, goldenATXID, expectedPostIndices interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InitialNIPostChallenge", reflect.TypeOf((*MocknipostValidator)(nil).InitialNIPostChallenge), challenge, atxs, goldenATXID, expectedPostIndices)
}

// NIPost mocks base method.
func (m *MocknipostValidator) NIPost(nodeId types.NodeID, atxId types.ATXID, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint32, opts ...verifying.OptionFunc) (uint64, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{nodeId, atxId, NIPost, expectedChallenge, numUnits}
	for _, a := range opts {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "NIPost", varargs...)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NIPost indicates an expected call of NIPost.
func (mr *MocknipostValidatorMockRecorder) NIPost(nodeId, atxId, NIPost, expectedChallenge, numUnits interface{}, opts ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{nodeId, atxId, NIPost, expectedChallenge, numUnits}, opts...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NIPost", reflect.TypeOf((*MocknipostValidator)(nil).NIPost), varargs...)
}

// NIPostChallenge mocks base method.
func (m *MocknipostValidator) NIPostChallenge(challenge *types.NIPostChallenge, atxs atxProvider, nodeID types.NodeID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NIPostChallenge", challenge, atxs, nodeID)
	ret0, _ := ret[0].(error)
	return ret0
}

// NIPostChallenge indicates an expected call of NIPostChallenge.
func (mr *MocknipostValidatorMockRecorder) NIPostChallenge(challenge, atxs, nodeID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NIPostChallenge", reflect.TypeOf((*MocknipostValidator)(nil).NIPostChallenge), challenge, atxs, nodeID)
}

// NumUnits mocks base method.
func (m *MocknipostValidator) NumUnits(cfg *PostConfig, numUnits uint32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NumUnits", cfg, numUnits)
	ret0, _ := ret[0].(error)
	return ret0
}

// NumUnits indicates an expected call of NumUnits.
func (mr *MocknipostValidatorMockRecorder) NumUnits(cfg, numUnits interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NumUnits", reflect.TypeOf((*MocknipostValidator)(nil).NumUnits), cfg, numUnits)
}

// PositioningAtx mocks base method.
func (m *MocknipostValidator) PositioningAtx(id *types.ATXID, atxs atxProvider, goldenATXID types.ATXID, pubepoch types.EpochID, layersPerEpoch uint32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PositioningAtx", id, atxs, goldenATXID, pubepoch, layersPerEpoch)
	ret0, _ := ret[0].(error)
	return ret0
}

// PositioningAtx indicates an expected call of PositioningAtx.
func (mr *MocknipostValidatorMockRecorder) PositioningAtx(id, atxs, goldenATXID, pubepoch, layersPerEpoch interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PositioningAtx", reflect.TypeOf((*MocknipostValidator)(nil).PositioningAtx), id, atxs, goldenATXID, pubepoch, layersPerEpoch)
}

// Post mocks base method.
func (m *MocknipostValidator) Post(nodeId types.NodeID, atxId types.ATXID, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint32, opts ...verifying.OptionFunc) error {
	m.ctrl.T.Helper()
	varargs := []interface{}{nodeId, atxId, Post, PostMetadata, numUnits}
	for _, a := range opts {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "Post", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// Post indicates an expected call of Post.
func (mr *MocknipostValidatorMockRecorder) Post(nodeId, atxId, Post, PostMetadata, numUnits interface{}, opts ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{nodeId, atxId, Post, PostMetadata, numUnits}, opts...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Post", reflect.TypeOf((*MocknipostValidator)(nil).Post), varargs...)
}

// PostMetadata mocks base method.
func (m *MocknipostValidator) PostMetadata(cfg *PostConfig, metadata *types.PostMetadata) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PostMetadata", cfg, metadata)
	ret0, _ := ret[0].(error)
	return ret0
}

// PostMetadata indicates an expected call of PostMetadata.
func (mr *MocknipostValidatorMockRecorder) PostMetadata(cfg, metadata interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PostMetadata", reflect.TypeOf((*MocknipostValidator)(nil).PostMetadata), cfg, metadata)
}

// VRFNonce mocks base method.
func (m *MocknipostValidator) VRFNonce(nodeId types.NodeID, commitmentAtxId types.ATXID, vrfNonce *types.VRFPostIndex, PostMetadata *types.PostMetadata, numUnits uint32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VRFNonce", nodeId, commitmentAtxId, vrfNonce, PostMetadata, numUnits)
	ret0, _ := ret[0].(error)
	return ret0
}

// VRFNonce indicates an expected call of VRFNonce.
func (mr *MocknipostValidatorMockRecorder) VRFNonce(nodeId, commitmentAtxId, vrfNonce, PostMetadata, numUnits interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VRFNonce", reflect.TypeOf((*MocknipostValidator)(nil).VRFNonce), nodeId, commitmentAtxId, vrfNonce, PostMetadata, numUnits)
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
func (m *MocklayerClock) AwaitLayer(layerID types.LayerID) chan struct{} {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AwaitLayer", layerID)
	ret0, _ := ret[0].(chan struct{})
	return ret0
}

// AwaitLayer indicates an expected call of AwaitLayer.
func (mr *MocklayerClockMockRecorder) AwaitLayer(layerID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AwaitLayer", reflect.TypeOf((*MocklayerClock)(nil).AwaitLayer), layerID)
}

// CurrentLayer mocks base method.
func (m *MocklayerClock) CurrentLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// CurrentLayer indicates an expected call of CurrentLayer.
func (mr *MocklayerClockMockRecorder) CurrentLayer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentLayer", reflect.TypeOf((*MocklayerClock)(nil).CurrentLayer))
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

// MocknipostBuilder is a mock of nipostBuilder interface.
type MocknipostBuilder struct {
	ctrl     *gomock.Controller
	recorder *MocknipostBuilderMockRecorder
}

// MocknipostBuilderMockRecorder is the mock recorder for MocknipostBuilder.
type MocknipostBuilderMockRecorder struct {
	mock *MocknipostBuilder
}

// NewMocknipostBuilder creates a new mock instance.
func NewMocknipostBuilder(ctrl *gomock.Controller) *MocknipostBuilder {
	mock := &MocknipostBuilder{ctrl: ctrl}
	mock.recorder = &MocknipostBuilderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocknipostBuilder) EXPECT() *MocknipostBuilderMockRecorder {
	return m.recorder
}

// BuildNIPost mocks base method.
func (m *MocknipostBuilder) BuildNIPost(ctx context.Context, challenge *types.NIPostChallenge) (*types.NIPost, time.Duration, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BuildNIPost", ctx, challenge)
	ret0, _ := ret[0].(*types.NIPost)
	ret1, _ := ret[1].(time.Duration)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// BuildNIPost indicates an expected call of BuildNIPost.
func (mr *MocknipostBuilderMockRecorder) BuildNIPost(ctx, challenge interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BuildNIPost", reflect.TypeOf((*MocknipostBuilder)(nil).BuildNIPost), ctx, challenge)
}

// DataDir mocks base method.
func (m *MocknipostBuilder) DataDir() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DataDir")
	ret0, _ := ret[0].(string)
	return ret0
}

// DataDir indicates an expected call of DataDir.
func (mr *MocknipostBuilderMockRecorder) DataDir() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DataDir", reflect.TypeOf((*MocknipostBuilder)(nil).DataDir))
}

// UpdatePoETProvers mocks base method.
func (m *MocknipostBuilder) UpdatePoETProvers(arg0 []PoetProvingServiceClient) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "UpdatePoETProvers", arg0)
}

// UpdatePoETProvers indicates an expected call of UpdatePoETProvers.
func (mr *MocknipostBuilderMockRecorder) UpdatePoETProvers(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdatePoETProvers", reflect.TypeOf((*MocknipostBuilder)(nil).UpdatePoETProvers), arg0)
}

// MockatxHandler is a mock of atxHandler interface.
type MockatxHandler struct {
	ctrl     *gomock.Controller
	recorder *MockatxHandlerMockRecorder
}

// MockatxHandlerMockRecorder is the mock recorder for MockatxHandler.
type MockatxHandlerMockRecorder struct {
	mock *MockatxHandler
}

// NewMockatxHandler creates a new mock instance.
func NewMockatxHandler(ctrl *gomock.Controller) *MockatxHandler {
	mock := &MockatxHandler{ctrl: ctrl}
	mock.recorder = &MockatxHandlerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockatxHandler) EXPECT() *MockatxHandlerMockRecorder {
	return m.recorder
}

// AwaitAtx mocks base method.
func (m *MockatxHandler) AwaitAtx(id types.ATXID) chan struct{} {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AwaitAtx", id)
	ret0, _ := ret[0].(chan struct{})
	return ret0
}

// AwaitAtx indicates an expected call of AwaitAtx.
func (mr *MockatxHandlerMockRecorder) AwaitAtx(id interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AwaitAtx", reflect.TypeOf((*MockatxHandler)(nil).AwaitAtx), id)
}

// GetPosAtxID mocks base method.
func (m *MockatxHandler) GetPosAtxID() (types.ATXID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPosAtxID")
	ret0, _ := ret[0].(types.ATXID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetPosAtxID indicates an expected call of GetPosAtxID.
func (mr *MockatxHandlerMockRecorder) GetPosAtxID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPosAtxID", reflect.TypeOf((*MockatxHandler)(nil).GetPosAtxID))
}

// UnsubscribeAtx mocks base method.
func (m *MockatxHandler) UnsubscribeAtx(id types.ATXID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "UnsubscribeAtx", id)
}

// UnsubscribeAtx indicates an expected call of UnsubscribeAtx.
func (mr *MockatxHandlerMockRecorder) UnsubscribeAtx(id interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UnsubscribeAtx", reflect.TypeOf((*MockatxHandler)(nil).UnsubscribeAtx), id)
}

// Mocksyncer is a mock of syncer interface.
type Mocksyncer struct {
	ctrl     *gomock.Controller
	recorder *MocksyncerMockRecorder
}

// MocksyncerMockRecorder is the mock recorder for Mocksyncer.
type MocksyncerMockRecorder struct {
	mock *Mocksyncer
}

// NewMocksyncer creates a new mock instance.
func NewMocksyncer(ctrl *gomock.Controller) *Mocksyncer {
	mock := &Mocksyncer{ctrl: ctrl}
	mock.recorder = &MocksyncerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mocksyncer) EXPECT() *MocksyncerMockRecorder {
	return m.recorder
}

// RegisterForATXSynced mocks base method.
func (m *Mocksyncer) RegisterForATXSynced() chan struct{} {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterForATXSynced")
	ret0, _ := ret[0].(chan struct{})
	return ret0
}

// RegisterForATXSynced indicates an expected call of RegisterForATXSynced.
func (mr *MocksyncerMockRecorder) RegisterForATXSynced() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterForATXSynced", reflect.TypeOf((*Mocksyncer)(nil).RegisterForATXSynced))
}

// MockatxProvider is a mock of atxProvider interface.
type MockatxProvider struct {
	ctrl     *gomock.Controller
	recorder *MockatxProviderMockRecorder
}

// MockatxProviderMockRecorder is the mock recorder for MockatxProvider.
type MockatxProviderMockRecorder struct {
	mock *MockatxProvider
}

// NewMockatxProvider creates a new mock instance.
func NewMockatxProvider(ctrl *gomock.Controller) *MockatxProvider {
	mock := &MockatxProvider{ctrl: ctrl}
	mock.recorder = &MockatxProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockatxProvider) EXPECT() *MockatxProviderMockRecorder {
	return m.recorder
}

// GetAtxHeader mocks base method.
func (m *MockatxProvider) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAtxHeader", id)
	ret0, _ := ret[0].(*types.ActivationTxHeader)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAtxHeader indicates an expected call of GetAtxHeader.
func (mr *MockatxProviderMockRecorder) GetAtxHeader(id interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAtxHeader", reflect.TypeOf((*MockatxProvider)(nil).GetAtxHeader), id)
}

// MockpostSetupProvider is a mock of postSetupProvider interface.
type MockpostSetupProvider struct {
	ctrl     *gomock.Controller
	recorder *MockpostSetupProviderMockRecorder
}

// MockpostSetupProviderMockRecorder is the mock recorder for MockpostSetupProvider.
type MockpostSetupProviderMockRecorder struct {
	mock *MockpostSetupProvider
}

// NewMockpostSetupProvider creates a new mock instance.
func NewMockpostSetupProvider(ctrl *gomock.Controller) *MockpostSetupProvider {
	mock := &MockpostSetupProvider{ctrl: ctrl}
	mock.recorder = &MockpostSetupProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpostSetupProvider) EXPECT() *MockpostSetupProviderMockRecorder {
	return m.recorder
}

// Benchmark mocks base method.
func (m *MockpostSetupProvider) Benchmark(p PostSetupProvider) (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Benchmark", p)
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Benchmark indicates an expected call of Benchmark.
func (mr *MockpostSetupProviderMockRecorder) Benchmark(p interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Benchmark", reflect.TypeOf((*MockpostSetupProvider)(nil).Benchmark), p)
}

// CommitmentAtx mocks base method.
func (m *MockpostSetupProvider) CommitmentAtx() (types.ATXID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CommitmentAtx")
	ret0, _ := ret[0].(types.ATXID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CommitmentAtx indicates an expected call of CommitmentAtx.
func (mr *MockpostSetupProviderMockRecorder) CommitmentAtx() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CommitmentAtx", reflect.TypeOf((*MockpostSetupProvider)(nil).CommitmentAtx))
}

// Config mocks base method.
func (m *MockpostSetupProvider) Config() PostConfig {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Config")
	ret0, _ := ret[0].(PostConfig)
	return ret0
}

// Config indicates an expected call of Config.
func (mr *MockpostSetupProviderMockRecorder) Config() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Config", reflect.TypeOf((*MockpostSetupProvider)(nil).Config))
}

// GenerateProof mocks base method.
func (m *MockpostSetupProvider) GenerateProof(ctx context.Context, challenge []byte) (*types.Post, *types.PostMetadata, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GenerateProof", ctx, challenge)
	ret0, _ := ret[0].(*types.Post)
	ret1, _ := ret[1].(*types.PostMetadata)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GenerateProof indicates an expected call of GenerateProof.
func (mr *MockpostSetupProviderMockRecorder) GenerateProof(ctx, challenge interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GenerateProof", reflect.TypeOf((*MockpostSetupProvider)(nil).GenerateProof), ctx, challenge)
}

// LastOpts mocks base method.
func (m *MockpostSetupProvider) LastOpts() *PostSetupOpts {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LastOpts")
	ret0, _ := ret[0].(*PostSetupOpts)
	return ret0
}

// LastOpts indicates an expected call of LastOpts.
func (mr *MockpostSetupProviderMockRecorder) LastOpts() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LastOpts", reflect.TypeOf((*MockpostSetupProvider)(nil).LastOpts))
}

// PrepareInitializer mocks base method.
func (m *MockpostSetupProvider) PrepareInitializer(ctx context.Context, opts PostSetupOpts) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PrepareInitializer", ctx, opts)
	ret0, _ := ret[0].(error)
	return ret0
}

// PrepareInitializer indicates an expected call of PrepareInitializer.
func (mr *MockpostSetupProviderMockRecorder) PrepareInitializer(ctx, opts interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PrepareInitializer", reflect.TypeOf((*MockpostSetupProvider)(nil).PrepareInitializer), ctx, opts)
}

// Providers mocks base method.
func (m *MockpostSetupProvider) Providers() ([]PostSetupProvider, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Providers")
	ret0, _ := ret[0].([]PostSetupProvider)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Providers indicates an expected call of Providers.
func (mr *MockpostSetupProviderMockRecorder) Providers() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Providers", reflect.TypeOf((*MockpostSetupProvider)(nil).Providers))
}

// Reset mocks base method.
func (m *MockpostSetupProvider) Reset() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Reset")
	ret0, _ := ret[0].(error)
	return ret0
}

// Reset indicates an expected call of Reset.
func (mr *MockpostSetupProviderMockRecorder) Reset() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Reset", reflect.TypeOf((*MockpostSetupProvider)(nil).Reset))
}

// StartSession mocks base method.
func (m *MockpostSetupProvider) StartSession(context context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StartSession", context)
	ret0, _ := ret[0].(error)
	return ret0
}

// StartSession indicates an expected call of StartSession.
func (mr *MockpostSetupProviderMockRecorder) StartSession(context interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StartSession", reflect.TypeOf((*MockpostSetupProvider)(nil).StartSession), context)
}

// Status mocks base method.
func (m *MockpostSetupProvider) Status() *PostSetupStatus {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Status")
	ret0, _ := ret[0].(*PostSetupStatus)
	return ret0
}

// Status indicates an expected call of Status.
func (mr *MockpostSetupProviderMockRecorder) Status() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Status", reflect.TypeOf((*MockpostSetupProvider)(nil).Status))
}

// VRFNonce mocks base method.
func (m *MockpostSetupProvider) VRFNonce() (*types.VRFPostIndex, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VRFNonce")
	ret0, _ := ret[0].(*types.VRFPostIndex)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// VRFNonce indicates an expected call of VRFNonce.
func (mr *MockpostSetupProviderMockRecorder) VRFNonce() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VRFNonce", reflect.TypeOf((*MockpostSetupProvider)(nil).VRFNonce))
}

// MockSmeshingProvider is a mock of SmeshingProvider interface.
type MockSmeshingProvider struct {
	ctrl     *gomock.Controller
	recorder *MockSmeshingProviderMockRecorder
}

// MockSmeshingProviderMockRecorder is the mock recorder for MockSmeshingProvider.
type MockSmeshingProviderMockRecorder struct {
	mock *MockSmeshingProvider
}

// NewMockSmeshingProvider creates a new mock instance.
func NewMockSmeshingProvider(ctrl *gomock.Controller) *MockSmeshingProvider {
	mock := &MockSmeshingProvider{ctrl: ctrl}
	mock.recorder = &MockSmeshingProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockSmeshingProvider) EXPECT() *MockSmeshingProviderMockRecorder {
	return m.recorder
}

// Coinbase mocks base method.
func (m *MockSmeshingProvider) Coinbase() types.Address {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Coinbase")
	ret0, _ := ret[0].(types.Address)
	return ret0
}

// Coinbase indicates an expected call of Coinbase.
func (mr *MockSmeshingProviderMockRecorder) Coinbase() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Coinbase", reflect.TypeOf((*MockSmeshingProvider)(nil).Coinbase))
}

// SetCoinbase mocks base method.
func (m *MockSmeshingProvider) SetCoinbase(coinbase types.Address) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetCoinbase", coinbase)
}

// SetCoinbase indicates an expected call of SetCoinbase.
func (mr *MockSmeshingProviderMockRecorder) SetCoinbase(coinbase interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetCoinbase", reflect.TypeOf((*MockSmeshingProvider)(nil).SetCoinbase), coinbase)
}

// SmesherID mocks base method.
func (m *MockSmeshingProvider) SmesherID() types.NodeID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SmesherID")
	ret0, _ := ret[0].(types.NodeID)
	return ret0
}

// SmesherID indicates an expected call of SmesherID.
func (mr *MockSmeshingProviderMockRecorder) SmesherID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SmesherID", reflect.TypeOf((*MockSmeshingProvider)(nil).SmesherID))
}

// Smeshing mocks base method.
func (m *MockSmeshingProvider) Smeshing() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Smeshing")
	ret0, _ := ret[0].(bool)
	return ret0
}

// Smeshing indicates an expected call of Smeshing.
func (mr *MockSmeshingProviderMockRecorder) Smeshing() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Smeshing", reflect.TypeOf((*MockSmeshingProvider)(nil).Smeshing))
}

// StartSmeshing mocks base method.
func (m *MockSmeshingProvider) StartSmeshing(arg0 types.Address, arg1 PostSetupOpts) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StartSmeshing", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// StartSmeshing indicates an expected call of StartSmeshing.
func (mr *MockSmeshingProviderMockRecorder) StartSmeshing(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StartSmeshing", reflect.TypeOf((*MockSmeshingProvider)(nil).StartSmeshing), arg0, arg1)
}

// StopSmeshing mocks base method.
func (m *MockSmeshingProvider) StopSmeshing(arg0 bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StopSmeshing", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// StopSmeshing indicates an expected call of StopSmeshing.
func (mr *MockSmeshingProviderMockRecorder) StopSmeshing(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StopSmeshing", reflect.TypeOf((*MockSmeshingProvider)(nil).StopSmeshing), arg0)
}

// UpdatePoETServers mocks base method.
func (m *MockSmeshingProvider) UpdatePoETServers(ctx context.Context, endpoints []string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdatePoETServers", ctx, endpoints)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdatePoETServers indicates an expected call of UpdatePoETServers.
func (mr *MockSmeshingProviderMockRecorder) UpdatePoETServers(ctx, endpoints interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdatePoETServers", reflect.TypeOf((*MockSmeshingProvider)(nil).UpdatePoETServers), ctx, endpoints)
}
