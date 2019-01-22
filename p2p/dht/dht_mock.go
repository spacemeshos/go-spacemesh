package dht

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

// MockDHT is a mocked dht
type MockDHT struct {
	UpdateFunc         func(n node.Node)
	updateCount        int
	SelectPeersFunc func(qty int) []node.Node
	bsres              error
	bsCount            int
	InternalLookupFunc func(dhtid node.DhtID) []node.Node
	LookupFunc         func(p2pcrypto.PublicKey) (node.Node, error)
	lookupRes          node.Node
	lookupErr          error
}

// SetUpdate sets the function to run on an issued update
func (m *MockDHT) SetUpdate(f func(n node.Node)) {
	m.UpdateFunc = f
}

// SetLookupResult sets the result ok a lookup operation
func (m *MockDHT) SetLookupResult(node node.Node, err error) {
	m.lookupRes = node
	m.lookupErr = err
}

// Update is a dht update operation it updates the updatecount
func (m *MockDHT) Update(node node.Node) {
	if m.UpdateFunc != nil {
		m.UpdateFunc(node)
	}
	m.updateCount++
}

// UpdateCount returns the number of times update was called
func (m *MockDHT) UpdateCount() int {
	return m.updateCount
}

// BootstrapCount returns the number of times bootstrap was called
func (m *MockDHT) BootstrapCount() int {
	return m.bsCount
}

// Lookup is a dht lookup operation
func (m *MockDHT) Lookup(pubkey p2pcrypto.PublicKey) (node.Node, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(pubkey)
	}
	return m.lookupRes, m.lookupErr
}

// InternalLookup is a lookup only in the local routing table
func (m *MockDHT) InternalLookup(dhtid node.DhtID) []node.Node {
	if m.InternalLookupFunc != nil {
		return m.InternalLookupFunc(dhtid)
	}
	return nil
}

// SetBootstrap set the bootstrap result
func (m *MockDHT) SetBootstrap(err error) {
	m.bsres = err
}

// Bootstrap is a dht bootstrap operation function it update the bootstrap count
func (m *MockDHT) Bootstrap(ctx context.Context) error {
	m.bsCount++
	return m.bsres
}


// SelectPeers mocks selecting peers.
func (m *MockDHT) SelectPeers(qty int) []node.Node {
	if m.SelectPeersFunc != nil {
		return m.SelectPeersFunc(qty)
	}
	return []node.Node{}
}

// Size returns the size of peers in the dht
func (m *MockDHT) Size() int {
	//todo: set size
	return m.updateCount
}
