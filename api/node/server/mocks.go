// Code generated by MockGen. DO NOT EDIT.
// Source: server.go
//
// Generated by this command:
//
//	mockgen -typed -package=server -destination=mocks.go -source=server.go
//

// Package server is a generated GoMock package.
package server

import (
	context "context"
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	hare3 "github.com/spacemeshos/go-spacemesh/hare3"
	gomock "go.uber.org/mock/gomock"
)

// MockpoetDB is a mock of poetDB interface.
type MockpoetDB struct {
	ctrl     *gomock.Controller
	recorder *MockpoetDBMockRecorder
}

// MockpoetDBMockRecorder is the mock recorder for MockpoetDB.
type MockpoetDBMockRecorder struct {
	mock *MockpoetDB
}

// NewMockpoetDB creates a new mock instance.
func NewMockpoetDB(ctrl *gomock.Controller) *MockpoetDB {
	mock := &MockpoetDB{ctrl: ctrl}
	mock.recorder = &MockpoetDBMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpoetDB) EXPECT() *MockpoetDBMockRecorder {
	return m.recorder
}

// ValidateAndStore mocks base method.
func (m *MockpoetDB) ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ValidateAndStore", ctx, proofMessage)
	ret0, _ := ret[0].(error)
	return ret0
}

// ValidateAndStore indicates an expected call of ValidateAndStore.
func (mr *MockpoetDBMockRecorder) ValidateAndStore(ctx, proofMessage any) *MockpoetDBValidateAndStoreCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ValidateAndStore", reflect.TypeOf((*MockpoetDB)(nil).ValidateAndStore), ctx, proofMessage)
	return &MockpoetDBValidateAndStoreCall{Call: call}
}

// MockpoetDBValidateAndStoreCall wrap *gomock.Call
type MockpoetDBValidateAndStoreCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpoetDBValidateAndStoreCall) Return(arg0 error) *MockpoetDBValidateAndStoreCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpoetDBValidateAndStoreCall) Do(f func(context.Context, *types.PoetProofMessage) error) *MockpoetDBValidateAndStoreCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpoetDBValidateAndStoreCall) DoAndReturn(f func(context.Context, *types.PoetProofMessage) error) *MockpoetDBValidateAndStoreCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Mockhare is a mock of hare interface.
type Mockhare struct {
	ctrl     *gomock.Controller
	recorder *MockhareMockRecorder
}

// MockhareMockRecorder is the mock recorder for Mockhare.
type MockhareMockRecorder struct {
	mock *Mockhare
}

// NewMockhare creates a new mock instance.
func NewMockhare(ctrl *gomock.Controller) *Mockhare {
	mock := &Mockhare{ctrl: ctrl}
	mock.recorder = &MockhareMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockhare) EXPECT() *MockhareMockRecorder {
	return m.recorder
}

// Beacon mocks base method.
func (m *Mockhare) Beacon(ctx context.Context, epoch types.EpochID) types.Beacon {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Beacon", ctx, epoch)
	ret0, _ := ret[0].(types.Beacon)
	return ret0
}

// Beacon indicates an expected call of Beacon.
func (mr *MockhareMockRecorder) Beacon(ctx, epoch any) *MockhareBeaconCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Beacon", reflect.TypeOf((*Mockhare)(nil).Beacon), ctx, epoch)
	return &MockhareBeaconCall{Call: call}
}

// MockhareBeaconCall wrap *gomock.Call
type MockhareBeaconCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockhareBeaconCall) Return(arg0 types.Beacon) *MockhareBeaconCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockhareBeaconCall) Do(f func(context.Context, types.EpochID) types.Beacon) *MockhareBeaconCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockhareBeaconCall) DoAndReturn(f func(context.Context, types.EpochID) types.Beacon) *MockhareBeaconCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MinerWeight mocks base method.
func (m *Mockhare) MinerWeight(ctx context.Context, node types.NodeID, layer types.LayerID) uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "MinerWeight", ctx, node, layer)
	ret0, _ := ret[0].(uint64)
	return ret0
}

// MinerWeight indicates an expected call of MinerWeight.
func (mr *MockhareMockRecorder) MinerWeight(ctx, node, layer any) *MockhareMinerWeightCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MinerWeight", reflect.TypeOf((*Mockhare)(nil).MinerWeight), ctx, node, layer)
	return &MockhareMinerWeightCall{Call: call}
}

// MockhareMinerWeightCall wrap *gomock.Call
type MockhareMinerWeightCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockhareMinerWeightCall) Return(arg0 uint64) *MockhareMinerWeightCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockhareMinerWeightCall) Do(f func(context.Context, types.NodeID, types.LayerID) uint64) *MockhareMinerWeightCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockhareMinerWeightCall) DoAndReturn(f func(context.Context, types.NodeID, types.LayerID) uint64) *MockhareMinerWeightCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RoundMessage mocks base method.
func (m *Mockhare) RoundMessage(layer types.LayerID, round hare3.IterRound) *hare3.Message {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RoundMessage", layer, round)
	ret0, _ := ret[0].(*hare3.Message)
	return ret0
}

// RoundMessage indicates an expected call of RoundMessage.
func (mr *MockhareMockRecorder) RoundMessage(layer, round any) *MockhareRoundMessageCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RoundMessage", reflect.TypeOf((*Mockhare)(nil).RoundMessage), layer, round)
	return &MockhareRoundMessageCall{Call: call}
}

// MockhareRoundMessageCall wrap *gomock.Call
type MockhareRoundMessageCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockhareRoundMessageCall) Return(arg0 *hare3.Message) *MockhareRoundMessageCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockhareRoundMessageCall) Do(f func(types.LayerID, hare3.IterRound) *hare3.Message) *MockhareRoundMessageCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockhareRoundMessageCall) DoAndReturn(f func(types.LayerID, hare3.IterRound) *hare3.Message) *MockhareRoundMessageCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// TotalWeight mocks base method.
func (m *Mockhare) TotalWeight(ctx context.Context, layer types.LayerID) uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "TotalWeight", ctx, layer)
	ret0, _ := ret[0].(uint64)
	return ret0
}

// TotalWeight indicates an expected call of TotalWeight.
func (mr *MockhareMockRecorder) TotalWeight(ctx, layer any) *MockhareTotalWeightCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TotalWeight", reflect.TypeOf((*Mockhare)(nil).TotalWeight), ctx, layer)
	return &MockhareTotalWeightCall{Call: call}
}

// MockhareTotalWeightCall wrap *gomock.Call
type MockhareTotalWeightCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockhareTotalWeightCall) Return(arg0 uint64) *MockhareTotalWeightCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockhareTotalWeightCall) Do(f func(context.Context, types.LayerID) uint64) *MockhareTotalWeightCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockhareTotalWeightCall) DoAndReturn(f func(context.Context, types.LayerID) uint64) *MockhareTotalWeightCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
