package hare

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"hash/fnv"
	"math"
	"sync"
)

type registrable interface {
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

type mockHashOracle struct {
	clients map[string]struct{}
	mutex   sync.RWMutex
	hasher  *hasherU32
}

func (mho *mockHashOracle) IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error) {
	return true, nil
}

func newMockHashOracle(expectedSize int) *mockHashOracle {
	mock := new(mockHashOracle)
	mock.clients = make(map[string]struct{}, expectedSize)
	mock.hasher = newHasherU32()

	return mock
}

func (mho *mockHashOracle) Register(client string) {
	mho.mutex.Lock()

	if _, exist := mho.clients[client]; exist {
		mho.mutex.Unlock()
		return
	}

	mho.clients[client] = struct{}{}
	mho.mutex.Unlock()
}

func (mho *mockHashOracle) Unregister(client string) {
	mho.mutex.Lock()
	delete(mho.clients, client)
	mho.mutex.Unlock()
}

// Calculates the threshold for the given committee size
func (mho *mockHashOracle) calcThreshold(committeeSize int) uint32 {
	mho.mutex.RLock()
	numClients := len(mho.clients)
	mho.mutex.RUnlock()

	if numClients == 0 {
		log.Error("Called calcThreshold with 0 clients registered")
		return 0
	}

	if committeeSize > numClients {
		/*log.Error("Requested for a committee bigger than the number of registered clients. Expected at least %v clients Actual: %v",
		committeeSize, numClients)*/
		return 0
	}

	return uint32(uint64(committeeSize) * uint64(mho.hasher.MaxValue()) / uint64(numClients))
}

func (mho *mockHashOracle) Validate(context.Context, types.LayerID, int32, int, types.NodeID, []byte, uint16) (bool, error) {
	panic("implement me!")
}

func (mho *mockHashOracle) CalcEligibility(context.Context, types.LayerID, int32, int, types.NodeID, []byte) (uint16, error) {
	panic("implement me!")
}

// eligible if a proof is valid for a given committee size
func (mho *mockHashOracle) eligible(ctx context.Context, layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (bool, error) {
	if sig == nil {
		log.Warning("Oracle query with proof=nil. Returning false")
		return false, errors.New("sig is nil")
	}

	// calculate hash of proof
	proofHash := mho.hasher.Hash(sig)
	if proofHash <= mho.calcThreshold(committeeSize) { // check threshold
		return true, nil
	}

	return false, nil
}

func (mho *mockHashOracle) Proof(context.Context, types.LayerID, int32) ([]byte, error) {
	return []byte{}, nil
}
