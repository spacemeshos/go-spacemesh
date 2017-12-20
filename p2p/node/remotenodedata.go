package node

import (
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"github.com/UnrulyOS/go-unruly/p2p/pb"
	"github.com/btcsuite/btcutil/base58"
)

// Basic remote node data
// Outside of swarm local node works with RemoteNodeData and note with Peers
// Peers should only be used internally by swarm
type RemoteNodeData interface {
	Id() string    // base58 encoded node key/id
	Ip() string    // node tcp listener e.g. 127.0.0.1:3038
	Bytes() []byte // node raw id bytes

	DhtId() dht.ID
	//CommonPrefixLength(other RemoteNodeData) int
}

// Outside of swarm - types only know about this and not about RemoteNode
type remoteNodeDataImpl struct {
	id    string // node Id is a base58 encoded bits of the node public key
	ip    string // node tcp address. e.g. 127.0.0.1:3030
	bytes []byte // bytes
	dhtId dht.ID
}

// Return serializable (pb) node infos slice from a slice of RemoteNodeData
func ToNodeInfo(nodes []RemoteNodeData) []*pb.NodeInfo {
	// init empty slice
	res := []*pb.NodeInfo{}
	for _, n := range nodes {
		res = append(res, &pb.NodeInfo{
			NodeId:     n.Bytes(),
			TcpAddress: n.Ip(),
		})
	}
	return res
}

func NewRemoteNodeData(id string, ip string) RemoteNodeData {
	bytes := base58.Decode(id)
	dhtId := dht.NewIdFromNodeKey(bytes)
	return &remoteNodeDataImpl{id, ip, bytes, dhtId}
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

func (rn *remoteNodeDataImpl) DhtId() dht.ID {
	return rn.dhtId
}
