package discovery

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// MockPeerStore is a mocked discovery
type MockPeerStore struct {
	UpdateFunc      func(n, src *node.NodeInfo)
	updateCount     int
	SelectPeersFunc func(ctx context.Context, qty int) []*node.NodeInfo
	bsres           error
	bsCount         int
	LookupFunc      func(p2pcrypto.PublicKey) (*node.NodeInfo, error)
	lookupRes       *node.NodeInfo
	lookupErr       error

	IsLocalAddressFunc func(info *node.NodeInfo) bool

	RemoveFunc  func(key p2pcrypto.PublicKey)
	GoodFunc    func(key p2pcrypto.PublicKey)
	AttemptFunc func(key p2pcrypto.PublicKey)
}

func (m *MockPeerStore) Remove(key p2pcrypto.PublicKey) {
	if m.RemoveFunc != nil {
		m.RemoveFunc(key)
	}
}

func (m *MockPeerStore) IsLocalAddress(info *node.NodeInfo) bool {
	if m.IsLocalAddressFunc != nil {
		return m.IsLocalAddressFunc(info)
	}
	return false
}

// SetUpdate sets the function to run on an issued update
func (m *MockPeerStore) SetUpdate(f func(n, addr *node.NodeInfo)) {
	m.UpdateFunc = f
}

// SetLookupResult sets the result ok a lookup operation
func (m *MockPeerStore) SetLookupResult(node *node.NodeInfo, err error) {
	m.lookupRes = node
	m.lookupErr = err
}

// Update is a discovery update operation it updates the updatecount
func (m *MockPeerStore) Update(n, src *node.NodeInfo) {
	if m.UpdateFunc != nil {
		m.UpdateFunc(n, src)
	}
	m.updateCount++
}

// UpdateCount returns the number of times update was called
func (m *MockPeerStore) UpdateCount() int {
	return m.updateCount
}

// BootstrapCount returns the number of times bootstrap was called
func (m *MockPeerStore) BootstrapCount() int {
	return m.bsCount
}

// netLookup is a discovery lookup operation
func (m *MockPeerStore) Lookup(pubkey p2pcrypto.PublicKey) (*node.NodeInfo, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(pubkey)
	}
	return m.lookupRes, m.lookupErr
}

// SetBootstrap set the bootstrap result
func (m *MockPeerStore) SetBootstrap(err error) {
	m.bsres = err
}

// Bootstrap is a discovery bootstrap operation function it update the bootstrap count
func (m *MockPeerStore) Bootstrap(ctx context.Context) error {
	m.bsCount++
	return m.bsres
}

// SelectPeers mocks selecting peers.
func (m *MockPeerStore) SelectPeers(ctx context.Context, qty int) []*node.NodeInfo {
	if m.SelectPeersFunc != nil {
		return m.SelectPeersFunc(ctx, qty)
	}
	return []*node.NodeInfo{}
}

// to satisfy the iface
func (m *MockPeerStore) SetLocalAddresses(tcp, udp int) {

}

// Size returns the size of peers in the discovery
func (m *MockPeerStore) Size() int {
	//todo: set size
	return m.updateCount
}

func (m *MockPeerStore) Shutdown() {

}

func (m *MockPeerStore) Good(key p2pcrypto.PublicKey) {
	if m.GoodFunc != nil {
		m.GoodFunc(key)
	}
}
func (m *MockPeerStore) Attempt(key p2pcrypto.PublicKey) {
	if m.AttemptFunc != nil {
		m.AttemptFunc(key)
	}
}

// mockAddrBook
type mockAddrBook struct {
	addAddressFunc func(n, src *node.NodeInfo)
	addressCount   int

	LookupFunc func(p2pcrypto.PublicKey) (*node.NodeInfo, error)
	lookupRes  *node.NodeInfo
	lookupErr  error

	GetAddressFunc func() *KnownAddress
	GetAddressRes  *KnownAddress

	NeedNewAddressesFunc func() bool

	AddressCacheResult []*node.NodeInfo

	GoodFunc    func(key p2pcrypto.PublicKey)
	AttemptFunc func(key p2pcrypto.PublicKey)

	IsLocalAddressFunc  func(info *node.NodeInfo) bool
	AddLocalAddressFunc func(info *node.NodeInfo)
}

func (m *mockAddrBook) Stop() {

}

func (m *mockAddrBook) AddLocalAddress(info *node.NodeInfo) {
	if m.AddLocalAddressFunc != nil {
		m.AddLocalAddressFunc(info)
	}
}

func (m *mockAddrBook) IsLocalAddress(info *node.NodeInfo) bool {
	if m.IsLocalAddressFunc != nil {
		return m.IsLocalAddressFunc(info)
	}
	return false
}

func (m *mockAddrBook) RemoveAddress(key p2pcrypto.PublicKey) {

}

func (m *mockAddrBook) Good(key p2pcrypto.PublicKey) {
	if m.GoodFunc != nil {
		m.GoodFunc(key)
	}
}
func (m *mockAddrBook) Attempt(key p2pcrypto.PublicKey) {
	if m.AttemptFunc != nil {
		m.AttemptFunc(key)
	}
}

func (m *mockAddrBook) NeedNewAddresses() bool {
	if m.NeedNewAddressesFunc != nil {
		return m.NeedNewAddressesFunc()
	}
	return false
}

// SetUpdate sets the function to run on an issued update
func (m *mockAddrBook) SetUpdate(f func(n, addr *node.NodeInfo)) {
	m.addAddressFunc = f
}

// SetLookupResult sets the result ok a lookup operation
func (m *mockAddrBook) SetLookupResult(node *node.NodeInfo, err error) {
	m.lookupRes = node
	m.lookupErr = err
}

// AddAddress mock
func (m *mockAddrBook) AddAddress(n, src *node.NodeInfo) {
	if m.addAddressFunc != nil {
		m.addAddressFunc(n, src)
	}
	m.addressCount++
}

// AddAddresses mock
func (m *mockAddrBook) AddAddresses(n []*node.NodeInfo, src *node.NodeInfo) {
	if m.addAddressFunc != nil {
		for _, addr := range n {
			m.addAddressFunc(addr, src)
			m.addressCount++
		}
	}
}

// AddAddressCount counts AddAddress calls
func (m *mockAddrBook) AddAddressCount() int {
	return m.addressCount
}

// AddressCache mock
func (m *mockAddrBook) AddressCache() []*node.NodeInfo {
	return m.AddressCacheResult
}

// Lookup mock
func (m *mockAddrBook) Lookup(pubkey p2pcrypto.PublicKey) (*node.NodeInfo, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(pubkey)
	}
	return m.lookupRes, m.lookupErr
}

// GetAddress mock
func (m *mockAddrBook) GetAddress() *KnownAddress {
	if m.GetAddressFunc != nil {
		return m.GetAddressFunc()
	}
	return m.GetAddressRes
}

// NumAddresses mock
func (m *mockAddrBook) NumAddresses() int {
	//todo: mockAddrBook size
	return m.addressCount
}

type refresherMock struct {
	BootstrapFunc func(ctx context.Context, minPeers int) error
}

func (r *refresherMock) Bootstrap(ctx context.Context, minPeers int) error {
	if r.BootstrapFunc != nil {
		return r.BootstrapFunc(ctx, minPeers)
	}
	return nil
}
