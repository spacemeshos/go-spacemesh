package hare

import (
	"bytes"
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"hash/fnv"
	"math/rand"
)

type Role byte

const (
	Passive = Role(0)
	Active  = Role(1)
	Leader  = Role(2)
)

type Rolacle interface {
	Role(rq RoleRequest) RolacleResponse
	ValidateRole(rq RoleRequest, res RolacleResponse) bool
}

type RolacleResponse struct {
	Role
	Signature
}

type RoleRequest struct {
	pubKey     crypto.PublicKey
	instanceId InstanceId
	k          int32
}

func (roleRequest *RoleRequest) bytes() []byte {
	var binBuf bytes.Buffer
	binary.Write(&binBuf, binary.BigEndian, roleRequest)

	return binBuf.Bytes()
}

func (roleRequest *RoleRequest) Id() uint32 {
	h := fnv.New32()
	h.Write(roleRequest.bytes())
	return h.Sum32()
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

func (mockOracle *MockOracle) Role(rq RoleRequest) RolacleResponse {
	i := rq.Id()

	// check if exist
	if role, exist := mockOracle.roles[i]; exist {
		return RolacleResponse{role, Signature{}}
	}

	// set role
	if rq.k%4 == Round2 && !mockOracle.isLeaderTaken { // only one leader
		mockOracle.roles[i] = Leader
	} else {
		if rand.Int()%2 == 0 {
			mockOracle.roles[i] = Active
		} else {
			mockOracle.roles[i] = Passive
		}
	}

	return RolacleResponse{mockOracle.roles[i], Signature{}}
}

func (mockOracle *MockOracle) ValidateRole(request RoleRequest, proof RolacleResponse) bool {
	response := mockOracle.Role(request)
	return response.Role == proof.Role && bytes.Equal(proof.Signature, response.Signature)
}
