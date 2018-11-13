package dht

import "github.com/spacemeshos/go-spacemesh/p2p/node"

// MockDHT is a mocked dht
type MockDHT struct {
	update      func(n node.Node)
	updateCount int
	bsres       error
	bsCount     int
	lookupRes   node.Node
	lookupErr   error
}

// SetUpdate sets the function to run on an issued update
func (m *MockDHT) SetUpdate(f func(n node.Node)) {
	m.update = f
}

// SetLookupResult sets the result ok a lookup operation
func (m *MockDHT) SetLookupResult(node node.Node, err error) {
	m.lookupRes = node
	m.lookupErr = err
}

// Update is a dht update operation it updates the updatecount
func (m *MockDHT) Update(node node.Node) {
	if m.update != nil {
		m.update(node)
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
func (m *MockDHT) Lookup(pubkey string) (node.Node, error) {
	return m.lookupRes, m.lookupErr
}

// SetBootstrap set the bootstrap result
func (m *MockDHT) SetBootstrap(err error) {
	m.bsres = err
}

// Bootstrap is a dht bootstrap operation function it update the bootstrap count
func (m *MockDHT) Bootstrap() error {
	m.bsCount++
	return m.bsres
}

func (m *MockDHT) SelectPeers(qty int) []node.Node {
	return []node.Node{}
}

func (m *MockDHT) Size() int {
	//todo: set size
	return m.updateCount
}
