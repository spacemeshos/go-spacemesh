package hare

import (
	"bytes"
	"encoding/binary"
	"hash/fnv"
	"math"
)

const (
	Passive = 0
	Active = 1
	Leader = 2
)

type Rolacle interface {
	Role(rq RoleRequest) Signature
	ValidateRole(role byte, rq RoleRequest, sig Signature) bool
}

type RoleRequest struct {
	pubKey PubKey
	layerId LayerId
	k uint32
}

func (roleRequest *RoleRequest) bytes() []byte {
	var binBuf bytes.Buffer
	binary.Write(&binBuf, binary.BigEndian, roleRequest)

	return binBuf.Bytes()
}

type MockOracle struct {
	roles map[uint32]byte
	isLeaderTaken bool
}

func NewMockOracle() *MockOracle {
	mock := &MockOracle{}
	mock.roles = make(map[uint32]byte)
	mock.isLeaderTaken = false

	return mock
}

func (roleRequest *RoleRequest) Id() uint32 {
	h := fnv.New32()
	h.Write(roleRequest.bytes())
	return h.Sum32()
}

func (mockOracle *MockOracle) Role(rq RoleRequest) Signature {
	i := rq.Id()

	if !mockOracle.isLeaderTaken {
		mockOracle.roles[i] = Leader
		mockOracle.isLeaderTaken = true
		return Signature{}
	}

	// check if exist
	if _, exist := mockOracle.roles[i]; exist {
		return Signature{}
	}

	if i < math.MaxUint32 / 2 {
		mockOracle.roles[i] = Active
	} else {
		mockOracle.roles[i] = Passive
	}

	return Signature{}
}

func (mockOracle *MockOracle) ValidateRole(role byte, rq RoleRequest, sig Signature) bool {
	return mockOracle.roles[rq.Id()] == role && sig == nil
}
