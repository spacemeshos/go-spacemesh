// Code generated by MockGen. DO NOT EDIT.
// Source: ./fetcher.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	p2p "github.com/spacemeshos/go-spacemesh/p2p"
)

// MockFetcher is a mock of Fetcher interface.
type MockFetcher struct {
	ctrl     *gomock.Controller
	recorder *MockFetcherMockRecorder
}

// MockFetcherMockRecorder is the mock recorder for MockFetcher.
type MockFetcherMockRecorder struct {
	mock *MockFetcher
}

// NewMockFetcher creates a new mock instance.
func NewMockFetcher(ctrl *gomock.Controller) *MockFetcher {
	mock := &MockFetcher{ctrl: ctrl}
	mock.recorder = &MockFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockFetcher) EXPECT() *MockFetcherMockRecorder {
	return m.recorder
}

// AddPeersFromHash mocks base method.
func (m *MockFetcher) AddPeersFromHash(arg0 types.Hash32, arg1 []types.Hash32) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddPeersFromHash", arg0, arg1)
}

// AddPeersFromHash indicates an expected call of AddPeersFromHash.
func (mr *MockFetcherMockRecorder) AddPeersFromHash(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddPeersFromHash", reflect.TypeOf((*MockFetcher)(nil).AddPeersFromHash), arg0, arg1)
}

// GetAtxs mocks base method.
func (m *MockFetcher) GetAtxs(arg0 context.Context, arg1 []types.ATXID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAtxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetAtxs indicates an expected call of GetAtxs.
func (mr *MockFetcherMockRecorder) GetAtxs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAtxs", reflect.TypeOf((*MockFetcher)(nil).GetAtxs), arg0, arg1)
}

// GetBallots mocks base method.
func (m *MockFetcher) GetBallots(arg0 context.Context, arg1 []types.BallotID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBallots", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBallots indicates an expected call of GetBallots.
func (mr *MockFetcherMockRecorder) GetBallots(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBallots", reflect.TypeOf((*MockFetcher)(nil).GetBallots), arg0, arg1)
}

// GetBlockTxs mocks base method.
func (m *MockFetcher) GetBlockTxs(arg0 context.Context, arg1 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlockTxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBlockTxs indicates an expected call of GetBlockTxs.
func (mr *MockFetcherMockRecorder) GetBlockTxs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlockTxs", reflect.TypeOf((*MockFetcher)(nil).GetBlockTxs), arg0, arg1)
}

// GetBlocks mocks base method.
func (m *MockFetcher) GetBlocks(arg0 context.Context, arg1 []types.BlockID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlocks", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBlocks indicates an expected call of GetBlocks.
func (mr *MockFetcherMockRecorder) GetBlocks(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlocks", reflect.TypeOf((*MockFetcher)(nil).GetBlocks), arg0, arg1)
}

// GetPoetProof mocks base method.
func (m *MockFetcher) GetPoetProof(arg0 context.Context, arg1 types.Hash32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPoetProof", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetPoetProof indicates an expected call of GetPoetProof.
func (mr *MockFetcherMockRecorder) GetPoetProof(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPoetProof", reflect.TypeOf((*MockFetcher)(nil).GetPoetProof), arg0, arg1)
}

// GetProposalTxs mocks base method.
func (m *MockFetcher) GetProposalTxs(arg0 context.Context, arg1 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProposalTxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetProposalTxs indicates an expected call of GetProposalTxs.
func (mr *MockFetcherMockRecorder) GetProposalTxs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposalTxs", reflect.TypeOf((*MockFetcher)(nil).GetProposalTxs), arg0, arg1)
}

// GetProposals mocks base method.
func (m *MockFetcher) GetProposals(arg0 context.Context, arg1 []types.ProposalID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProposals", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetProposals indicates an expected call of GetProposals.
func (mr *MockFetcherMockRecorder) GetProposals(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposals", reflect.TypeOf((*MockFetcher)(nil).GetProposals), arg0, arg1)
}

// RegisterPeerHashes mocks base method.
func (m *MockFetcher) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RegisterPeerHashes", peer, hashes)
}

// RegisterPeerHashes indicates an expected call of RegisterPeerHashes.
func (mr *MockFetcherMockRecorder) RegisterPeerHashes(peer, hashes interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterPeerHashes", reflect.TypeOf((*MockFetcher)(nil).RegisterPeerHashes), peer, hashes)
}

// MockBlockFetcher is a mock of BlockFetcher interface.
type MockBlockFetcher struct {
	ctrl     *gomock.Controller
	recorder *MockBlockFetcherMockRecorder
}

// MockBlockFetcherMockRecorder is the mock recorder for MockBlockFetcher.
type MockBlockFetcherMockRecorder struct {
	mock *MockBlockFetcher
}

// NewMockBlockFetcher creates a new mock instance.
func NewMockBlockFetcher(ctrl *gomock.Controller) *MockBlockFetcher {
	mock := &MockBlockFetcher{ctrl: ctrl}
	mock.recorder = &MockBlockFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockBlockFetcher) EXPECT() *MockBlockFetcherMockRecorder {
	return m.recorder
}

// GetBlocks mocks base method.
func (m *MockBlockFetcher) GetBlocks(arg0 context.Context, arg1 []types.BlockID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlocks", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBlocks indicates an expected call of GetBlocks.
func (mr *MockBlockFetcherMockRecorder) GetBlocks(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlocks", reflect.TypeOf((*MockBlockFetcher)(nil).GetBlocks), arg0, arg1)
}

// MockAtxFetcher is a mock of AtxFetcher interface.
type MockAtxFetcher struct {
	ctrl     *gomock.Controller
	recorder *MockAtxFetcherMockRecorder
}

// MockAtxFetcherMockRecorder is the mock recorder for MockAtxFetcher.
type MockAtxFetcherMockRecorder struct {
	mock *MockAtxFetcher
}

// NewMockAtxFetcher creates a new mock instance.
func NewMockAtxFetcher(ctrl *gomock.Controller) *MockAtxFetcher {
	mock := &MockAtxFetcher{ctrl: ctrl}
	mock.recorder = &MockAtxFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAtxFetcher) EXPECT() *MockAtxFetcherMockRecorder {
	return m.recorder
}

// GetAtxs mocks base method.
func (m *MockAtxFetcher) GetAtxs(arg0 context.Context, arg1 []types.ATXID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAtxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetAtxs indicates an expected call of GetAtxs.
func (mr *MockAtxFetcherMockRecorder) GetAtxs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAtxs", reflect.TypeOf((*MockAtxFetcher)(nil).GetAtxs), arg0, arg1)
}

// MockTxFetcher is a mock of TxFetcher interface.
type MockTxFetcher struct {
	ctrl     *gomock.Controller
	recorder *MockTxFetcherMockRecorder
}

// MockTxFetcherMockRecorder is the mock recorder for MockTxFetcher.
type MockTxFetcherMockRecorder struct {
	mock *MockTxFetcher
}

// NewMockTxFetcher creates a new mock instance.
func NewMockTxFetcher(ctrl *gomock.Controller) *MockTxFetcher {
	mock := &MockTxFetcher{ctrl: ctrl}
	mock.recorder = &MockTxFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTxFetcher) EXPECT() *MockTxFetcherMockRecorder {
	return m.recorder
}

// GetBlockTxs mocks base method.
func (m *MockTxFetcher) GetBlockTxs(arg0 context.Context, arg1 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlockTxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBlockTxs indicates an expected call of GetBlockTxs.
func (mr *MockTxFetcherMockRecorder) GetBlockTxs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlockTxs", reflect.TypeOf((*MockTxFetcher)(nil).GetBlockTxs), arg0, arg1)
}

// GetProposalTxs mocks base method.
func (m *MockTxFetcher) GetProposalTxs(arg0 context.Context, arg1 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProposalTxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetProposalTxs indicates an expected call of GetProposalTxs.
func (mr *MockTxFetcherMockRecorder) GetProposalTxs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposalTxs", reflect.TypeOf((*MockTxFetcher)(nil).GetProposalTxs), arg0, arg1)
}

// MockPoetProofFetcher is a mock of PoetProofFetcher interface.
type MockPoetProofFetcher struct {
	ctrl     *gomock.Controller
	recorder *MockPoetProofFetcherMockRecorder
}

// MockPoetProofFetcherMockRecorder is the mock recorder for MockPoetProofFetcher.
type MockPoetProofFetcherMockRecorder struct {
	mock *MockPoetProofFetcher
}

// NewMockPoetProofFetcher creates a new mock instance.
func NewMockPoetProofFetcher(ctrl *gomock.Controller) *MockPoetProofFetcher {
	mock := &MockPoetProofFetcher{ctrl: ctrl}
	mock.recorder = &MockPoetProofFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPoetProofFetcher) EXPECT() *MockPoetProofFetcherMockRecorder {
	return m.recorder
}

// GetPoetProof mocks base method.
func (m *MockPoetProofFetcher) GetPoetProof(arg0 context.Context, arg1 types.Hash32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPoetProof", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetPoetProof indicates an expected call of GetPoetProof.
func (mr *MockPoetProofFetcherMockRecorder) GetPoetProof(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPoetProof", reflect.TypeOf((*MockPoetProofFetcher)(nil).GetPoetProof), arg0, arg1)
}

// MockBallotFetcher is a mock of BallotFetcher interface.
type MockBallotFetcher struct {
	ctrl     *gomock.Controller
	recorder *MockBallotFetcherMockRecorder
}

// MockBallotFetcherMockRecorder is the mock recorder for MockBallotFetcher.
type MockBallotFetcherMockRecorder struct {
	mock *MockBallotFetcher
}

// NewMockBallotFetcher creates a new mock instance.
func NewMockBallotFetcher(ctrl *gomock.Controller) *MockBallotFetcher {
	mock := &MockBallotFetcher{ctrl: ctrl}
	mock.recorder = &MockBallotFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockBallotFetcher) EXPECT() *MockBallotFetcherMockRecorder {
	return m.recorder
}

// GetBallots mocks base method.
func (m *MockBallotFetcher) GetBallots(arg0 context.Context, arg1 []types.BallotID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBallots", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBallots indicates an expected call of GetBallots.
func (mr *MockBallotFetcherMockRecorder) GetBallots(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBallots", reflect.TypeOf((*MockBallotFetcher)(nil).GetBallots), arg0, arg1)
}

// MockProposalFetcher is a mock of ProposalFetcher interface.
type MockProposalFetcher struct {
	ctrl     *gomock.Controller
	recorder *MockProposalFetcherMockRecorder
}

// MockProposalFetcherMockRecorder is the mock recorder for MockProposalFetcher.
type MockProposalFetcherMockRecorder struct {
	mock *MockProposalFetcher
}

// NewMockProposalFetcher creates a new mock instance.
func NewMockProposalFetcher(ctrl *gomock.Controller) *MockProposalFetcher {
	mock := &MockProposalFetcher{ctrl: ctrl}
	mock.recorder = &MockProposalFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockProposalFetcher) EXPECT() *MockProposalFetcherMockRecorder {
	return m.recorder
}

// GetProposals mocks base method.
func (m *MockProposalFetcher) GetProposals(arg0 context.Context, arg1 []types.ProposalID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProposals", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetProposals indicates an expected call of GetProposals.
func (mr *MockProposalFetcherMockRecorder) GetProposals(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposals", reflect.TypeOf((*MockProposalFetcher)(nil).GetProposals), arg0, arg1)
}

// MockPeerTracker is a mock of PeerTracker interface.
type MockPeerTracker struct {
	ctrl     *gomock.Controller
	recorder *MockPeerTrackerMockRecorder
}

// MockPeerTrackerMockRecorder is the mock recorder for MockPeerTracker.
type MockPeerTrackerMockRecorder struct {
	mock *MockPeerTracker
}

// NewMockPeerTracker creates a new mock instance.
func NewMockPeerTracker(ctrl *gomock.Controller) *MockPeerTracker {
	mock := &MockPeerTracker{ctrl: ctrl}
	mock.recorder = &MockPeerTrackerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPeerTracker) EXPECT() *MockPeerTrackerMockRecorder {
	return m.recorder
}

// AddPeersFromHash mocks base method.
func (m *MockPeerTracker) AddPeersFromHash(arg0 types.Hash32, arg1 []types.Hash32) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddPeersFromHash", arg0, arg1)
}

// AddPeersFromHash indicates an expected call of AddPeersFromHash.
func (mr *MockPeerTrackerMockRecorder) AddPeersFromHash(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddPeersFromHash", reflect.TypeOf((*MockPeerTracker)(nil).AddPeersFromHash), arg0, arg1)
}

// RegisterPeerHashes mocks base method.
func (m *MockPeerTracker) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RegisterPeerHashes", peer, hashes)
}

// RegisterPeerHashes indicates an expected call of RegisterPeerHashes.
func (mr *MockPeerTrackerMockRecorder) RegisterPeerHashes(peer, hashes interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterPeerHashes", reflect.TypeOf((*MockPeerTracker)(nil).RegisterPeerHashes), peer, hashes)
}
