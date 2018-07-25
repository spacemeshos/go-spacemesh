package dht

import "github.com/spacemeshos/go-spacemesh/p2p/node"

type DHT interface {
	Update(node node.Node)
	Lookup(pubkey string) (node.Node, error)
	Bootstrap() error
}

type MockDHT struct {
	update      func(n node.Node)
	updateCount int
	bsres       error
	bsCount     int
	lookupRes   node.Node
	lookupErr   error
}

func (m *MockDHT) SetUpdate(f func(n node.Node)) {
	m.update = f
}

func (m *MockDHT) SetLookupResult(node node.Node, err error) {
	m.lookupRes = node
	m.lookupErr = err
}

func (m *MockDHT) Update(node node.Node) {
	if m.update != nil {
		m.update(node)
	}
	m.updateCount++
}

func (m *MockDHT) UpdateCount() int {
	return m.updateCount
}

func (m *MockDHT) BootstrapCount() int {
	return m.bsCount
}

func (m *MockDHT) Lookup(pubkey string) (node.Node, error) {
	return m.lookupRes, m.lookupErr
}

func (m *MockDHT) SetBootstrap(err error) {
	m.bsres = err
}

func (m *MockDHT) Bootstrap() error {
	m.bsCount++
	return m.bsres
}
