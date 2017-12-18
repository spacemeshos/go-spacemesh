package p2p

import (
	"github.com/UnrulyOS/go-unruly/p2p/dht/kbucket/keyspace"
	"github.com/btcsuite/btcutil/base58"
)

// Basic remote node data
// Outside of swarm local node works with RemoteNodeData and note with Peers
// Peers should only be used internally by swarm
type RemoteNodeData interface {
	Id() string		// base58 encoded node key/id
	Ip() string		// node tcp listener e.g. 127.0.0.1:3038
	Bytes() []byte	// node raw id bytes

	XorKey() keyspace.Key	// normalized key in the xor keyspace
	CommonPrefixLength(other RemoteNodeData) int
}

// Outside of swarm - types only know about this and not about RemoteNode
type remoteNodeDataImpl struct {
	id string // node Id is a base58 encoded bits of the node public key
	ip string // node tcp address. e.g. 127.0.0.1:3030
	bytes []byte // bytes
	xorkey keyspace.Key
}

func NewRemoteNodeData(id string, ip string) RemoteNodeData {
	bytes := base58.Decode(id)
	xorkey := keyspace.XORKeySpace.Key(bytes)
	return &remoteNodeDataImpl{id, ip, bytes,  xorkey}
}

func (rn *remoteNodeDataImpl) Id() string {
	return rn.id
}

func (rn *remoteNodeDataImpl) Ip() string {
	return rn.ip
}

func (rn *remoteNodeDataImpl) Bytes() []byte {
	return rn.bytes
}

func (rn *remoteNodeDataImpl) XorKey() keyspace.Key {
	return rn.xorkey
}

func (rn *remoteNodeDataImpl) CommonPrefixLength(other RemoteNodeData) int {
	return keyspace.ZeroPrefixLen(keyspace.XOR(rn.XorKey().Bytes, other.XorKey().Bytes))
}