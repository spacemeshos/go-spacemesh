package hare

import (
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

type Stringer interface {
	String() string
}

type Registrable interface {
	Register(isHonest bool, id string)
	Unregister(isHonest bool, id string)
}

type Rolacle interface {
	Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool
}

type HareRolacle interface {
	Eligible(instanceId InstanceId, k int32, pubKey string, proof []byte) bool
}

type hareRolacle struct {
	oracle        Rolacle
	committeeSize int
}

func newHareOracle(oracle Rolacle, committeeSize int) *hareRolacle {
	return &hareRolacle{oracle, committeeSize}
}

func (hr *hareRolacle) Eligible(instanceId InstanceId, k int32, pubKey string, proof []byte) bool {
	return hr.oracle.Eligible(hashInstanceAndK(instanceId, k), expectedCommitteeSize(k, hr.committeeSize), pubKey, proof)
}

func expectedCommitteeSize(k int32, n int) int {
	if k%4 == Round2 {
		return 1 // 1 leader
	}

	// N actives
	return n
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
func (mock *MockHashOracle) Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool {
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
