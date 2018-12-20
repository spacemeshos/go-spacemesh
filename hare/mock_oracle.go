package hare

import (
	"bytes"
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"hash/fnv"
)

const (
	Passive = 0
	Active  = 1
	Leader  = 2
)

type Rolacle interface {
	Role(rq RoleRequest) Signature
	ValidateRole(role byte, rq RoleRequest, sig Signature) bool
}

type RoleRequest struct {
	pubKey  crypto.PublicKey
	layerId LayerId
	k       uint32
}

func (roleRequest *RoleRequest) bytes() []byte {
	var binBuf bytes.Buffer
	binary.Write(&binBuf, binary.BigEndian, roleRequest)

	return binBuf.Bytes()
}

type MockOracle struct {
	roles map[uint32]byte
}

func NewMockOracle() *MockOracle {
	mock := &MockOracle{}
	mock.roles = make(map[uint32]byte)

	return mock
}

func (roleRequest *RoleRequest) Id() uint32 {
	h := fnv.New32()
	h.Write(roleRequest.bytes())
	return h.Sum32()
}

func (mockOracle *MockOracle) Role(rq RoleRequest) Signature {
	i := rq.Id()

	// check if exist
	if _, exist := mockOracle.roles[i]; exist {
		return Signature{}
	}

	mockOracle.roles[i] = roleFromIteration(rq.k)

	return Signature{}
}

func (mockOracle *MockOracle) ValidateRole(role byte, rq RoleRequest, sig Signature) bool {
	mockOracle.Role(rq)
	return mockOracle.roles[rq.Id()] == role && bytes.Equal(sig, Signature{})
}
