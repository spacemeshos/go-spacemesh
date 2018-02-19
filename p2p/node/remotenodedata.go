package node

import (
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"strings"
	"time"
)

// RemoteNodeData defines basic remote node data
// Outside of swarm local node works with RemoteNodeData and not with Peers
// Peers should only be used internally by swarm
type RemoteNodeData interface {
	ID() string    // base58 encoded node key/id
	IP() string    // node tcp listener e.g. 127.0.0.1:3038
	Bytes() []byte // node raw id bytes

	DhtID() dht.ID // dht id must be uniformly distributed for XOR distance to work, hence we hash the node key/id to create it

	GetLastFindNodeCall(nodeID string) time.Time // time of last find node call sent to node
	SetLastFindNodeCall(nodeID string, t time.Time)

	Pretty() string
}

// Outside of swarm - types only know about this and not about RemoteNode
type remoteNodeDataImpl struct {
	id    string // node Id is a base58 encoded bits of the node public key
	ip    string // node tcp address. e.g. 127.0.0.1:3030
	bytes []byte // bytes
	dhtID dht.ID

	lastFindNodeCall map[string]time.Time
}

// ToNodeInfo returns marshaled protobufs node infos slice from a slice of RemoteNodeData.
// filterId: node id to exclude from the result
func ToNodeInfo(nodes []RemoteNodeData, filterID string) []*pb.NodeInfo {
	// init empty slice
	res := []*pb.NodeInfo{}
	for _, n := range nodes {

		if n.ID() == filterID {
			continue
		}

		res = append(res, &pb.NodeInfo{
			NodeId:     n.Bytes(),
			TcpAddress: n.IP(),
		})
	}
	return res
}

// PickFindNodeServers picks up to count server who haven't been queried recently.
// nodeId - the target node id of this find node operation.
// Used in KAD node discovery.
func PickFindNodeServers(nodes []RemoteNodeData, nodeID string, count int) []RemoteNodeData {

	res := []RemoteNodeData{}
	added := 0

	for _, v := range nodes {
		if time.Now().Sub(v.GetLastFindNodeCall(nodeID)) > time.Duration(time.Minute*10) {
			res = append(res, v)
			added++

			if added == count {
				break
			}
		}
	}

	return res
}

// Union returns a union of 2 lists of nodes.
func Union(list1 []RemoteNodeData, list2 []RemoteNodeData) []RemoteNodeData {

	idSet := map[string]RemoteNodeData{}

	for _, n := range list1 {
		idSet[n.ID()] = n
	}
	for _, n := range list2 {
		if idSet[n.ID()] == nil {
			idSet[n.ID()] = n
		}
	}

	res := []RemoteNodeData{}

	for _, n := range idSet {
		res = append(res, n)
	}

	return res
}

// FromNodeInfos converts a list of NodeInfo to a list of RemoteNodeData.
func FromNodeInfos(nodes []*pb.NodeInfo) []RemoteNodeData {
	res := []RemoteNodeData{}
	for _, n := range nodes {
		id := base58.Encode(n.NodeId)
		node := NewRemoteNodeData(id, n.TcpAddress)
		res = append(res, node)
	}
	return res
}

// NewRemoteNodeData creates a new RemoteNodeData
func NewRemoteNodeData(id string, ip string) RemoteNodeData {
	bytes := base58.Decode(id)
	dhtID := dht.NewIDFromNodeKey(bytes)
	return &remoteNodeDataImpl{id: id,
		ip:               ip,
		bytes:            bytes,
		dhtID:            dhtID,
		lastFindNodeCall: map[string]time.Time{},
	}
}

// NewRemoteNodeDataFromString creates a remote node from a string in the following format: 126.0.0.1:3572/QmcjTLy94HGFo4JoYibudGeBV2DSBb6E4apBjFsBGnMsWa .
func NewRemoteNodeDataFromString(data string) RemoteNodeData {
	items := strings.Split(data, "/")
	if len(items) != 2 {
		return nil
	}
	return NewRemoteNodeData(items[1], items[0])
}

// GetLastFindNodeCall returns the time of the last find node call
func (rn *remoteNodeDataImpl) GetLastFindNodeCall(nodeID string) time.Time {
	return rn.lastFindNodeCall[nodeID]
}

// SetLastFindNodeCall updates the time of the last find node call
func (rn *remoteNodeDataImpl) SetLastFindNodeCall(nodeID string, t time.Time) {
	rn.lastFindNodeCall[nodeID] = t
}

func (rn *remoteNodeDataImpl) ID() string {
	return rn.id
}

func (rn *remoteNodeDataImpl) IP() string {
	return rn.ip
}

func (rn *remoteNodeDataImpl) Bytes() []byte {
	return rn.bytes
}

func (rn *remoteNodeDataImpl) DhtID() dht.ID {
	return rn.dhtID
}

func (rn *remoteNodeDataImpl) Pretty() string {
	return fmt.Sprintf("<RN Id: %s. Ip:%s %s>", rn.id[:8], rn.ip, rn.dhtID.Pretty())
}
