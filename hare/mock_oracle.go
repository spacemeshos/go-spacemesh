package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
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

type Stringer interface {
	String() string
}

type Registrable interface {
	Register(isHonest bool, id string)
	Unregister(isHonest bool, id string)
}

type hasherU32 struct {
}

func newHasherU32() *hasherU32 {
	h := new(hasherU32)

	return h
}

func (h *hasherU32) Hash(values ...[]byte) uint32 {
	fnv := fnv.New32()
	for _, b := range values {
		fnv.Write(b)
	}
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

func (mock *MockHashOracle) IsIdentityActiveOnConsensusView(edId string, layer types.LayerID) (bool, error) {
	return true, nil
}

// N is the expected comity size
func NewMockHashOracle(expectedSize int) *MockHashOracle {
	mock := new(MockHashOracle)
	mock.clients = make(map[string]struct{}, expectedSize)
	mock.hasher = newHasherU32()

	return mock
}

func (mock *MockHashOracle) Register(client string) {
	mock.mutex.Lock()

	if _, exist := mock.clients[client]; exist {
		mock.mutex.Unlock()
		return
	}

	mock.clients[client] = struct{}{}
	mock.mutex.Unlock()
}

func (mock *MockHashOracle) Unregister(client string) {
	mock.mutex.Lock()
	delete(mock.clients, client)
	mock.mutex.Unlock()
}

// Calculates the threshold for the given committee size
func (mock *MockHashOracle) calcThreshold(committeeSize int) uint32 {
	mock.mutex.RLock()
	numClients := len(mock.clients)
	mock.mutex.RUnlock()

	if numClients == 0 {
		log.Error("Called calcThreshold with 0 clients registered")
		return 0
	}

	if committeeSize > numClients {
		/*log.Error("Requested for a committee bigger than the number of registered clients. Expected at least %v clients Actual: %v",
		committeeSize, numClients)*/
		return 0
	}

	return uint32(uint64(committeeSize) * uint64(mock.hasher.MaxValue()) / uint64(numClients))
}

// Eligible if a proof is valid for a given committee size
func (mock *MockHashOracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	if sig == nil {
		log.Warning("Oracle query with proof=nil. Returning false")
		return false, errors.New("sig is nil")
	}

	// calculate hash of proof
	proofHash := mock.hasher.Hash(sig)
	if proofHash <= mock.calcThreshold(committeeSize) { // check threshold
		return true, nil
	}

	return false, nil
}

func (m *MockHashOracle) Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	return []byte{}, nil
}
