package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"hash/fnv"
	"math"
	"sync"
)

type Role byte

const (
	Passive = Role(0)
	Active  = Role(1)
	Leader  = Role(2)
)

type Rolacle interface {
	Role(sig Signature) Role
}

type MockHashOracle struct {
	clients    map[string]struct{}
	comitySize int
	mutex      sync.Mutex
}

// N is the expected comity size
func NewMockHashOracle(expectedSize int, comitySize int) *MockHashOracle {
	mock := new(MockHashOracle)
	mock.clients = make(map[string]struct{}, expectedSize)
	mock.comitySize = comitySize

	return mock
}

func (mock *MockHashOracle) Register(pubKey crypto.PublicKey) {
	mock.mutex.Lock()
	defer mock.mutex.Unlock()

	if _, exist := mock.clients[pubKey.String()]; exist {
		return
	}

	mock.clients[pubKey.String()] = struct{}{}
}

func (mock *MockHashOracle) Unregister(pubKey crypto.PublicKey) {
	mock.mutex.Lock()
	delete(mock.clients, pubKey.String())
	mock.mutex.Unlock()
}

func (mock *MockHashOracle) calcThreshold(comitySize int) uint32 {
	if len(mock.clients) == 0 {
		panic("Called calcThreshold with 0 clients registered")
	}

	return uint32(uint64(comitySize) * uint64(math.MaxUint32) / uint64(len(mock.clients)))
}

func (mock *MockHashOracle) Role(proof Signature) Role {
	if proof == nil {
		log.Warning("Oracle query with proof=nil. Returning passive")
		return Passive
	}

	// calculate thresholds
	threshLeader := mock.calcThreshold(3)               // expect 3 leaders
	threshActive := mock.calcThreshold(mock.comitySize) // expect comitySize-3 actives

	// calculate hash of proof
	hash := fnv.New32()
	hash.Write(proof)
	proofHash := hash.Sum32()
	if proofHash < threshLeader {
		return Leader
	}

	if proofHash < threshActive {
		return Active
	}

	// the rest are passive
	return Passive
}

type MockStaticOracle struct {
	roles       map[uint32]Role
	r           uint32
	defaultSize int
	hasLeader   bool
	mutex       sync.Mutex
}

func NewMockStaticOracle(defaultSize int) *MockStaticOracle {
	static := &MockStaticOracle{}
	static.roles = make(map[uint32]Role, defaultSize)
	static.defaultSize = defaultSize
	static.hasLeader = false

	return static
}

func (static *MockStaticOracle) Role(r uint32, proof Signature) Role {
	return roleFromRoundCounter(r)
}
