// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go
//
// Generated by this command:
//
//	mockgen -typed -package=hashsync -destination=./mocks_test.go -source=./interface.go
//

// Package hashsync is a generated GoMock package.
package hashsync

import (
	context "context"
	io "io"
	reflect "reflect"

	types "github.com/spacemeshos/go-spacemesh/common/types"
	p2p "github.com/spacemeshos/go-spacemesh/p2p"
	server "github.com/spacemeshos/go-spacemesh/p2p/server"
	gomock "go.uber.org/mock/gomock"
)

// MockIterator is a mock of Iterator interface.
type MockIterator struct {
	ctrl     *gomock.Controller
	recorder *MockIteratorMockRecorder
}

// MockIteratorMockRecorder is the mock recorder for MockIterator.
type MockIteratorMockRecorder struct {
	mock *MockIterator
}

// NewMockIterator creates a new mock instance.
func NewMockIterator(ctrl *gomock.Controller) *MockIterator {
	mock := &MockIterator{ctrl: ctrl}
	mock.recorder = &MockIteratorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockIterator) EXPECT() *MockIteratorMockRecorder {
	return m.recorder
}

// Equal mocks base method.
func (m *MockIterator) Equal(other Iterator) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Equal", other)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Equal indicates an expected call of Equal.
func (mr *MockIteratorMockRecorder) Equal(other any) *MockIteratorEqualCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Equal", reflect.TypeOf((*MockIterator)(nil).Equal), other)
	return &MockIteratorEqualCall{Call: call}
}

// MockIteratorEqualCall wrap *gomock.Call
type MockIteratorEqualCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockIteratorEqualCall) Return(arg0 bool) *MockIteratorEqualCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockIteratorEqualCall) Do(f func(Iterator) bool) *MockIteratorEqualCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockIteratorEqualCall) DoAndReturn(f func(Iterator) bool) *MockIteratorEqualCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Key mocks base method.
func (m *MockIterator) Key() Ordered {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Key")
	ret0, _ := ret[0].(Ordered)
	return ret0
}

// Key indicates an expected call of Key.
func (mr *MockIteratorMockRecorder) Key() *MockIteratorKeyCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Key", reflect.TypeOf((*MockIterator)(nil).Key))
	return &MockIteratorKeyCall{Call: call}
}

// MockIteratorKeyCall wrap *gomock.Call
type MockIteratorKeyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockIteratorKeyCall) Return(arg0 Ordered) *MockIteratorKeyCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockIteratorKeyCall) Do(f func() Ordered) *MockIteratorKeyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockIteratorKeyCall) DoAndReturn(f func() Ordered) *MockIteratorKeyCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Next mocks base method.
func (m *MockIterator) Next() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Next")
}

// Next indicates an expected call of Next.
func (mr *MockIteratorMockRecorder) Next() *MockIteratorNextCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Next", reflect.TypeOf((*MockIterator)(nil).Next))
	return &MockIteratorNextCall{Call: call}
}

// MockIteratorNextCall wrap *gomock.Call
type MockIteratorNextCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockIteratorNextCall) Return() *MockIteratorNextCall {
	c.Call = c.Call.Return()
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockIteratorNextCall) Do(f func()) *MockIteratorNextCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockIteratorNextCall) DoAndReturn(f func()) *MockIteratorNextCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockItemStore is a mock of ItemStore interface.
type MockItemStore struct {
	ctrl     *gomock.Controller
	recorder *MockItemStoreMockRecorder
}

// MockItemStoreMockRecorder is the mock recorder for MockItemStore.
type MockItemStoreMockRecorder struct {
	mock *MockItemStore
}

// NewMockItemStore creates a new mock instance.
func NewMockItemStore(ctrl *gomock.Controller) *MockItemStore {
	mock := &MockItemStore{ctrl: ctrl}
	mock.recorder = &MockItemStoreMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockItemStore) EXPECT() *MockItemStoreMockRecorder {
	return m.recorder
}

// Add mocks base method.
func (m *MockItemStore) Add(ctx context.Context, k Ordered) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Add", ctx, k)
	ret0, _ := ret[0].(error)
	return ret0
}

// Add indicates an expected call of Add.
func (mr *MockItemStoreMockRecorder) Add(ctx, k any) *MockItemStoreAddCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Add", reflect.TypeOf((*MockItemStore)(nil).Add), ctx, k)
	return &MockItemStoreAddCall{Call: call}
}

// MockItemStoreAddCall wrap *gomock.Call
type MockItemStoreAddCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockItemStoreAddCall) Return(arg0 error) *MockItemStoreAddCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockItemStoreAddCall) Do(f func(context.Context, Ordered) error) *MockItemStoreAddCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockItemStoreAddCall) DoAndReturn(f func(context.Context, Ordered) error) *MockItemStoreAddCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Copy mocks base method.
func (m *MockItemStore) Copy() ItemStore {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Copy")
	ret0, _ := ret[0].(ItemStore)
	return ret0
}

// Copy indicates an expected call of Copy.
func (mr *MockItemStoreMockRecorder) Copy() *MockItemStoreCopyCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Copy", reflect.TypeOf((*MockItemStore)(nil).Copy))
	return &MockItemStoreCopyCall{Call: call}
}

// MockItemStoreCopyCall wrap *gomock.Call
type MockItemStoreCopyCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockItemStoreCopyCall) Return(arg0 ItemStore) *MockItemStoreCopyCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockItemStoreCopyCall) Do(f func() ItemStore) *MockItemStoreCopyCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockItemStoreCopyCall) DoAndReturn(f func() ItemStore) *MockItemStoreCopyCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetRangeInfo mocks base method.
func (m *MockItemStore) GetRangeInfo(preceding Iterator, x, y Ordered, count int) RangeInfo {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRangeInfo", preceding, x, y, count)
	ret0, _ := ret[0].(RangeInfo)
	return ret0
}

// GetRangeInfo indicates an expected call of GetRangeInfo.
func (mr *MockItemStoreMockRecorder) GetRangeInfo(preceding, x, y, count any) *MockItemStoreGetRangeInfoCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRangeInfo", reflect.TypeOf((*MockItemStore)(nil).GetRangeInfo), preceding, x, y, count)
	return &MockItemStoreGetRangeInfoCall{Call: call}
}

// MockItemStoreGetRangeInfoCall wrap *gomock.Call
type MockItemStoreGetRangeInfoCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockItemStoreGetRangeInfoCall) Return(arg0 RangeInfo) *MockItemStoreGetRangeInfoCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockItemStoreGetRangeInfoCall) Do(f func(Iterator, Ordered, Ordered, int) RangeInfo) *MockItemStoreGetRangeInfoCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockItemStoreGetRangeInfoCall) DoAndReturn(f func(Iterator, Ordered, Ordered, int) RangeInfo) *MockItemStoreGetRangeInfoCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Has mocks base method.
func (m *MockItemStore) Has(k Ordered) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Has", k)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Has indicates an expected call of Has.
func (mr *MockItemStoreMockRecorder) Has(k any) *MockItemStoreHasCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Has", reflect.TypeOf((*MockItemStore)(nil).Has), k)
	return &MockItemStoreHasCall{Call: call}
}

// MockItemStoreHasCall wrap *gomock.Call
type MockItemStoreHasCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockItemStoreHasCall) Return(arg0 bool) *MockItemStoreHasCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockItemStoreHasCall) Do(f func(Ordered) bool) *MockItemStoreHasCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockItemStoreHasCall) DoAndReturn(f func(Ordered) bool) *MockItemStoreHasCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Max mocks base method.
func (m *MockItemStore) Max() Iterator {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Max")
	ret0, _ := ret[0].(Iterator)
	return ret0
}

// Max indicates an expected call of Max.
func (mr *MockItemStoreMockRecorder) Max() *MockItemStoreMaxCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Max", reflect.TypeOf((*MockItemStore)(nil).Max))
	return &MockItemStoreMaxCall{Call: call}
}

// MockItemStoreMaxCall wrap *gomock.Call
type MockItemStoreMaxCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockItemStoreMaxCall) Return(arg0 Iterator) *MockItemStoreMaxCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockItemStoreMaxCall) Do(f func() Iterator) *MockItemStoreMaxCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockItemStoreMaxCall) DoAndReturn(f func() Iterator) *MockItemStoreMaxCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Min mocks base method.
func (m *MockItemStore) Min() Iterator {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Min")
	ret0, _ := ret[0].(Iterator)
	return ret0
}

// Min indicates an expected call of Min.
func (mr *MockItemStoreMockRecorder) Min() *MockItemStoreMinCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Min", reflect.TypeOf((*MockItemStore)(nil).Min))
	return &MockItemStoreMinCall{Call: call}
}

// MockItemStoreMinCall wrap *gomock.Call
type MockItemStoreMinCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockItemStoreMinCall) Return(arg0 Iterator) *MockItemStoreMinCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockItemStoreMinCall) Do(f func() Iterator) *MockItemStoreMinCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockItemStoreMinCall) DoAndReturn(f func() Iterator) *MockItemStoreMinCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockRequester is a mock of Requester interface.
type MockRequester struct {
	ctrl     *gomock.Controller
	recorder *MockRequesterMockRecorder
}

// MockRequesterMockRecorder is the mock recorder for MockRequester.
type MockRequesterMockRecorder struct {
	mock *MockRequester
}

// NewMockRequester creates a new mock instance.
func NewMockRequester(ctrl *gomock.Controller) *MockRequester {
	mock := &MockRequester{ctrl: ctrl}
	mock.recorder = &MockRequesterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRequester) EXPECT() *MockRequesterMockRecorder {
	return m.recorder
}

// Run mocks base method.
func (m *MockRequester) Run(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Run", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Run indicates an expected call of Run.
func (mr *MockRequesterMockRecorder) Run(arg0 any) *MockRequesterRunCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockRequester)(nil).Run), arg0)
	return &MockRequesterRunCall{Call: call}
}

// MockRequesterRunCall wrap *gomock.Call
type MockRequesterRunCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockRequesterRunCall) Return(arg0 error) *MockRequesterRunCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockRequesterRunCall) Do(f func(context.Context) error) *MockRequesterRunCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockRequesterRunCall) DoAndReturn(f func(context.Context) error) *MockRequesterRunCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// StreamRequest mocks base method.
func (m *MockRequester) StreamRequest(arg0 context.Context, arg1 p2p.Peer, arg2 []byte, arg3 server.StreamRequestCallback, arg4 ...string) error {
	m.ctrl.T.Helper()
	varargs := []any{arg0, arg1, arg2, arg3}
	for _, a := range arg4 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "StreamRequest", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// StreamRequest indicates an expected call of StreamRequest.
func (mr *MockRequesterMockRecorder) StreamRequest(arg0, arg1, arg2, arg3 any, arg4 ...any) *MockRequesterStreamRequestCall {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{arg0, arg1, arg2, arg3}, arg4...)
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StreamRequest", reflect.TypeOf((*MockRequester)(nil).StreamRequest), varargs...)
	return &MockRequesterStreamRequestCall{Call: call}
}

// MockRequesterStreamRequestCall wrap *gomock.Call
type MockRequesterStreamRequestCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockRequesterStreamRequestCall) Return(arg0 error) *MockRequesterStreamRequestCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockRequesterStreamRequestCall) Do(f func(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error) *MockRequesterStreamRequestCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockRequesterStreamRequestCall) DoAndReturn(f func(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error) *MockRequesterStreamRequestCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockSyncBase is a mock of SyncBase interface.
type MockSyncBase struct {
	ctrl     *gomock.Controller
	recorder *MockSyncBaseMockRecorder
}

// MockSyncBaseMockRecorder is the mock recorder for MockSyncBase.
type MockSyncBaseMockRecorder struct {
	mock *MockSyncBase
}

// NewMockSyncBase creates a new mock instance.
func NewMockSyncBase(ctrl *gomock.Controller) *MockSyncBase {
	mock := &MockSyncBase{ctrl: ctrl}
	mock.recorder = &MockSyncBaseMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockSyncBase) EXPECT() *MockSyncBaseMockRecorder {
	return m.recorder
}

// Count mocks base method.
func (m *MockSyncBase) Count() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Count")
	ret0, _ := ret[0].(int)
	return ret0
}

// Count indicates an expected call of Count.
func (mr *MockSyncBaseMockRecorder) Count() *MockSyncBaseCountCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Count", reflect.TypeOf((*MockSyncBase)(nil).Count))
	return &MockSyncBaseCountCall{Call: call}
}

// MockSyncBaseCountCall wrap *gomock.Call
type MockSyncBaseCountCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSyncBaseCountCall) Return(arg0 int) *MockSyncBaseCountCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSyncBaseCountCall) Do(f func() int) *MockSyncBaseCountCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSyncBaseCountCall) DoAndReturn(f func() int) *MockSyncBaseCountCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Derive mocks base method.
func (m *MockSyncBase) Derive(p p2p.Peer) Syncer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Derive", p)
	ret0, _ := ret[0].(Syncer)
	return ret0
}

// Derive indicates an expected call of Derive.
func (mr *MockSyncBaseMockRecorder) Derive(p any) *MockSyncBaseDeriveCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Derive", reflect.TypeOf((*MockSyncBase)(nil).Derive), p)
	return &MockSyncBaseDeriveCall{Call: call}
}

// MockSyncBaseDeriveCall wrap *gomock.Call
type MockSyncBaseDeriveCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSyncBaseDeriveCall) Return(arg0 Syncer) *MockSyncBaseDeriveCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSyncBaseDeriveCall) Do(f func(p2p.Peer) Syncer) *MockSyncBaseDeriveCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSyncBaseDeriveCall) DoAndReturn(f func(p2p.Peer) Syncer) *MockSyncBaseDeriveCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Probe mocks base method.
func (m *MockSyncBase) Probe(ctx context.Context, p p2p.Peer) (ProbeResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Probe", ctx, p)
	ret0, _ := ret[0].(ProbeResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Probe indicates an expected call of Probe.
func (mr *MockSyncBaseMockRecorder) Probe(ctx, p any) *MockSyncBaseProbeCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Probe", reflect.TypeOf((*MockSyncBase)(nil).Probe), ctx, p)
	return &MockSyncBaseProbeCall{Call: call}
}

// MockSyncBaseProbeCall wrap *gomock.Call
type MockSyncBaseProbeCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSyncBaseProbeCall) Return(arg0 ProbeResult, arg1 error) *MockSyncBaseProbeCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSyncBaseProbeCall) Do(f func(context.Context, p2p.Peer) (ProbeResult, error)) *MockSyncBaseProbeCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSyncBaseProbeCall) DoAndReturn(f func(context.Context, p2p.Peer) (ProbeResult, error)) *MockSyncBaseProbeCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Wait mocks base method.
func (m *MockSyncBase) Wait() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Wait")
	ret0, _ := ret[0].(error)
	return ret0
}

// Wait indicates an expected call of Wait.
func (mr *MockSyncBaseMockRecorder) Wait() *MockSyncBaseWaitCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Wait", reflect.TypeOf((*MockSyncBase)(nil).Wait))
	return &MockSyncBaseWaitCall{Call: call}
}

// MockSyncBaseWaitCall wrap *gomock.Call
type MockSyncBaseWaitCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSyncBaseWaitCall) Return(arg0 error) *MockSyncBaseWaitCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSyncBaseWaitCall) Do(f func() error) *MockSyncBaseWaitCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSyncBaseWaitCall) DoAndReturn(f func() error) *MockSyncBaseWaitCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockSyncer is a mock of Syncer interface.
type MockSyncer struct {
	ctrl     *gomock.Controller
	recorder *MockSyncerMockRecorder
}

// MockSyncerMockRecorder is the mock recorder for MockSyncer.
type MockSyncerMockRecorder struct {
	mock *MockSyncer
}

// NewMockSyncer creates a new mock instance.
func NewMockSyncer(ctrl *gomock.Controller) *MockSyncer {
	mock := &MockSyncer{ctrl: ctrl}
	mock.recorder = &MockSyncerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockSyncer) EXPECT() *MockSyncerMockRecorder {
	return m.recorder
}

// Peer mocks base method.
func (m *MockSyncer) Peer() p2p.Peer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Peer")
	ret0, _ := ret[0].(p2p.Peer)
	return ret0
}

// Peer indicates an expected call of Peer.
func (mr *MockSyncerMockRecorder) Peer() *MockSyncerPeerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Peer", reflect.TypeOf((*MockSyncer)(nil).Peer))
	return &MockSyncerPeerCall{Call: call}
}

// MockSyncerPeerCall wrap *gomock.Call
type MockSyncerPeerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSyncerPeerCall) Return(arg0 p2p.Peer) *MockSyncerPeerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSyncerPeerCall) Do(f func() p2p.Peer) *MockSyncerPeerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSyncerPeerCall) DoAndReturn(f func() p2p.Peer) *MockSyncerPeerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Serve mocks base method.
func (m *MockSyncer) Serve(ctx context.Context, req []byte, stream io.ReadWriter) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Serve", ctx, req, stream)
	ret0, _ := ret[0].(error)
	return ret0
}

// Serve indicates an expected call of Serve.
func (mr *MockSyncerMockRecorder) Serve(ctx, req, stream any) *MockSyncerServeCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Serve", reflect.TypeOf((*MockSyncer)(nil).Serve), ctx, req, stream)
	return &MockSyncerServeCall{Call: call}
}

// MockSyncerServeCall wrap *gomock.Call
type MockSyncerServeCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSyncerServeCall) Return(arg0 error) *MockSyncerServeCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSyncerServeCall) Do(f func(context.Context, []byte, io.ReadWriter) error) *MockSyncerServeCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSyncerServeCall) DoAndReturn(f func(context.Context, []byte, io.ReadWriter) error) *MockSyncerServeCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Sync mocks base method.
func (m *MockSyncer) Sync(ctx context.Context, x, y *types.Hash32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Sync", ctx, x, y)
	ret0, _ := ret[0].(error)
	return ret0
}

// Sync indicates an expected call of Sync.
func (mr *MockSyncerMockRecorder) Sync(ctx, x, y any) *MockSyncerSyncCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Sync", reflect.TypeOf((*MockSyncer)(nil).Sync), ctx, x, y)
	return &MockSyncerSyncCall{Call: call}
}

// MockSyncerSyncCall wrap *gomock.Call
type MockSyncerSyncCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSyncerSyncCall) Return(arg0 error) *MockSyncerSyncCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSyncerSyncCall) Do(f func(context.Context, *types.Hash32, *types.Hash32) error) *MockSyncerSyncCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSyncerSyncCall) DoAndReturn(f func(context.Context, *types.Hash32, *types.Hash32) error) *MockSyncerSyncCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockPairwiseSyncer is a mock of PairwiseSyncer interface.
type MockPairwiseSyncer struct {
	ctrl     *gomock.Controller
	recorder *MockPairwiseSyncerMockRecorder
}

// MockPairwiseSyncerMockRecorder is the mock recorder for MockPairwiseSyncer.
type MockPairwiseSyncerMockRecorder struct {
	mock *MockPairwiseSyncer
}

// NewMockPairwiseSyncer creates a new mock instance.
func NewMockPairwiseSyncer(ctrl *gomock.Controller) *MockPairwiseSyncer {
	mock := &MockPairwiseSyncer{ctrl: ctrl}
	mock.recorder = &MockPairwiseSyncerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPairwiseSyncer) EXPECT() *MockPairwiseSyncerMockRecorder {
	return m.recorder
}

// Probe mocks base method.
func (m *MockPairwiseSyncer) Probe(ctx context.Context, peer p2p.Peer, is ItemStore, x, y *types.Hash32) (ProbeResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Probe", ctx, peer, is, x, y)
	ret0, _ := ret[0].(ProbeResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Probe indicates an expected call of Probe.
func (mr *MockPairwiseSyncerMockRecorder) Probe(ctx, peer, is, x, y any) *MockPairwiseSyncerProbeCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Probe", reflect.TypeOf((*MockPairwiseSyncer)(nil).Probe), ctx, peer, is, x, y)
	return &MockPairwiseSyncerProbeCall{Call: call}
}

// MockPairwiseSyncerProbeCall wrap *gomock.Call
type MockPairwiseSyncerProbeCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockPairwiseSyncerProbeCall) Return(arg0 ProbeResult, arg1 error) *MockPairwiseSyncerProbeCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockPairwiseSyncerProbeCall) Do(f func(context.Context, p2p.Peer, ItemStore, *types.Hash32, *types.Hash32) (ProbeResult, error)) *MockPairwiseSyncerProbeCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockPairwiseSyncerProbeCall) DoAndReturn(f func(context.Context, p2p.Peer, ItemStore, *types.Hash32, *types.Hash32) (ProbeResult, error)) *MockPairwiseSyncerProbeCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Serve mocks base method.
func (m *MockPairwiseSyncer) Serve(ctx context.Context, req []byte, stream io.ReadWriter, is ItemStore) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Serve", ctx, req, stream, is)
	ret0, _ := ret[0].(error)
	return ret0
}

// Serve indicates an expected call of Serve.
func (mr *MockPairwiseSyncerMockRecorder) Serve(ctx, req, stream, is any) *MockPairwiseSyncerServeCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Serve", reflect.TypeOf((*MockPairwiseSyncer)(nil).Serve), ctx, req, stream, is)
	return &MockPairwiseSyncerServeCall{Call: call}
}

// MockPairwiseSyncerServeCall wrap *gomock.Call
type MockPairwiseSyncerServeCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockPairwiseSyncerServeCall) Return(arg0 error) *MockPairwiseSyncerServeCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockPairwiseSyncerServeCall) Do(f func(context.Context, []byte, io.ReadWriter, ItemStore) error) *MockPairwiseSyncerServeCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockPairwiseSyncerServeCall) DoAndReturn(f func(context.Context, []byte, io.ReadWriter, ItemStore) error) *MockPairwiseSyncerServeCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// SyncStore mocks base method.
func (m *MockPairwiseSyncer) SyncStore(ctx context.Context, peer p2p.Peer, is ItemStore, x, y *types.Hash32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SyncStore", ctx, peer, is, x, y)
	ret0, _ := ret[0].(error)
	return ret0
}

// SyncStore indicates an expected call of SyncStore.
func (mr *MockPairwiseSyncerMockRecorder) SyncStore(ctx, peer, is, x, y any) *MockPairwiseSyncerSyncStoreCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SyncStore", reflect.TypeOf((*MockPairwiseSyncer)(nil).SyncStore), ctx, peer, is, x, y)
	return &MockPairwiseSyncerSyncStoreCall{Call: call}
}

// MockPairwiseSyncerSyncStoreCall wrap *gomock.Call
type MockPairwiseSyncerSyncStoreCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockPairwiseSyncerSyncStoreCall) Return(arg0 error) *MockPairwiseSyncerSyncStoreCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockPairwiseSyncerSyncStoreCall) Do(f func(context.Context, p2p.Peer, ItemStore, *types.Hash32, *types.Hash32) error) *MockPairwiseSyncerSyncStoreCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockPairwiseSyncerSyncStoreCall) DoAndReturn(f func(context.Context, p2p.Peer, ItemStore, *types.Hash32, *types.Hash32) error) *MockPairwiseSyncerSyncStoreCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MocksyncRunner is a mock of syncRunner interface.
type MocksyncRunner struct {
	ctrl     *gomock.Controller
	recorder *MocksyncRunnerMockRecorder
}

// MocksyncRunnerMockRecorder is the mock recorder for MocksyncRunner.
type MocksyncRunnerMockRecorder struct {
	mock *MocksyncRunner
}

// NewMocksyncRunner creates a new mock instance.
func NewMocksyncRunner(ctrl *gomock.Controller) *MocksyncRunner {
	mock := &MocksyncRunner{ctrl: ctrl}
	mock.recorder = &MocksyncRunnerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocksyncRunner) EXPECT() *MocksyncRunnerMockRecorder {
	return m.recorder
}

// fullSync mocks base method.
func (m *MocksyncRunner) fullSync(ctx context.Context, syncPeers []p2p.Peer) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "fullSync", ctx, syncPeers)
	ret0, _ := ret[0].(error)
	return ret0
}

// fullSync indicates an expected call of fullSync.
func (mr *MocksyncRunnerMockRecorder) fullSync(ctx, syncPeers any) *MocksyncRunnerfullSyncCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "fullSync", reflect.TypeOf((*MocksyncRunner)(nil).fullSync), ctx, syncPeers)
	return &MocksyncRunnerfullSyncCall{Call: call}
}

// MocksyncRunnerfullSyncCall wrap *gomock.Call
type MocksyncRunnerfullSyncCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocksyncRunnerfullSyncCall) Return(arg0 error) *MocksyncRunnerfullSyncCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocksyncRunnerfullSyncCall) Do(f func(context.Context, []p2p.Peer) error) *MocksyncRunnerfullSyncCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocksyncRunnerfullSyncCall) DoAndReturn(f func(context.Context, []p2p.Peer) error) *MocksyncRunnerfullSyncCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// splitSync mocks base method.
func (m *MocksyncRunner) splitSync(ctx context.Context, syncPeers []p2p.Peer) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "splitSync", ctx, syncPeers)
	ret0, _ := ret[0].(error)
	return ret0
}

// splitSync indicates an expected call of splitSync.
func (mr *MocksyncRunnerMockRecorder) splitSync(ctx, syncPeers any) *MocksyncRunnersplitSyncCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "splitSync", reflect.TypeOf((*MocksyncRunner)(nil).splitSync), ctx, syncPeers)
	return &MocksyncRunnersplitSyncCall{Call: call}
}

// MocksyncRunnersplitSyncCall wrap *gomock.Call
type MocksyncRunnersplitSyncCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MocksyncRunnersplitSyncCall) Return(arg0 error) *MocksyncRunnersplitSyncCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MocksyncRunnersplitSyncCall) Do(f func(context.Context, []p2p.Peer) error) *MocksyncRunnersplitSyncCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MocksyncRunnersplitSyncCall) DoAndReturn(f func(context.Context, []p2p.Peer) error) *MocksyncRunnersplitSyncCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}