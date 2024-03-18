package v2alpha1

import (
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
