package hare

import (
	"encoding/binary"
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

type MockOracle struct {
	roles         map[uint32]Role
	isLeaderTaken bool
}

func NewMockOracle() *MockOracle {
	mock := &MockOracle{}
	mock.roles = make(map[uint32]Role)

	return mock
}

func (mockOracle *MockOracle) Role(proof Signature) Role {
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