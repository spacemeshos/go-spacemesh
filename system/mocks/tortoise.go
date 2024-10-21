// Code generated by MockGen. DO NOT EDIT.
// Source: ./tortoise.go
//
// Generated by this command:
//
//	mockgen -typed -package=mocks -destination=./mocks/tortoise.go -source=./tortoise.go
//

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	atxsdata "github.com/spacemeshos/go-spacemesh/atxsdata"
	types "github.com/spacemeshos/go-spacemesh/common/types"
	result "github.com/spacemeshos/go-spacemesh/common/types/result"
	gomock "go.uber.org/mock/gomock"
)

// MockTortoise is a mock of Tortoise interface.
type MockTortoise struct {
	ctrl     *gomock.Controller
	recorder *MockTortoiseMockRecorder
	isgomock struct{}
}

// MockTortoiseMockRecorder is the mock recorder for MockTortoise.
type MockTortoiseMockRecorder struct {
	mock *MockTortoise
}

// NewMockTortoise creates a new mock instance.
func NewMockTortoise(ctrl *gomock.Controller) *MockTortoise {
	mock := &MockTortoise{ctrl: ctrl}
	mock.recorder = &MockTortoiseMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTortoise) EXPECT() *MockTortoiseMockRecorder {
	return m.recorder
}

// GetMissingActiveSet mocks base method.
func (m *MockTortoise) GetMissingActiveSet(arg0 types.EpochID, arg1 []types.ATXID) []types.ATXID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMissingActiveSet", arg0, arg1)
	ret0, _ := ret[0].([]types.ATXID)
	return ret0
}

// GetMissingActiveSet indicates an expected call of GetMissingActiveSet.
func (mr *MockTortoiseMockRecorder) GetMissingActiveSet(arg0, arg1 any) *MockTortoiseGetMissingActiveSetCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMissingActiveSet", reflect.TypeOf((*MockTortoise)(nil).GetMissingActiveSet), arg0, arg1)
	return &MockTortoiseGetMissingActiveSetCall{Call: call}
}

// MockTortoiseGetMissingActiveSetCall wrap *gomock.Call
type MockTortoiseGetMissingActiveSetCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseGetMissingActiveSetCall) Return(arg0 []types.ATXID) *MockTortoiseGetMissingActiveSetCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseGetMissingActiveSetCall) Do(f func(types.EpochID, []types.ATXID) []types.ATXID) *MockTortoiseGetMissingActiveSetCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseGetMissingActiveSetCall) DoAndReturn(f func(types.EpochID, []types.ATXID) []types.ATXID) *MockTortoiseGetMissingActiveSetCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// LatestComplete mocks base method.
func (m *MockTortoise) LatestComplete() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LatestComplete")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// LatestComplete indicates an expected call of LatestComplete.
func (mr *MockTortoiseMockRecorder) LatestComplete() *MockTortoiseLatestCompleteCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LatestComplete", reflect.TypeOf((*MockTortoise)(nil).LatestComplete))
	return &MockTortoiseLatestCompleteCall{Call: call}
}

// MockTortoiseLatestCompleteCall wrap *gomock.Call
type MockTortoiseLatestCompleteCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseLatestCompleteCall) Return(arg0 types.LayerID) *MockTortoiseLatestCompleteCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseLatestCompleteCall) Do(f func() types.LayerID) *MockTortoiseLatestCompleteCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseLatestCompleteCall) DoAndReturn(f func() types.LayerID) *MockTortoiseLatestCompleteCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// OnApplied mocks base method.
func (m *MockTortoise) OnApplied(arg0 types.LayerID, arg1 types.Hash32) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "OnApplied", arg0, arg1)
	ret0, _ := ret[0].(bool)
	return ret0
}

// OnApplied indicates an expected call of OnApplied.
func (mr *MockTortoiseMockRecorder) OnApplied(arg0, arg1 any) *MockTortoiseOnAppliedCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnApplied", reflect.TypeOf((*MockTortoise)(nil).OnApplied), arg0, arg1)
	return &MockTortoiseOnAppliedCall{Call: call}
}

// MockTortoiseOnAppliedCall wrap *gomock.Call
type MockTortoiseOnAppliedCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseOnAppliedCall) Return(arg0 bool) *MockTortoiseOnAppliedCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseOnAppliedCall) Do(f func(types.LayerID, types.Hash32) bool) *MockTortoiseOnAppliedCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseOnAppliedCall) DoAndReturn(f func(types.LayerID, types.Hash32) bool) *MockTortoiseOnAppliedCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// OnAtx mocks base method.
func (m *MockTortoise) OnAtx(arg0 types.EpochID, arg1 types.ATXID, arg2 *atxsdata.ATX) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnAtx", arg0, arg1, arg2)
}

// OnAtx indicates an expected call of OnAtx.
func (mr *MockTortoiseMockRecorder) OnAtx(arg0, arg1, arg2 any) *MockTortoiseOnAtxCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnAtx", reflect.TypeOf((*MockTortoise)(nil).OnAtx), arg0, arg1, arg2)
	return &MockTortoiseOnAtxCall{Call: call}
}

// MockTortoiseOnAtxCall wrap *gomock.Call
type MockTortoiseOnAtxCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseOnAtxCall) Return() *MockTortoiseOnAtxCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseOnAtxCall) Do(f func(types.EpochID, types.ATXID, *atxsdata.ATX)) *MockTortoiseOnAtxCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseOnAtxCall) DoAndReturn(f func(types.EpochID, types.ATXID, *atxsdata.ATX)) *MockTortoiseOnAtxCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// OnBlock mocks base method.
func (m *MockTortoise) OnBlock(arg0 types.BlockHeader) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnBlock", arg0)
}

// OnBlock indicates an expected call of OnBlock.
func (mr *MockTortoiseMockRecorder) OnBlock(arg0 any) *MockTortoiseOnBlockCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnBlock", reflect.TypeOf((*MockTortoise)(nil).OnBlock), arg0)
	return &MockTortoiseOnBlockCall{Call: call}
}

// MockTortoiseOnBlockCall wrap *gomock.Call
type MockTortoiseOnBlockCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseOnBlockCall) Return() *MockTortoiseOnBlockCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseOnBlockCall) Do(f func(types.BlockHeader)) *MockTortoiseOnBlockCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseOnBlockCall) DoAndReturn(f func(types.BlockHeader)) *MockTortoiseOnBlockCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// OnHareOutput mocks base method.
func (m *MockTortoise) OnHareOutput(arg0 types.LayerID, arg1 types.BlockID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnHareOutput", arg0, arg1)
}

// OnHareOutput indicates an expected call of OnHareOutput.
func (mr *MockTortoiseMockRecorder) OnHareOutput(arg0, arg1 any) *MockTortoiseOnHareOutputCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnHareOutput", reflect.TypeOf((*MockTortoise)(nil).OnHareOutput), arg0, arg1)
	return &MockTortoiseOnHareOutputCall{Call: call}
}

// MockTortoiseOnHareOutputCall wrap *gomock.Call
type MockTortoiseOnHareOutputCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseOnHareOutputCall) Return() *MockTortoiseOnHareOutputCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseOnHareOutputCall) Do(f func(types.LayerID, types.BlockID)) *MockTortoiseOnHareOutputCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseOnHareOutputCall) DoAndReturn(f func(types.LayerID, types.BlockID)) *MockTortoiseOnHareOutputCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// OnMalfeasance mocks base method.
func (m *MockTortoise) OnMalfeasance(arg0 types.NodeID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnMalfeasance", arg0)
}

// OnMalfeasance indicates an expected call of OnMalfeasance.
func (mr *MockTortoiseMockRecorder) OnMalfeasance(arg0 any) *MockTortoiseOnMalfeasanceCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnMalfeasance", reflect.TypeOf((*MockTortoise)(nil).OnMalfeasance), arg0)
	return &MockTortoiseOnMalfeasanceCall{Call: call}
}

// MockTortoiseOnMalfeasanceCall wrap *gomock.Call
type MockTortoiseOnMalfeasanceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseOnMalfeasanceCall) Return() *MockTortoiseOnMalfeasanceCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseOnMalfeasanceCall) Do(f func(types.NodeID)) *MockTortoiseOnMalfeasanceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseOnMalfeasanceCall) DoAndReturn(f func(types.NodeID)) *MockTortoiseOnMalfeasanceCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// OnWeakCoin mocks base method.
func (m *MockTortoise) OnWeakCoin(arg0 types.LayerID, arg1 bool) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnWeakCoin", arg0, arg1)
}

// OnWeakCoin indicates an expected call of OnWeakCoin.
func (mr *MockTortoiseMockRecorder) OnWeakCoin(arg0, arg1 any) *MockTortoiseOnWeakCoinCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnWeakCoin", reflect.TypeOf((*MockTortoise)(nil).OnWeakCoin), arg0, arg1)
	return &MockTortoiseOnWeakCoinCall{Call: call}
}

// MockTortoiseOnWeakCoinCall wrap *gomock.Call
type MockTortoiseOnWeakCoinCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseOnWeakCoinCall) Return() *MockTortoiseOnWeakCoinCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseOnWeakCoinCall) Do(f func(types.LayerID, bool)) *MockTortoiseOnWeakCoinCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseOnWeakCoinCall) DoAndReturn(f func(types.LayerID, bool)) *MockTortoiseOnWeakCoinCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// TallyVotes mocks base method.
func (m *MockTortoise) TallyVotes(arg0 types.LayerID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "TallyVotes", arg0)
}

// TallyVotes indicates an expected call of TallyVotes.
func (mr *MockTortoiseMockRecorder) TallyVotes(arg0 any) *MockTortoiseTallyVotesCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TallyVotes", reflect.TypeOf((*MockTortoise)(nil).TallyVotes), arg0)
	return &MockTortoiseTallyVotesCall{Call: call}
}

// MockTortoiseTallyVotesCall wrap *gomock.Call
type MockTortoiseTallyVotesCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseTallyVotesCall) Return() *MockTortoiseTallyVotesCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseTallyVotesCall) Do(f func(types.LayerID)) *MockTortoiseTallyVotesCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseTallyVotesCall) DoAndReturn(f func(types.LayerID)) *MockTortoiseTallyVotesCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Updates mocks base method.
func (m *MockTortoise) Updates() []result.Layer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Updates")
	ret0, _ := ret[0].([]result.Layer)
	return ret0
}

// Updates indicates an expected call of Updates.
func (mr *MockTortoiseMockRecorder) Updates() *MockTortoiseUpdatesCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Updates", reflect.TypeOf((*MockTortoise)(nil).Updates))
	return &MockTortoiseUpdatesCall{Call: call}
}

// MockTortoiseUpdatesCall wrap *gomock.Call
type MockTortoiseUpdatesCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockTortoiseUpdatesCall) Return(arg0 []result.Layer) *MockTortoiseUpdatesCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockTortoiseUpdatesCall) Do(f func() []result.Layer) *MockTortoiseUpdatesCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockTortoiseUpdatesCall) DoAndReturn(f func() []result.Layer) *MockTortoiseUpdatesCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
