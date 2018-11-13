package hare

import (
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"hash/fnv"
	"time"
)

const ProtoName = "HARE_PROTOCOL"
const RoundDuration = time.Second * time.Duration(15)
const (
	Status   = 0 // round 0
	Proposal = 1 // round 1
	Commit   = 2 // round 2
	Notify   = 3 // round 3
)

type BlockId uint32 // TODO: replace with import
type LayerId [32]byte // TODO: replace with import

type Byteable interface {
	Bytes() []byte
}

type NetworkService interface {
	RegisterProtocol(protocol string) chan service.Message
	Broadcast(protocol string, payload []byte) error
}

type Identifiable interface {
	Id() uint32
}

func (layerId *LayerId) Id() uint32 {
	h := fnv.New32()
	h.Write(layerId[:])
	return h.Sum32()
}

func (layerId *LayerId) Bytes() []byte {
	return layerId[:]
}