package node

import (
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"github.com/UnrulyOS/go-unruly/p2p/pb"
	"github.com/btcsuite/btcutil/base58"
	"time"
)

// Basic remote node data
// Outside of swarm local node works with RemoteNodeData and note with Peers
// Peers should only be used internally by swarm
type RemoteNodeData interface {
	Id() string    // base58 encoded node key/id
	Ip() string    // node tcp listener e.g. 127.0.0.1:3038
	Bytes() []byte // node raw id bytes

	DhtId() dht.ID

	GetLastFindNodeCall(nodeId string) time.Time // time of last find node call sent to node
	SetLastFindNodeCall(nodeId string, t time.Time)
}

// Outside of swarm - types only know about this and not about RemoteNode
type remoteNodeDataImpl struct {
	id    string // node Id is a base58 encoded bits of the node public key
	ip    string // node tcp address. e.g. 127.0.0.1:3030
	bytes []byte // bytes
	dhtId dht.ID

	lastFindNodeCall map[string]time.Time
}

// Return serializable (pb) node infos slice from a slice of RemoteNodeData
// filterId: node id to exclude from the result
func ToNodeInfo(nodes []RemoteNodeData, filterId string) []*pb.NodeInfo {
	// init empty slice
	res := []*pb.NodeInfo{}
	for _, n := range nodes {

		if n.Id() == filterId {
			continue
		}

		res = append(res, &pb.NodeInfo{
			NodeId:     n.Bytes(),
			TcpAddress: n.Ip(),
		})
	}
	return res
}

// pick up to count server who haven't been queried to find a node recently
// nodeId - the target node id of this find node operation
func PickFindNodeServers(nodes []RemoteNodeData, nodeId string, count int) []RemoteNodeData {

	res := []RemoteNodeData{}
	added := 0

	for _, v := range nodes {
		if time.Now().Sub(v.GetLastFindNodeCall(nodeId)) > time.Duration(time.Minute*10) {
			res = append(res, v)
			added += 1

			if added == count {
				break
			}
		}
	}

	return res
}

// return a union of 2 lists of nods
func Union(list1 []RemoteNodeData, list2 []RemoteNodeData) []RemoteNodeData {

	idSet := map[string]RemoteNodeData{}

	for _, n := range list1 {
		idSet[n.Id()] = n
	}
	for _, n := range list2 {
		if idSet[n.Id()] == nil {
			idSet[n.Id()] = n
		}
	}

	res := []RemoteNodeData{}

	for _, n := range idSet {
		res = append(res, n)
	}

	return res
}

func FromNodeInfos(nodes []*pb.NodeInfo) []RemoteNodeData {
	res := []RemoteNodeData{}
	for _, n := range nodes {
		id := base58.Encode(n.NodeId)
		node := NewRemoteNodeData(id, n.TcpAddress)
		res = append(res, node)
	}
	return res
}

func NewRemoteNodeData(id string, ip string) RemoteNodeData {
	bytes := base58.Decode(id)
	dhtId := dht.NewIdFromNodeKey(bytes)
	return &remoteNodeDataImpl{id: id,
		ip:               ip,
		bytes:            bytes,
		dhtId:            dhtId,
		lastFindNodeCall: map[string]time.Time{},
	}
}

func (rn *remoteNodeDataImpl) GetLastFindNodeCall(nodeId string) time.Time {
	return rn.lastFindNodeCall[nodeId]
}

func (rn *remoteNodeDataImpl) SetLastFindNodeCall(nodeId string, t time.Time) {
	rn.lastFindNodeCall[nodeId] = t
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
