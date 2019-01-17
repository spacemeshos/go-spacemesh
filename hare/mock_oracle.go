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
	Validate(committeeSize int, proof Signature) bool
}

type hasherU32 struct {
}

func newHasherU32() *hasherU32 {
	h := new(hasherU32)

	return h
}

func (h *hasherU32) Hash(data []byte) uint32 {
	fnv := fnv.New32()
	fnv.Write(data)
	return fnv.Sum32()
}

func (h *hasherU32) MaxValue() uint32 {
	return math.MaxUint32
}

type MockHashOracle struct {
	clients map[string]struct{}
	mutex   sync.RWMutex
	hasher  *hasherU32
}

// N is the expected comity size
func NewMockHashOracle(expectedSize int) *MockHashOracle {
	mock := new(MockHashOracle)
	mock.clients = make(map[string]struct{}, expectedSize)
	mock.hasher = newHasherU32()

	return mock
}

func (mock *MockHashOracle) Register(pubKey crypto.PublicKey) {
	mock.mutex.Lock()

	if _, exist := mock.clients[pubKey.String()]; exist {
		mock.mutex.Unlock()
		return
	}

	mock.clients[pubKey.String()] = struct{}{}
	mock.mutex.Unlock()
}

func (mock *MockHashOracle) Unregister(pubKey crypto.PublicKey) {
	mock.mutex.Lock()
	delete(mock.clients, pubKey.String())
	mock.mutex.Unlock()
}

// Calculates the threshold for the given committee size
func (mock *MockHashOracle) calcThreshold(committeeSize int) uint32 {
	mock.mutex.RLock()
	defer mock.mutex.RUnlock()

	if len(mock.clients) == 0 {
		log.Error("Called calcThreshold with 0 clients registered")
		return 0
	}

	if committeeSize > len(mock.clients) {
		log.Error("Requested for a committee bigger than the number of registered clients. Expected at least %v clients Actual: %v",
			committeeSize, len(mock.clients))
		return 0
	}

	return uint32(uint64(committeeSize) * uint64(mock.hasher.MaxValue()) / uint64(len(mock.clients)))
}

// Validate if a proof is valid for a given committee size
func (mock *MockHashOracle) Validate(committeeSize int, proof Signature) bool {
	if proof == nil {
		log.Warning("Oracle query with proof=nil. Returning false")
		return false
	}

	// calculate hash of proof
	proofHash := mock.hasher.Hash(proof)
	if proofHash <= mock.calcThreshold(committeeSize) { // check threshold
		return true
	}

	return false
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
