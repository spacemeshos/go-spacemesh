// Code generated by MockGen. DO NOT EDIT.
// Source: ./fetcher.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	p2p "github.com/spacemeshos/go-spacemesh/p2p"
	gomock "go.uber.org/mock/gomock"
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

// GetActiveSet mocks base method.
func (m *MockFetcher) GetActiveSet(arg0 context.Context, arg1 types.Hash32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetActiveSet", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetActiveSet indicates an expected call of GetActiveSet.
func (mr *MockFetcherMockRecorder) GetActiveSet(arg0, arg1 interface{}) *FetcherGetActiveSetCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetActiveSet", reflect.TypeOf((*MockFetcher)(nil).GetActiveSet), arg0, arg1)
	return &FetcherGetActiveSetCall{Call: call}
}

// FetcherGetActiveSetCall wrap *gomock.Call
type FetcherGetActiveSetCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherGetActiveSetCall) Return(arg0 error) *FetcherGetActiveSetCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherGetActiveSetCall) Do(f func(context.Context, types.Hash32) error) *FetcherGetActiveSetCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherGetActiveSetCall) DoAndReturn(f func(context.Context, types.Hash32) error) *FetcherGetActiveSetCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetAtxs mocks base method.
func (m *MockFetcher) GetAtxs(arg0 context.Context, arg1 []types.ATXID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAtxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetAtxs indicates an expected call of GetAtxs.
func (mr *MockFetcherMockRecorder) GetAtxs(arg0, arg1 interface{}) *FetcherGetAtxsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAtxs", reflect.TypeOf((*MockFetcher)(nil).GetAtxs), arg0, arg1)
	return &FetcherGetAtxsCall{Call: call}
}

// FetcherGetAtxsCall wrap *gomock.Call
type FetcherGetAtxsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherGetAtxsCall) Return(arg0 error) *FetcherGetAtxsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherGetAtxsCall) Do(f func(context.Context, []types.ATXID) error) *FetcherGetAtxsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherGetAtxsCall) DoAndReturn(f func(context.Context, []types.ATXID) error) *FetcherGetAtxsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetBallots mocks base method.
func (m *MockFetcher) GetBallots(arg0 context.Context, arg1 []types.BallotID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBallots", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBallots indicates an expected call of GetBallots.
func (mr *MockFetcherMockRecorder) GetBallots(arg0, arg1 interface{}) *FetcherGetBallotsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBallots", reflect.TypeOf((*MockFetcher)(nil).GetBallots), arg0, arg1)
	return &FetcherGetBallotsCall{Call: call}
}

// FetcherGetBallotsCall wrap *gomock.Call
type FetcherGetBallotsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherGetBallotsCall) Return(arg0 error) *FetcherGetBallotsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherGetBallotsCall) Do(f func(context.Context, []types.BallotID) error) *FetcherGetBallotsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherGetBallotsCall) DoAndReturn(f func(context.Context, []types.BallotID) error) *FetcherGetBallotsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetBlockTxs mocks base method.
func (m *MockFetcher) GetBlockTxs(arg0 context.Context, arg1 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlockTxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBlockTxs indicates an expected call of GetBlockTxs.
func (mr *MockFetcherMockRecorder) GetBlockTxs(arg0, arg1 interface{}) *FetcherGetBlockTxsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlockTxs", reflect.TypeOf((*MockFetcher)(nil).GetBlockTxs), arg0, arg1)
	return &FetcherGetBlockTxsCall{Call: call}
}

// FetcherGetBlockTxsCall wrap *gomock.Call
type FetcherGetBlockTxsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherGetBlockTxsCall) Return(arg0 error) *FetcherGetBlockTxsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherGetBlockTxsCall) Do(f func(context.Context, []types.TransactionID) error) *FetcherGetBlockTxsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherGetBlockTxsCall) DoAndReturn(f func(context.Context, []types.TransactionID) error) *FetcherGetBlockTxsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetBlocks mocks base method.
func (m *MockFetcher) GetBlocks(arg0 context.Context, arg1 []types.BlockID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBlocks", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetBlocks indicates an expected call of GetBlocks.
func (mr *MockFetcherMockRecorder) GetBlocks(arg0, arg1 interface{}) *FetcherGetBlocksCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlocks", reflect.TypeOf((*MockFetcher)(nil).GetBlocks), arg0, arg1)
	return &FetcherGetBlocksCall{Call: call}
}

// FetcherGetBlocksCall wrap *gomock.Call
type FetcherGetBlocksCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherGetBlocksCall) Return(arg0 error) *FetcherGetBlocksCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherGetBlocksCall) Do(f func(context.Context, []types.BlockID) error) *FetcherGetBlocksCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherGetBlocksCall) DoAndReturn(f func(context.Context, []types.BlockID) error) *FetcherGetBlocksCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetPoetProof mocks base method.
func (m *MockFetcher) GetPoetProof(arg0 context.Context, arg1 types.Hash32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPoetProof", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetPoetProof indicates an expected call of GetPoetProof.
func (mr *MockFetcherMockRecorder) GetPoetProof(arg0, arg1 interface{}) *FetcherGetPoetProofCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPoetProof", reflect.TypeOf((*MockFetcher)(nil).GetPoetProof), arg0, arg1)
	return &FetcherGetPoetProofCall{Call: call}
}

// FetcherGetPoetProofCall wrap *gomock.Call
type FetcherGetPoetProofCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherGetPoetProofCall) Return(arg0 error) *FetcherGetPoetProofCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherGetPoetProofCall) Do(f func(context.Context, types.Hash32) error) *FetcherGetPoetProofCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherGetPoetProofCall) DoAndReturn(f func(context.Context, types.Hash32) error) *FetcherGetPoetProofCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetProposalTxs mocks base method.
func (m *MockFetcher) GetProposalTxs(arg0 context.Context, arg1 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProposalTxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetProposalTxs indicates an expected call of GetProposalTxs.
func (mr *MockFetcherMockRecorder) GetProposalTxs(arg0, arg1 interface{}) *FetcherGetProposalTxsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposalTxs", reflect.TypeOf((*MockFetcher)(nil).GetProposalTxs), arg0, arg1)
	return &FetcherGetProposalTxsCall{Call: call}
}

// FetcherGetProposalTxsCall wrap *gomock.Call
type FetcherGetProposalTxsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherGetProposalTxsCall) Return(arg0 error) *FetcherGetProposalTxsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherGetProposalTxsCall) Do(f func(context.Context, []types.TransactionID) error) *FetcherGetProposalTxsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherGetProposalTxsCall) DoAndReturn(f func(context.Context, []types.TransactionID) error) *FetcherGetProposalTxsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetProposals mocks base method.
func (m *MockFetcher) GetProposals(arg0 context.Context, arg1 []types.ProposalID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProposals", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetProposals indicates an expected call of GetProposals.
func (mr *MockFetcherMockRecorder) GetProposals(arg0, arg1 interface{}) *FetcherGetProposalsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposals", reflect.TypeOf((*MockFetcher)(nil).GetProposals), arg0, arg1)
	return &FetcherGetProposalsCall{Call: call}
}

// FetcherGetProposalsCall wrap *gomock.Call
type FetcherGetProposalsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherGetProposalsCall) Return(arg0 error) *FetcherGetProposalsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherGetProposalsCall) Do(f func(context.Context, []types.ProposalID) error) *FetcherGetProposalsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherGetProposalsCall) DoAndReturn(f func(context.Context, []types.ProposalID) error) *FetcherGetProposalsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RegisterPeerHashes mocks base method.
func (m *MockFetcher) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RegisterPeerHashes", peer, hashes)
}

// RegisterPeerHashes indicates an expected call of RegisterPeerHashes.
func (mr *MockFetcherMockRecorder) RegisterPeerHashes(peer, hashes interface{}) *FetcherRegisterPeerHashesCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterPeerHashes", reflect.TypeOf((*MockFetcher)(nil).RegisterPeerHashes), peer, hashes)
	return &FetcherRegisterPeerHashesCall{Call: call}
}

// FetcherRegisterPeerHashesCall wrap *gomock.Call
type FetcherRegisterPeerHashesCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *FetcherRegisterPeerHashesCall) Return() *FetcherRegisterPeerHashesCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *FetcherRegisterPeerHashesCall) Do(f func(p2p.Peer, []types.Hash32)) *FetcherRegisterPeerHashesCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *FetcherRegisterPeerHashesCall) DoAndReturn(f func(p2p.Peer, []types.Hash32)) *FetcherRegisterPeerHashesCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockBlockFetcherMockRecorder) GetBlocks(arg0, arg1 interface{}) *BlockFetcherGetBlocksCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlocks", reflect.TypeOf((*MockBlockFetcher)(nil).GetBlocks), arg0, arg1)
	return &BlockFetcherGetBlocksCall{Call: call}
}

// BlockFetcherGetBlocksCall wrap *gomock.Call
type BlockFetcherGetBlocksCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *BlockFetcherGetBlocksCall) Return(arg0 error) *BlockFetcherGetBlocksCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *BlockFetcherGetBlocksCall) Do(f func(context.Context, []types.BlockID) error) *BlockFetcherGetBlocksCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *BlockFetcherGetBlocksCall) DoAndReturn(f func(context.Context, []types.BlockID) error) *BlockFetcherGetBlocksCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockAtxFetcherMockRecorder) GetAtxs(arg0, arg1 interface{}) *AtxFetcherGetAtxsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAtxs", reflect.TypeOf((*MockAtxFetcher)(nil).GetAtxs), arg0, arg1)
	return &AtxFetcherGetAtxsCall{Call: call}
}

// AtxFetcherGetAtxsCall wrap *gomock.Call
type AtxFetcherGetAtxsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *AtxFetcherGetAtxsCall) Return(arg0 error) *AtxFetcherGetAtxsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *AtxFetcherGetAtxsCall) Do(f func(context.Context, []types.ATXID) error) *AtxFetcherGetAtxsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *AtxFetcherGetAtxsCall) DoAndReturn(f func(context.Context, []types.ATXID) error) *AtxFetcherGetAtxsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockTxFetcherMockRecorder) GetBlockTxs(arg0, arg1 interface{}) *TxFetcherGetBlockTxsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBlockTxs", reflect.TypeOf((*MockTxFetcher)(nil).GetBlockTxs), arg0, arg1)
	return &TxFetcherGetBlockTxsCall{Call: call}
}

// TxFetcherGetBlockTxsCall wrap *gomock.Call
type TxFetcherGetBlockTxsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *TxFetcherGetBlockTxsCall) Return(arg0 error) *TxFetcherGetBlockTxsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *TxFetcherGetBlockTxsCall) Do(f func(context.Context, []types.TransactionID) error) *TxFetcherGetBlockTxsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *TxFetcherGetBlockTxsCall) DoAndReturn(f func(context.Context, []types.TransactionID) error) *TxFetcherGetBlockTxsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetProposalTxs mocks base method.
func (m *MockTxFetcher) GetProposalTxs(arg0 context.Context, arg1 []types.TransactionID) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProposalTxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetProposalTxs indicates an expected call of GetProposalTxs.
func (mr *MockTxFetcherMockRecorder) GetProposalTxs(arg0, arg1 interface{}) *TxFetcherGetProposalTxsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposalTxs", reflect.TypeOf((*MockTxFetcher)(nil).GetProposalTxs), arg0, arg1)
	return &TxFetcherGetProposalTxsCall{Call: call}
}

// TxFetcherGetProposalTxsCall wrap *gomock.Call
type TxFetcherGetProposalTxsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *TxFetcherGetProposalTxsCall) Return(arg0 error) *TxFetcherGetProposalTxsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *TxFetcherGetProposalTxsCall) Do(f func(context.Context, []types.TransactionID) error) *TxFetcherGetProposalTxsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *TxFetcherGetProposalTxsCall) DoAndReturn(f func(context.Context, []types.TransactionID) error) *TxFetcherGetProposalTxsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockPoetProofFetcherMockRecorder) GetPoetProof(arg0, arg1 interface{}) *PoetProofFetcherGetPoetProofCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPoetProof", reflect.TypeOf((*MockPoetProofFetcher)(nil).GetPoetProof), arg0, arg1)
	return &PoetProofFetcherGetPoetProofCall{Call: call}
}

// PoetProofFetcherGetPoetProofCall wrap *gomock.Call
type PoetProofFetcherGetPoetProofCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *PoetProofFetcherGetPoetProofCall) Return(arg0 error) *PoetProofFetcherGetPoetProofCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *PoetProofFetcherGetPoetProofCall) Do(f func(context.Context, types.Hash32) error) *PoetProofFetcherGetPoetProofCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *PoetProofFetcherGetPoetProofCall) DoAndReturn(f func(context.Context, types.Hash32) error) *PoetProofFetcherGetPoetProofCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockBallotFetcherMockRecorder) GetBallots(arg0, arg1 interface{}) *BallotFetcherGetBallotsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBallots", reflect.TypeOf((*MockBallotFetcher)(nil).GetBallots), arg0, arg1)
	return &BallotFetcherGetBallotsCall{Call: call}
}

// BallotFetcherGetBallotsCall wrap *gomock.Call
type BallotFetcherGetBallotsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *BallotFetcherGetBallotsCall) Return(arg0 error) *BallotFetcherGetBallotsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *BallotFetcherGetBallotsCall) Do(f func(context.Context, []types.BallotID) error) *BallotFetcherGetBallotsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *BallotFetcherGetBallotsCall) DoAndReturn(f func(context.Context, []types.BallotID) error) *BallotFetcherGetBallotsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockProposalFetcherMockRecorder) GetProposals(arg0, arg1 interface{}) *ProposalFetcherGetProposalsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProposals", reflect.TypeOf((*MockProposalFetcher)(nil).GetProposals), arg0, arg1)
	return &ProposalFetcherGetProposalsCall{Call: call}
}

// ProposalFetcherGetProposalsCall wrap *gomock.Call
type ProposalFetcherGetProposalsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *ProposalFetcherGetProposalsCall) Return(arg0 error) *ProposalFetcherGetProposalsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *ProposalFetcherGetProposalsCall) Do(f func(context.Context, []types.ProposalID) error) *ProposalFetcherGetProposalsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *ProposalFetcherGetProposalsCall) DoAndReturn(f func(context.Context, []types.ProposalID) error) *ProposalFetcherGetProposalsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockActiveSetFetcher is a mock of ActiveSetFetcher interface.
type MockActiveSetFetcher struct {
	ctrl     *gomock.Controller
	recorder *MockActiveSetFetcherMockRecorder
}

// MockActiveSetFetcherMockRecorder is the mock recorder for MockActiveSetFetcher.
type MockActiveSetFetcherMockRecorder struct {
	mock *MockActiveSetFetcher
}

// NewMockActiveSetFetcher creates a new mock instance.
func NewMockActiveSetFetcher(ctrl *gomock.Controller) *MockActiveSetFetcher {
	mock := &MockActiveSetFetcher{ctrl: ctrl}
	mock.recorder = &MockActiveSetFetcherMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockActiveSetFetcher) EXPECT() *MockActiveSetFetcherMockRecorder {
	return m.recorder
}

// GetActiveSet mocks base method.
func (m *MockActiveSetFetcher) GetActiveSet(arg0 context.Context, arg1 types.Hash32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetActiveSet", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// GetActiveSet indicates an expected call of GetActiveSet.
func (mr *MockActiveSetFetcherMockRecorder) GetActiveSet(arg0, arg1 interface{}) *ActiveSetFetcherGetActiveSetCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetActiveSet", reflect.TypeOf((*MockActiveSetFetcher)(nil).GetActiveSet), arg0, arg1)
	return &ActiveSetFetcherGetActiveSetCall{Call: call}
}

// ActiveSetFetcherGetActiveSetCall wrap *gomock.Call
type ActiveSetFetcherGetActiveSetCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *ActiveSetFetcherGetActiveSetCall) Return(arg0 error) *ActiveSetFetcherGetActiveSetCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *ActiveSetFetcherGetActiveSetCall) Do(f func(context.Context, types.Hash32) error) *ActiveSetFetcherGetActiveSetCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *ActiveSetFetcherGetActiveSetCall) DoAndReturn(f func(context.Context, types.Hash32) error) *ActiveSetFetcherGetActiveSetCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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

// RegisterPeerHashes mocks base method.
func (m *MockPeerTracker) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RegisterPeerHashes", peer, hashes)
}

// RegisterPeerHashes indicates an expected call of RegisterPeerHashes.
func (mr *MockPeerTrackerMockRecorder) RegisterPeerHashes(peer, hashes interface{}) *PeerTrackerRegisterPeerHashesCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterPeerHashes", reflect.TypeOf((*MockPeerTracker)(nil).RegisterPeerHashes), peer, hashes)
	return &PeerTrackerRegisterPeerHashesCall{Call: call}
}

// PeerTrackerRegisterPeerHashesCall wrap *gomock.Call
type PeerTrackerRegisterPeerHashesCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *PeerTrackerRegisterPeerHashesCall) Return() *PeerTrackerRegisterPeerHashesCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *PeerTrackerRegisterPeerHashesCall) Do(f func(p2p.Peer, []types.Hash32)) *PeerTrackerRegisterPeerHashesCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *PeerTrackerRegisterPeerHashesCall) DoAndReturn(f func(p2p.Peer, []types.Hash32)) *PeerTrackerRegisterPeerHashesCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
