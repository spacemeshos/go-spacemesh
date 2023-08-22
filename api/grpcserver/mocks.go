// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package grpcserver is a generated GoMock package.
package grpcserver

import (
	context "context"
	reflect "reflect"
	time "time"

	gomock "github.com/golang/mock/gomock"
	activation "github.com/spacemeshos/go-spacemesh/activation"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	p2p "github.com/spacemeshos/go-spacemesh/p2p"
	system "github.com/spacemeshos/go-spacemesh/system"
)

// MocknetworkIdentity is a mock of networkIdentity interface.
type MocknetworkIdentity struct {
	ctrl     *gomock.Controller
	recorder *MocknetworkIdentityMockRecorder
}

// MocknetworkIdentityMockRecorder is the mock recorder for MocknetworkIdentity.
type MocknetworkIdentityMockRecorder struct {
	mock *MocknetworkIdentity
}

// NewMocknetworkIdentity creates a new mock instance.
func NewMocknetworkIdentity(ctrl *gomock.Controller) *MocknetworkIdentity {
	mock := &MocknetworkIdentity{ctrl: ctrl}
	mock.recorder = &MocknetworkIdentityMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocknetworkIdentity) EXPECT() *MocknetworkIdentityMockRecorder {
	return m.recorder
}

// ID mocks base method.
func (m *MocknetworkIdentity) ID() p2p.Peer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ID")
	ret0, _ := ret[0].(p2p.Peer)
	return ret0
}

// ID indicates an expected call of ID.
func (mr *MocknetworkIdentityMockRecorder) ID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ID", reflect.TypeOf((*MocknetworkIdentity)(nil).ID))
}

// MockconservativeState is a mock of conservativeState interface.
type MockconservativeState struct {
	ctrl     *gomock.Controller
	recorder *MockconservativeStateMockRecorder
}

// MockconservativeStateMockRecorder is the mock recorder for MockconservativeState.
type MockconservativeStateMockRecorder struct {
	mock *MockconservativeState
}

// NewMockconservativeState creates a new mock instance.
func NewMockconservativeState(ctrl *gomock.Controller) *MockconservativeState {
	mock := &MockconservativeState{ctrl: ctrl}
	mock.recorder = &MockconservativeStateMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockconservativeState) EXPECT() *MockconservativeStateMockRecorder {
	return m.recorder
}

// GetAllAccounts mocks base method.
func (m *MockconservativeState) GetAllAccounts() ([]*types.Account, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllAccounts")
	ret0, _ := ret[0].([]*types.Account)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllAccounts indicates an expected call of GetAllAccounts.
func (mr *MockconservativeStateMockRecorder) GetAllAccounts() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllAccounts", reflect.TypeOf((*MockconservativeState)(nil).GetAllAccounts))
}

// GetBalance mocks base method.
func (m *MockconservativeState) GetBalance(arg0 types.Address) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBalance", arg0)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBalance indicates an expected call of GetBalance.
func (mr *MockconservativeStateMockRecorder) GetBalance(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBalance", reflect.TypeOf((*MockconservativeState)(nil).GetBalance), arg0)
}

// GetLayerStateRoot mocks base method.
func (m *MockconservativeState) GetLayerStateRoot(arg0 types.LayerID) (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayerStateRoot", arg0)
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLayerStateRoot indicates an expected call of GetLayerStateRoot.
func (mr *MockconservativeStateMockRecorder) GetLayerStateRoot(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerStateRoot", reflect.TypeOf((*MockconservativeState)(nil).GetLayerStateRoot), arg0)
}

// GetMeshTransaction mocks base method.
func (m *MockconservativeState) GetMeshTransaction(arg0 types.TransactionID) (*types.MeshTransaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMeshTransaction", arg0)
	ret0, _ := ret[0].(*types.MeshTransaction)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetMeshTransaction indicates an expected call of GetMeshTransaction.
func (mr *MockconservativeStateMockRecorder) GetMeshTransaction(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMeshTransaction", reflect.TypeOf((*MockconservativeState)(nil).GetMeshTransaction), arg0)
}

// GetMeshTransactions mocks base method.
func (m *MockconservativeState) GetMeshTransactions(arg0 []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMeshTransactions", arg0)
	ret0, _ := ret[0].([]*types.MeshTransaction)
	ret1, _ := ret[1].(map[types.TransactionID]struct{})
	return ret0, ret1
}

// GetMeshTransactions indicates an expected call of GetMeshTransactions.
func (mr *MockconservativeStateMockRecorder) GetMeshTransactions(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMeshTransactions", reflect.TypeOf((*MockconservativeState)(nil).GetMeshTransactions), arg0)
}

// GetNonce mocks base method.
func (m *MockconservativeState) GetNonce(arg0 types.Address) (types.Nonce, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNonce", arg0)
	ret0, _ := ret[0].(types.Nonce)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetNonce indicates an expected call of GetNonce.
func (mr *MockconservativeStateMockRecorder) GetNonce(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNonce", reflect.TypeOf((*MockconservativeState)(nil).GetNonce), arg0)
}

// GetProjection mocks base method.
func (m *MockconservativeState) GetProjection(arg0 types.Address) (uint64, uint64) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProjection", arg0)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(uint64)
	return ret0, ret1
}

// GetProjection indicates an expected call of GetProjection.
func (mr *MockconservativeStateMockRecorder) GetProjection(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProjection", reflect.TypeOf((*MockconservativeState)(nil).GetProjection), arg0)
}

// GetStateRoot mocks base method.
func (m *MockconservativeState) GetStateRoot() (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetStateRoot")
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetStateRoot indicates an expected call of GetStateRoot.
func (mr *MockconservativeStateMockRecorder) GetStateRoot() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStateRoot", reflect.TypeOf((*MockconservativeState)(nil).GetStateRoot))
}

// GetTransactionsByAddress mocks base method.
func (m *MockconservativeState) GetTransactionsByAddress(arg0, arg1 types.LayerID, arg2 types.Address) ([]*types.MeshTransaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetTransactionsByAddress", arg0, arg1, arg2)
	ret0, _ := ret[0].([]*types.MeshTransaction)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetTransactionsByAddress indicates an expected call of GetTransactionsByAddress.
func (mr *MockconservativeStateMockRecorder) GetTransactionsByAddress(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetTransactionsByAddress", reflect.TypeOf((*MockconservativeState)(nil).GetTransactionsByAddress), arg0, arg1, arg2)
}

// Validation mocks base method.
func (m *MockconservativeState) Validation(raw types.RawTx) system.ValidationRequest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validation", raw)
	ret0, _ := ret[0].(system.ValidationRequest)
	return ret0
}

// Validation indicates an expected call of Validation.
func (mr *MockconservativeStateMockRecorder) Validation(raw interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validation", reflect.TypeOf((*MockconservativeState)(nil).Validation), raw)
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

// IsSynced mocks base method.
func (m *Mocksyncer) IsSynced(arg0 context.Context) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsSynced", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsSynced indicates an expected call of IsSynced.
func (mr *MocksyncerMockRecorder) IsSynced(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsSynced", reflect.TypeOf((*Mocksyncer)(nil).IsSynced), arg0)
}

// MocktxValidator is a mock of txValidator interface.
type MocktxValidator struct {
	ctrl     *gomock.Controller
	recorder *MocktxValidatorMockRecorder
}

// MocktxValidatorMockRecorder is the mock recorder for MocktxValidator.
type MocktxValidatorMockRecorder struct {
	mock *MocktxValidator
}

// NewMocktxValidator creates a new mock instance.
func NewMocktxValidator(ctrl *gomock.Controller) *MocktxValidator {
	mock := &MocktxValidator{ctrl: ctrl}
	mock.recorder = &MocktxValidatorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocktxValidator) EXPECT() *MocktxValidatorMockRecorder {
	return m.recorder
}

// VerifyAndCacheTx mocks base method.
func (m *MocktxValidator) VerifyAndCacheTx(arg0 context.Context, arg1 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VerifyAndCacheTx", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// VerifyAndCacheTx indicates an expected call of VerifyAndCacheTx.
func (mr *MocktxValidatorMockRecorder) VerifyAndCacheTx(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VerifyAndCacheTx", reflect.TypeOf((*MocktxValidator)(nil).VerifyAndCacheTx), arg0, arg1)
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

// GetFullAtx mocks base method.
func (m *MockatxProvider) GetFullAtx(id types.ATXID) (*types.VerifiedActivationTx, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetFullAtx", id)
	ret0, _ := ret[0].(*types.VerifiedActivationTx)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetFullAtx indicates an expected call of GetFullAtx.
func (mr *MockatxProviderMockRecorder) GetFullAtx(id interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetFullAtx", reflect.TypeOf((*MockatxProvider)(nil).GetFullAtx), id)
}

// GetMalfeasanceProof mocks base method.
func (m *MockatxProvider) GetMalfeasanceProof(id types.NodeID) (*types.MalfeasanceProof, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMalfeasanceProof", id)
	ret0, _ := ret[0].(*types.MalfeasanceProof)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetMalfeasanceProof indicates an expected call of GetMalfeasanceProof.
func (mr *MockatxProviderMockRecorder) GetMalfeasanceProof(id interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMalfeasanceProof", reflect.TypeOf((*MockatxProvider)(nil).GetMalfeasanceProof), id)
}

// MaxHeightAtx mocks base method.
func (m *MockatxProvider) MaxHeightAtx() (types.ATXID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "MaxHeightAtx")
	ret0, _ := ret[0].(types.ATXID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// MaxHeightAtx indicates an expected call of MaxHeightAtx.
func (mr *MockatxProviderMockRecorder) MaxHeightAtx() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MaxHeightAtx", reflect.TypeOf((*MockatxProvider)(nil).MaxHeightAtx))
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
func (m *MockpostSetupProvider) Benchmark(p activation.PostSetupProvider) (int, error) {
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

// Config mocks base method.
func (m *MockpostSetupProvider) Config() activation.PostConfig {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Config")
	ret0, _ := ret[0].(activation.PostConfig)
	return ret0
}

// Config indicates an expected call of Config.
func (mr *MockpostSetupProviderMockRecorder) Config() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Config", reflect.TypeOf((*MockpostSetupProvider)(nil).Config))
}

// Providers mocks base method.
func (m *MockpostSetupProvider) Providers() ([]activation.PostSetupProvider, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Providers")
	ret0, _ := ret[0].([]activation.PostSetupProvider)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Providers indicates an expected call of Providers.
func (mr *MockpostSetupProviderMockRecorder) Providers() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Providers", reflect.TypeOf((*MockpostSetupProvider)(nil).Providers))
}

// Status mocks base method.
func (m *MockpostSetupProvider) Status() *activation.PostSetupStatus {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Status")
	ret0, _ := ret[0].(*activation.PostSetupStatus)
	return ret0
}

// Status indicates an expected call of Status.
func (mr *MockpostSetupProviderMockRecorder) Status() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Status", reflect.TypeOf((*MockpostSetupProvider)(nil).Status))
}

// MockpeerCounter is a mock of peerCounter interface.
type MockpeerCounter struct {
	ctrl     *gomock.Controller
	recorder *MockpeerCounterMockRecorder
}

// MockpeerCounterMockRecorder is the mock recorder for MockpeerCounter.
type MockpeerCounterMockRecorder struct {
	mock *MockpeerCounter
}

// NewMockpeerCounter creates a new mock instance.
func NewMockpeerCounter(ctrl *gomock.Controller) *MockpeerCounter {
	mock := &MockpeerCounter{ctrl: ctrl}
	mock.recorder = &MockpeerCounterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpeerCounter) EXPECT() *MockpeerCounterMockRecorder {
	return m.recorder
}

// PeerCount mocks base method.
func (m *MockpeerCounter) PeerCount() uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PeerCount")
	ret0, _ := ret[0].(uint64)
	return ret0
}

// PeerCount indicates an expected call of PeerCount.
func (mr *MockpeerCounterMockRecorder) PeerCount() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PeerCount", reflect.TypeOf((*MockpeerCounter)(nil).PeerCount))
}

// MockgenesisTimeAPI is a mock of genesisTimeAPI interface.
type MockgenesisTimeAPI struct {
	ctrl     *gomock.Controller
	recorder *MockgenesisTimeAPIMockRecorder
}

// MockgenesisTimeAPIMockRecorder is the mock recorder for MockgenesisTimeAPI.
type MockgenesisTimeAPIMockRecorder struct {
	mock *MockgenesisTimeAPI
}

// NewMockgenesisTimeAPI creates a new mock instance.
func NewMockgenesisTimeAPI(ctrl *gomock.Controller) *MockgenesisTimeAPI {
	mock := &MockgenesisTimeAPI{ctrl: ctrl}
	mock.recorder = &MockgenesisTimeAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockgenesisTimeAPI) EXPECT() *MockgenesisTimeAPIMockRecorder {
	return m.recorder
}

// CurrentLayer mocks base method.
func (m *MockgenesisTimeAPI) CurrentLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// CurrentLayer indicates an expected call of CurrentLayer.
func (mr *MockgenesisTimeAPIMockRecorder) CurrentLayer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentLayer", reflect.TypeOf((*MockgenesisTimeAPI)(nil).CurrentLayer))
}

// GenesisTime mocks base method.
func (m *MockgenesisTimeAPI) GenesisTime() time.Time {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GenesisTime")
	ret0, _ := ret[0].(time.Time)
	return ret0
}

// GenesisTime indicates an expected call of GenesisTime.
func (mr *MockgenesisTimeAPIMockRecorder) GenesisTime() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GenesisTime", reflect.TypeOf((*MockgenesisTimeAPI)(nil).GenesisTime))
}

// MockmeshAPI is a mock of meshAPI interface.
type MockmeshAPI struct {
	ctrl     *gomock.Controller
	recorder *MockmeshAPIMockRecorder
}

// MockmeshAPIMockRecorder is the mock recorder for MockmeshAPI.
type MockmeshAPIMockRecorder struct {
	mock *MockmeshAPI
}

// NewMockmeshAPI creates a new mock instance.
func NewMockmeshAPI(ctrl *gomock.Controller) *MockmeshAPI {
	mock := &MockmeshAPI{ctrl: ctrl}
	mock.recorder = &MockmeshAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockmeshAPI) EXPECT() *MockmeshAPIMockRecorder {
	return m.recorder
}

// GetATXs mocks base method.
func (m *MockmeshAPI) GetATXs(arg0 context.Context, arg1 []types.ATXID) (map[types.ATXID]*types.VerifiedActivationTx, []types.ATXID) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetATXs", arg0, arg1)
	ret0, _ := ret[0].(map[types.ATXID]*types.VerifiedActivationTx)
	ret1, _ := ret[1].([]types.ATXID)
	return ret0, ret1
}

// GetATXs indicates an expected call of GetATXs.
func (mr *MockmeshAPIMockRecorder) GetATXs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetATXs", reflect.TypeOf((*MockmeshAPI)(nil).GetATXs), arg0, arg1)
}

// GetLayer mocks base method.
func (m *MockmeshAPI) GetLayer(arg0 types.LayerID) (*types.Layer, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayer", arg0)
	ret0, _ := ret[0].(*types.Layer)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLayer indicates an expected call of GetLayer.
func (mr *MockmeshAPIMockRecorder) GetLayer(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayer", reflect.TypeOf((*MockmeshAPI)(nil).GetLayer), arg0)
}

// GetRewards mocks base method.
func (m *MockmeshAPI) GetRewards(arg0 types.Address) ([]*types.Reward, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRewards", arg0)
	ret0, _ := ret[0].([]*types.Reward)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetRewards indicates an expected call of GetRewards.
func (mr *MockmeshAPIMockRecorder) GetRewards(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRewards", reflect.TypeOf((*MockmeshAPI)(nil).GetRewards), arg0)
}

// LatestLayer mocks base method.
func (m *MockmeshAPI) LatestLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LatestLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// LatestLayer indicates an expected call of LatestLayer.
func (mr *MockmeshAPIMockRecorder) LatestLayer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LatestLayer", reflect.TypeOf((*MockmeshAPI)(nil).LatestLayer))
}

// LatestLayerInState mocks base method.
func (m *MockmeshAPI) LatestLayerInState() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LatestLayerInState")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// LatestLayerInState indicates an expected call of LatestLayerInState.
func (mr *MockmeshAPIMockRecorder) LatestLayerInState() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LatestLayerInState", reflect.TypeOf((*MockmeshAPI)(nil).LatestLayerInState))
}

// MeshHash mocks base method.
func (m *MockmeshAPI) MeshHash(arg0 types.LayerID) (types.Hash32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "MeshHash", arg0)
	ret0, _ := ret[0].(types.Hash32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// MeshHash indicates an expected call of MeshHash.
func (mr *MockmeshAPIMockRecorder) MeshHash(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MeshHash", reflect.TypeOf((*MockmeshAPI)(nil).MeshHash), arg0)
}

// ProcessedLayer mocks base method.
func (m *MockmeshAPI) ProcessedLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessedLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// ProcessedLayer indicates an expected call of ProcessedLayer.
func (mr *MockmeshAPIMockRecorder) ProcessedLayer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessedLayer", reflect.TypeOf((*MockmeshAPI)(nil).ProcessedLayer))
}

// Mockoracle is a mock of oracle interface.
type Mockoracle struct {
	ctrl     *gomock.Controller
	recorder *MockoracleMockRecorder
}

// MockoracleMockRecorder is the mock recorder for Mockoracle.
type MockoracleMockRecorder struct {
	mock *Mockoracle
}

// NewMockoracle creates a new mock instance.
func NewMockoracle(ctrl *gomock.Controller) *Mockoracle {
	mock := &Mockoracle{ctrl: ctrl}
	mock.recorder = &MockoracleMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockoracle) EXPECT() *MockoracleMockRecorder {
	return m.recorder
}

// ActiveSet mocks base method.
func (m *Mockoracle) ActiveSet(arg0 context.Context, arg1 types.EpochID) ([]types.ATXID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ActiveSet", arg0, arg1)
	ret0, _ := ret[0].([]types.ATXID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ActiveSet indicates an expected call of ActiveSet.
func (mr *MockoracleMockRecorder) ActiveSet(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ActiveSet", reflect.TypeOf((*Mockoracle)(nil).ActiveSet), arg0, arg1)
}
