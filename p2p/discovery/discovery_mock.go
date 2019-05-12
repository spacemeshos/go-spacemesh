package discovery

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// MockDiscovery is a mocked discovery
type MockDiscovery struct {
	UpdateFunc      func(n, src node.Node)
	updateCount     int
	SelectPeersFunc func(qty int) []node.Node
	bsres           error
	bsCount         int
	LookupFunc      func(p2pcrypto.PublicKey) (node.Node, error)
	lookupRes       node.Node
	lookupErr       error
}

func (m *MockDiscovery) Remove(key p2pcrypto.PublicKey) {

}

// SetUpdate sets the function to run on an issued update
func (m *MockDiscovery) SetUpdate(f func(n, addr node.Node)) {
	m.UpdateFunc = f
}

// SetLookupResult sets the result ok a lookup operation
func (m *MockDiscovery) SetLookupResult(node node.Node, err error) {
	m.lookupRes = node
	m.lookupErr = err
}

// Update is a discovery update operation it updates the updatecount
func (m *MockDiscovery) Update(n, src node.Node) {
	if m.UpdateFunc != nil {
		m.UpdateFunc(n, src)
	}
	m.updateCount++
}

// UpdateCount returns the number of times update was called
func (m *MockDiscovery) UpdateCount() int {
	return m.updateCount
}

// BootstrapCount returns the number of times bootstrap was called
func (m *MockDiscovery) BootstrapCount() int {
	return m.bsCount
}

// netLookup is a discovery lookup operation
func (m *MockDiscovery) Lookup(pubkey p2pcrypto.PublicKey) (node.Node, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(pubkey)
	}
	return m.lookupRes, m.lookupErr
}

// SetBootstrap set the bootstrap result
func (m *MockDiscovery) SetBootstrap(err error) {
	m.bsres = err
}

// Bootstrap is a discovery bootstrap operation function it update the bootstrap count
func (m *MockDiscovery) Bootstrap(ctx context.Context) error {
	m.bsCount++
	return m.bsres
}

// SelectPeers mocks selecting peers.
func (m *MockDiscovery) SelectPeers(qty int) []node.Node {
	if m.SelectPeersFunc != nil {
		return m.SelectPeersFunc(qty)
	}
	return []node.Node{}
}

// to satisfy the iface
func (m *MockDiscovery) SetLocalAddresses(tcp, udp string) {

}

// Size returns the size of peers in the discovery
func (m *MockDiscovery) Size() int {
	//todo: set size
	return m.updateCount
}

// mockAddrBook
type mockAddrBook struct {
	addAddressFunc func(n, src discNode)
	addressCount   int

	LookupFunc func(p2pcrypto.PublicKey) (discNode, error)
	lookupRes  discNode
	lookupErr  error

	GetAddressFunc func() *KnownAddress
	GetAddressRes  *KnownAddress

	AddressCacheResult []discNode
}

func (m *mockAddrBook) RemoveAddress(key p2pcrypto.PublicKey) {

}

// SetUpdate sets the function to run on an issued update
func (m *mockAddrBook) SetUpdate(f func(n, addr discNode)) {
	m.addAddressFunc = f
}

// SetLookupResult sets the result ok a lookup operation
func (m *mockAddrBook) SetLookupResult(node discNode, err error) {
	m.lookupRes = node
	m.lookupErr = err
}

// AddAddress mock
func (m *mockAddrBook) AddAddress(n, src discNode) {
	if m.addAddressFunc != nil {
		m.addAddressFunc(n, src)
	}
	m.addressCount++
}

// AddAddresses mock
func (m *mockAddrBook) AddAddresses(n []discNode, src discNode) {
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
func (m *mockAddrBook) AddressCache() []discNode {
	return m.AddressCacheResult
}

// Lookup mock
func (m *mockAddrBook) Lookup(pubkey p2pcrypto.PublicKey) (discNode, error) {
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
