package dht

import (
	"context"
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
	LookupFunc         func(string) (node.Node, error)
	lookupRes          node.Node
	lookupErr          error
}

// SetUpdate sets the function to run on an issued UpdateFunc
func (m *MockDHT) SetUpdate(f func(n node.Node)) {
	m.UpdateFunc = f
}

// SetLookupResult sets the result ok a LookupFunc operation
func (m *MockDHT) SetLookupResult(node node.Node, err error) {
	m.lookupRes = node
	m.lookupErr = err
}

// Update is a dht UpdateFunc operation it updates the updatecount
func (m *MockDHT) Update(node node.Node) {
	if m.UpdateFunc != nil {
		m.UpdateFunc(node)
	}
	m.updateCount++
}

// UpdateCount returns the number of times UpdateFunc was called
func (m *MockDHT) UpdateCount() int {
	return m.updateCount
}

// BootstrapCount returns the number of times bootstrap was called
func (m *MockDHT) BootstrapCount() int {
	return m.bsCount
}

// Lookup is a dht LookupFunc operation
func (m *MockDHT) Lookup(pubkey string) (node.Node, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(pubkey)
	}
	return m.lookupRes, m.lookupErr
}

// Lookup is a dht LookupFunc operation
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

// Bootstrap is a dht bootstrap operation function it UpdateFunc the bootstrap count
func (m *MockDHT) Bootstrap(ctx context.Context) error {
	m.bsCount++
	return m.bsres
}

func (m *MockDHT) SelectPeers(qty int) []node.Node {
	if m.SelectPeersFunc != nil {
		return m.SelectPeersFunc(qty)
	}
	return []node.Node{}
}

func (m *MockDHT) Size() int {
	//todo: set size
	return m.updateCount
}
