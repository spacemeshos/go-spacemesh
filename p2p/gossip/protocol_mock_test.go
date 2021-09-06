// Code generated by MockGen. DO NOT EDIT.
// Source: ./protocol.go

// Package gossip is a generated GoMock package.
package gossip

import (
	context "context"
	gomock "github.com/golang/mock/gomock"
	p2pcrypto "github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	peers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	service "github.com/spacemeshos/go-spacemesh/p2p/service"
	reflect "reflect"
)

// MockpeersManager is a mock of peersManager interface
type MockpeersManager struct {
	ctrl     *gomock.Controller
	recorder *MockpeersManagerMockRecorder
}

// MockpeersManagerMockRecorder is the mock recorder for MockpeersManager
type MockpeersManagerMockRecorder struct {
	mock *MockpeersManager
}

// NewMockpeersManager creates a new mock instance
func NewMockpeersManager(ctrl *gomock.Controller) *MockpeersManager {
	mock := &MockpeersManager{ctrl: ctrl}
	mock.recorder = &MockpeersManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockpeersManager) EXPECT() *MockpeersManagerMockRecorder {
	return m.recorder
}

// GetPeers mocks base method
func (m *MockpeersManager) GetPeers() []peers.Peer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPeers")
	ret0, _ := ret[0].([]peers.Peer)
	return ret0
}

// GetPeers indicates an expected call of GetPeers
func (mr *MockpeersManagerMockRecorder) GetPeers() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPeers", reflect.TypeOf((*MockpeersManager)(nil).GetPeers))
}

// PeerCount mocks base method
func (m *MockpeersManager) PeerCount() uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PeerCount")
	ret0, _ := ret[0].(uint64)
	return ret0
}

// PeerCount indicates an expected call of PeerCount
func (mr *MockpeersManagerMockRecorder) PeerCount() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PeerCount", reflect.TypeOf((*MockpeersManager)(nil).PeerCount))
}

// Close mocks base method
func (m *MockpeersManager) Close() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Close")
}

// Close indicates an expected call of Close
func (mr *MockpeersManagerMockRecorder) Close() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockpeersManager)(nil).Close))
}

// MockbaseNetwork is a mock of baseNetwork interface
type MockbaseNetwork struct {
	ctrl     *gomock.Controller
	recorder *MockbaseNetworkMockRecorder
}

// MockbaseNetworkMockRecorder is the mock recorder for MockbaseNetwork
type MockbaseNetworkMockRecorder struct {
	mock *MockbaseNetwork
}

// NewMockbaseNetwork creates a new mock instance
func NewMockbaseNetwork(ctrl *gomock.Controller) *MockbaseNetwork {
	mock := &MockbaseNetwork{ctrl: ctrl}
	mock.recorder = &MockbaseNetworkMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockbaseNetwork) EXPECT() *MockbaseNetworkMockRecorder {
	return m.recorder
}

// SendMessage mocks base method
func (m *MockbaseNetwork) SendMessage(ctx context.Context, peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendMessage", ctx, peerPubkey, protocol, payload)
	ret0, _ := ret[0].(error)
	return ret0
}

// SendMessage indicates an expected call of SendMessage
func (mr *MockbaseNetworkMockRecorder) SendMessage(ctx, peerPubkey, protocol, payload interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendMessage", reflect.TypeOf((*MockbaseNetwork)(nil).SendMessage), ctx, peerPubkey, protocol, payload)
}

// SubscribePeerEvents mocks base method
func (m *MockbaseNetwork) SubscribePeerEvents() (chan p2pcrypto.PublicKey, chan p2pcrypto.PublicKey) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SubscribePeerEvents")
	ret0, _ := ret[0].(chan p2pcrypto.PublicKey)
	ret1, _ := ret[1].(chan p2pcrypto.PublicKey)
	return ret0, ret1
}

// SubscribePeerEvents indicates an expected call of SubscribePeerEvents
func (mr *MockbaseNetworkMockRecorder) SubscribePeerEvents() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SubscribePeerEvents", reflect.TypeOf((*MockbaseNetwork)(nil).SubscribePeerEvents))
}

// ProcessGossipProtocolMessage mocks base method
func (m *MockbaseNetwork) ProcessGossipProtocolMessage(ctx context.Context, sender p2pcrypto.PublicKey, ownMessage bool, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessGossipProtocolMessage", ctx, sender, ownMessage, protocol, data, validationCompletedChan)
	ret0, _ := ret[0].(error)
	return ret0
}

// ProcessGossipProtocolMessage indicates an expected call of ProcessGossipProtocolMessage
func (mr *MockbaseNetworkMockRecorder) ProcessGossipProtocolMessage(ctx, sender, ownMessage, protocol, data, validationCompletedChan interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessGossipProtocolMessage", reflect.TypeOf((*MockbaseNetwork)(nil).ProcessGossipProtocolMessage), ctx, sender, ownMessage, protocol, data, validationCompletedChan)
}
