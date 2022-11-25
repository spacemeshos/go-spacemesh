// Code generated by MockGen. DO NOT EDIT.
// Source: ./interface.go

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"
	time "time"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
)

// MockatxReceiver is a mock of atxReceiver interface.
type MockatxReceiver struct {
	ctrl     *gomock.Controller
	recorder *MockatxReceiverMockRecorder
}

// MockatxReceiverMockRecorder is the mock recorder for MockatxReceiver.
type MockatxReceiverMockRecorder struct {
	mock *MockatxReceiver
}

// NewMockatxReceiver creates a new mock instance.
func NewMockatxReceiver(ctrl *gomock.Controller) *MockatxReceiver {
	mock := &MockatxReceiver{ctrl: ctrl}
	mock.recorder = &MockatxReceiverMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockatxReceiver) EXPECT() *MockatxReceiverMockRecorder {
	return m.recorder
}

// OnAtx mocks base method.
func (m *MockatxReceiver) OnAtx(arg0 *types.ActivationTxHeader) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnAtx", arg0)
}

// OnAtx indicates an expected call of OnAtx.
func (mr *MockatxReceiverMockRecorder) OnAtx(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnAtx", reflect.TypeOf((*MockatxReceiver)(nil).OnAtx), arg0)
}

// MockpoetValidatorPersister is a mock of poetValidatorPersister interface.
type MockpoetValidatorPersister struct {
	ctrl     *gomock.Controller
	recorder *MockpoetValidatorPersisterMockRecorder
}

// MockpoetValidatorPersisterMockRecorder is the mock recorder for MockpoetValidatorPersister.
type MockpoetValidatorPersisterMockRecorder struct {
	mock *MockpoetValidatorPersister
}

// NewMockpoetValidatorPersister creates a new mock instance.
func NewMockpoetValidatorPersister(ctrl *gomock.Controller) *MockpoetValidatorPersister {
	mock := &MockpoetValidatorPersister{ctrl: ctrl}
	mock.recorder = &MockpoetValidatorPersisterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpoetValidatorPersister) EXPECT() *MockpoetValidatorPersisterMockRecorder {
	return m.recorder
}

// HasProof mocks base method.
func (m *MockpoetValidatorPersister) HasProof(arg0 types.PoetProofRef) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasProof", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// HasProof indicates an expected call of HasProof.
func (mr *MockpoetValidatorPersisterMockRecorder) HasProof(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasProof", reflect.TypeOf((*MockpoetValidatorPersister)(nil).HasProof), arg0)
}

// StoreProof mocks base method.
func (m *MockpoetValidatorPersister) StoreProof(arg0 types.PoetProofRef, arg1 *types.PoetProofMessage) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StoreProof", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// StoreProof indicates an expected call of StoreProof.
func (mr *MockpoetValidatorPersisterMockRecorder) StoreProof(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StoreProof", reflect.TypeOf((*MockpoetValidatorPersister)(nil).StoreProof), arg0, arg1)
}

// Validate mocks base method.
func (m *MockpoetValidatorPersister) Validate(arg0 types.PoetProof, arg1 []byte, arg2 string, arg3 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validate", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// Validate indicates an expected call of Validate.
func (mr *MockpoetValidatorPersisterMockRecorder) Validate(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validate", reflect.TypeOf((*MockpoetValidatorPersister)(nil).Validate), arg0, arg1, arg2, arg3)
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

// Validate mocks base method.
func (m *MocknipostValidator) Validate(nodeId types.NodeID, atxId types.ATXID, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint32) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Validate", nodeId, atxId, NIPost, expectedChallenge, numUnits)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Validate indicates an expected call of Validate.
func (mr *MocknipostValidatorMockRecorder) Validate(nodeId, atxId, NIPost, expectedChallenge, numUnits interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Validate", reflect.TypeOf((*MocknipostValidator)(nil).Validate), nodeId, atxId, NIPost, expectedChallenge, numUnits)
}

// ValidatePost mocks base method.
func (m *MocknipostValidator) ValidatePost(nodeId types.NodeID, atxId types.ATXID, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint32) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ValidatePost", nodeId, atxId, Post, PostMetadata, numUnits)
	ret0, _ := ret[0].(error)
	return ret0
}

// ValidatePost indicates an expected call of ValidatePost.
func (mr *MocknipostValidatorMockRecorder) ValidatePost(nodeId, atxId, Post, PostMetadata, numUnits interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ValidatePost", reflect.TypeOf((*MocknipostValidator)(nil).ValidatePost), nodeId, atxId, Post, PostMetadata, numUnits)
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
