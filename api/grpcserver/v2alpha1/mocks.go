package v2alpha1

import (
	"github.com/spacemeshos/go-spacemesh/p2p"
	"reflect"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

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
func (mr *MockgenesisTimeAPIMockRecorder) CurrentLayer() *MockgenesisTimeAPICurrentLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentLayer",
		reflect.TypeOf((*MockgenesisTimeAPI)(nil).CurrentLayer))
	return &MockgenesisTimeAPICurrentLayerCall{Call: call}
}

// MockgenesisTimeAPICurrentLayerCall wrap *gomock.Call.
type MockgenesisTimeAPICurrentLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return.
func (c *MockgenesisTimeAPICurrentLayerCall) Return(arg0 types.LayerID) *MockgenesisTimeAPICurrentLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do.
func (c *MockgenesisTimeAPICurrentLayerCall) Do(f func() types.LayerID) *MockgenesisTimeAPICurrentLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn.
func (c *MockgenesisTimeAPICurrentLayerCall) DoAndReturn(f func() types.LayerID) *MockgenesisTimeAPICurrentLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GenesisTime mocks base method.
func (m *MockgenesisTimeAPI) GenesisTime() time.Time {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GenesisTime")
	ret0, _ := ret[0].(time.Time)
	return ret0
}

// GenesisTime indicates an expected call of GenesisTime.
func (mr *MockgenesisTimeAPIMockRecorder) GenesisTime() *MockgenesisTimeAPIGenesisTimeCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GenesisTime",
		reflect.TypeOf((*MockgenesisTimeAPI)(nil).GenesisTime))
	return &MockgenesisTimeAPIGenesisTimeCall{Call: call}
}

// MockgenesisTimeAPIGenesisTimeCall wrap *gomock.Call.
type MockgenesisTimeAPIGenesisTimeCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return.
func (c *MockgenesisTimeAPIGenesisTimeCall) Return(arg0 time.Time) *MockgenesisTimeAPIGenesisTimeCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do.
func (c *MockgenesisTimeAPIGenesisTimeCall) Do(f func() time.Time) *MockgenesisTimeAPIGenesisTimeCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn.
func (c *MockgenesisTimeAPIGenesisTimeCall) DoAndReturn(f func() time.Time) *MockgenesisTimeAPIGenesisTimeCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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

// GetLayer mocks base method.
func (m *MockmeshAPI) GetLayer(arg0 types.LayerID) (*types.Layer, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayer", arg0)
	ret0, _ := ret[0].(*types.Layer)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLayer indicates an expected call of GetLayer.
func (mr *MockmeshAPIMockRecorder) GetLayer(arg0 any) *MockmeshAPIGetLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayer", reflect.TypeOf((*MockmeshAPI)(nil).GetLayer), arg0)
	return &MockmeshAPIGetLayerCall{Call: call}
}

// MockmeshAPIGetLayerCall wrap *gomock.Call
type MockmeshAPIGetLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshAPIGetLayerCall) Return(arg0 *types.Layer, arg1 error) *MockmeshAPIGetLayerCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshAPIGetLayerCall) Do(f func(types.LayerID) (*types.Layer, error)) *MockmeshAPIGetLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshAPIGetLayerCall) DoAndReturn(f func(types.LayerID) (*types.Layer, error)) *MockmeshAPIGetLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetLayerVerified mocks base method.
func (m *MockmeshAPI) GetLayerVerified(arg0 types.LayerID) (*types.Block, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLayerVerified", arg0)
	ret0, _ := ret[0].(*types.Block)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLayerVerified indicates an expected call of GetLayerVerified.
func (mr *MockmeshAPIMockRecorder) GetLayerVerified(arg0 any) *MockmeshAPIGetLayerVerifiedCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLayerVerified", reflect.TypeOf((*MockmeshAPI)(nil).GetLayerVerified), arg0)
	return &MockmeshAPIGetLayerVerifiedCall{Call: call}
}

// MockmeshAPIGetLayerVerifiedCall wrap *gomock.Call
type MockmeshAPIGetLayerVerifiedCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshAPIGetLayerVerifiedCall) Return(arg0 *types.Block, arg1 error) *MockmeshAPIGetLayerVerifiedCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshAPIGetLayerVerifiedCall) Do(f func(types.LayerID) (*types.Block, error)) *MockmeshAPIGetLayerVerifiedCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshAPIGetLayerVerifiedCall) DoAndReturn(f func(types.LayerID) (*types.Block, error)) *MockmeshAPIGetLayerVerifiedCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetRewardsByCoinbase mocks base method.
func (m *MockmeshAPI) GetRewardsByCoinbase(arg0 types.Address) ([]*types.Reward, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRewardsByCoinbase", arg0)
	ret0, _ := ret[0].([]*types.Reward)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetRewardsByCoinbase indicates an expected call of GetRewardsByCoinbase.
func (mr *MockmeshAPIMockRecorder) GetRewardsByCoinbase(arg0 any) *MockmeshAPIGetRewardsByCoinbaseCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRewardsByCoinbase", reflect.TypeOf((*MockmeshAPI)(nil).GetRewardsByCoinbase), arg0)
	return &MockmeshAPIGetRewardsByCoinbaseCall{Call: call}
}

// MockmeshAPIGetRewardsByCoinbaseCall wrap *gomock.Call
type MockmeshAPIGetRewardsByCoinbaseCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshAPIGetRewardsByCoinbaseCall) Return(arg0 []*types.Reward, arg1 error) *MockmeshAPIGetRewardsByCoinbaseCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshAPIGetRewardsByCoinbaseCall) Do(f func(types.Address) ([]*types.Reward, error)) *MockmeshAPIGetRewardsByCoinbaseCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshAPIGetRewardsByCoinbaseCall) DoAndReturn(f func(types.Address) ([]*types.Reward, error)) *MockmeshAPIGetRewardsByCoinbaseCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetRewardsBySmesherId mocks base method.
func (m *MockmeshAPI) GetRewardsBySmesherId(id types.NodeID) ([]*types.Reward, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRewardsBySmesherId", id)
	ret0, _ := ret[0].([]*types.Reward)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetRewardsBySmesherId indicates an expected call of GetRewardsBySmesherId.
func (mr *MockmeshAPIMockRecorder) GetRewardsBySmesherId(id any) *MockmeshAPIGetRewardsBySmesherIdCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRewardsBySmesherId", reflect.TypeOf((*MockmeshAPI)(nil).GetRewardsBySmesherId), id)
	return &MockmeshAPIGetRewardsBySmesherIdCall{Call: call}
}

// MockmeshAPIGetRewardsBySmesherIdCall wrap *gomock.Call
type MockmeshAPIGetRewardsBySmesherIdCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshAPIGetRewardsBySmesherIdCall) Return(arg0 []*types.Reward, arg1 error) *MockmeshAPIGetRewardsBySmesherIdCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshAPIGetRewardsBySmesherIdCall) Do(f func(types.NodeID) ([]*types.Reward, error)) *MockmeshAPIGetRewardsBySmesherIdCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshAPIGetRewardsBySmesherIdCall) DoAndReturn(f func(types.NodeID) ([]*types.Reward, error)) *MockmeshAPIGetRewardsBySmesherIdCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// LatestLayer mocks base method.
func (m *MockmeshAPI) LatestLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LatestLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// LatestLayer indicates an expected call of LatestLayer.
func (mr *MockmeshAPIMockRecorder) LatestLayer() *MockmeshAPILatestLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LatestLayer", reflect.TypeOf((*MockmeshAPI)(nil).LatestLayer))
	return &MockmeshAPILatestLayerCall{Call: call}
}

// MockmeshAPILatestLayerCall wrap *gomock.Call
type MockmeshAPILatestLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshAPILatestLayerCall) Return(arg0 types.LayerID) *MockmeshAPILatestLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshAPILatestLayerCall) Do(f func() types.LayerID) *MockmeshAPILatestLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshAPILatestLayerCall) DoAndReturn(f func() types.LayerID) *MockmeshAPILatestLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// LatestLayerInState mocks base method.
func (m *MockmeshAPI) LatestLayerInState() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LatestLayerInState")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// LatestLayerInState indicates an expected call of LatestLayerInState.
func (mr *MockmeshAPIMockRecorder) LatestLayerInState() *MockmeshAPILatestLayerInStateCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LatestLayerInState", reflect.TypeOf((*MockmeshAPI)(nil).LatestLayerInState))
	return &MockmeshAPILatestLayerInStateCall{Call: call}
}

// MockmeshAPILatestLayerInStateCall wrap *gomock.Call
type MockmeshAPILatestLayerInStateCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshAPILatestLayerInStateCall) Return(arg0 types.LayerID) *MockmeshAPILatestLayerInStateCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshAPILatestLayerInStateCall) Do(f func() types.LayerID) *MockmeshAPILatestLayerInStateCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshAPILatestLayerInStateCall) DoAndReturn(f func() types.LayerID) *MockmeshAPILatestLayerInStateCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockmeshAPIMockRecorder) MeshHash(arg0 any) *MockmeshAPIMeshHashCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MeshHash", reflect.TypeOf((*MockmeshAPI)(nil).MeshHash), arg0)
	return &MockmeshAPIMeshHashCall{Call: call}
}

// MockmeshAPIMeshHashCall wrap *gomock.Call
type MockmeshAPIMeshHashCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshAPIMeshHashCall) Return(arg0 types.Hash32, arg1 error) *MockmeshAPIMeshHashCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshAPIMeshHashCall) Do(f func(types.LayerID) (types.Hash32, error)) *MockmeshAPIMeshHashCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshAPIMeshHashCall) DoAndReturn(f func(types.LayerID) (types.Hash32, error)) *MockmeshAPIMeshHashCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// ProcessedLayer mocks base method.
func (m *MockmeshAPI) ProcessedLayer() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessedLayer")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// ProcessedLayer indicates an expected call of ProcessedLayer.
func (mr *MockmeshAPIMockRecorder) ProcessedLayer() *MockmeshAPIProcessedLayerCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessedLayer", reflect.TypeOf((*MockmeshAPI)(nil).ProcessedLayer))
	return &MockmeshAPIProcessedLayerCall{Call: call}
}

// MockmeshAPIProcessedLayerCall wrap *gomock.Call
type MockmeshAPIProcessedLayerCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockmeshAPIProcessedLayerCall) Return(arg0 types.LayerID) *MockmeshAPIProcessedLayerCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockmeshAPIProcessedLayerCall) Do(f func() types.LayerID) *MockmeshAPIProcessedLayerCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockmeshAPIProcessedLayerCall) DoAndReturn(f func() types.LayerID) *MockmeshAPIProcessedLayerCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
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
func (mr *MockpeerCounterMockRecorder) PeerCount() *MockpeerCounterPeerCountCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PeerCount", reflect.TypeOf((*MockpeerCounter)(nil).PeerCount))
	return &MockpeerCounterPeerCountCall{Call: call}
}

// MockpeerCounterPeerCountCall wrap *gomock.Call
type MockpeerCounterPeerCountCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpeerCounterPeerCountCall) Return(arg0 uint64) *MockpeerCounterPeerCountCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpeerCounterPeerCountCall) Do(f func() uint64) *MockpeerCounterPeerCountCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpeerCounterPeerCountCall) DoAndReturn(f func() uint64) *MockpeerCounterPeerCountCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Mockpeers is a mock of peers interface.
type Mockpeers struct {
	ctrl     *gomock.Controller
	recorder *MockpeersMockRecorder
}

// MockpeersMockRecorder is the mock recorder for Mockpeers.
type MockpeersMockRecorder struct {
	mock *Mockpeers
}

// NewMockpeers creates a new mock instance.
func NewMockpeers(ctrl *gomock.Controller) *Mockpeers {
	mock := &Mockpeers{ctrl: ctrl}
	mock.recorder = &MockpeersMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *Mockpeers) EXPECT() *MockpeersMockRecorder {
	return m.recorder
}

// ConnectedPeerInfo mocks base method.
func (m *Mockpeers) ConnectedPeerInfo(arg0 p2p.Peer) *p2p.PeerInfo {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ConnectedPeerInfo", arg0)
	ret0, _ := ret[0].(*p2p.PeerInfo)
	return ret0
}

// ConnectedPeerInfo indicates an expected call of ConnectedPeerInfo.
func (mr *MockpeersMockRecorder) ConnectedPeerInfo(arg0 any) *MockpeersConnectedPeerInfoCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ConnectedPeerInfo", reflect.TypeOf((*Mockpeers)(nil).ConnectedPeerInfo), arg0)
	return &MockpeersConnectedPeerInfoCall{Call: call}
}

// MockpeersConnectedPeerInfoCall wrap *gomock.Call
type MockpeersConnectedPeerInfoCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpeersConnectedPeerInfoCall) Return(arg0 *p2p.PeerInfo) *MockpeersConnectedPeerInfoCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpeersConnectedPeerInfoCall) Do(f func(p2p.Peer) *p2p.PeerInfo) *MockpeersConnectedPeerInfoCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpeersConnectedPeerInfoCall) DoAndReturn(f func(p2p.Peer) *p2p.PeerInfo) *MockpeersConnectedPeerInfoCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetPeers mocks base method.
func (m *Mockpeers) GetPeers() []p2p.Peer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPeers")
	ret0, _ := ret[0].([]p2p.Peer)
	return ret0
}

// GetPeers indicates an expected call of GetPeers.
func (mr *MockpeersMockRecorder) GetPeers() *MockpeersGetPeersCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPeers", reflect.TypeOf((*Mockpeers)(nil).GetPeers))
	return &MockpeersGetPeersCall{Call: call}
}

// MockpeersGetPeersCall wrap *gomock.Call
type MockpeersGetPeersCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockpeersGetPeersCall) Return(arg0 []p2p.Peer) *MockpeersGetPeersCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockpeersGetPeersCall) Do(f func() []p2p.Peer) *MockpeersGetPeersCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockpeersGetPeersCall) DoAndReturn(f func() []p2p.Peer) *MockpeersGetPeersCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
