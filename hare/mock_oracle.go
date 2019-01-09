package hare

import (
	"encoding/binary"
	"sync"
)

type Role byte

const (
	Passive = Role(0)
	Active  = Role(1)
	Leader  = Role(2)
)

type Rolacle interface {
	Role(r uint32, sig Signature) Role
}

type MockHashOracle struct {
	isLeaderTaken bool
}

func NewMockOracle() *MockHashOracle {
	mock := &MockHashOracle{}

	return mock
}

func (mockOracle *MockHashOracle) Role(r uint32, proof Signature) Role {
	if proof == nil {
		return Passive
	}

	data := binary.LittleEndian.Uint32(proof)

	if data < 10 {
		return Leader
	}

	if data < 10000 {
		return Active
	}

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
