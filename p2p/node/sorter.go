package node

import (
	"container/list"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"sort"
)

type PeerDistance struct {
	Node     RemoteNodeData
	Distance dht.ID
}

// PeerSorter is a sort.Interface of RemoteNodeData by XOR distance
type PeerSorter []*PeerDistance

func (p PeerSorter) Len() int      { return len(p) }
func (p PeerSorter) Swap(a, b int) { p[a], p[b] = p[b], p[a] }
func (p PeerSorter) Less(a, b int) bool {
	return p[a].Distance.Less(p[b].Distance)
}

func CopyPeersFromList(target dht.ID, dest PeerSorter, src *list.List) PeerSorter {
	for e := src.Front(); e != nil; e = e.Next() {
		p := e.Value.(RemoteNodeData)
		pd := PeerDistance{
			Node:     p,
			Distance: p.DhtId().Xor(target),
		}
		dest = append(dest, &pd)
	}
	return dest
}

func SortClosestPeers(peers []RemoteNodeData, target dht.ID) []RemoteNodeData {
	var psarr PeerSorter
	for _, p := range peers {
		pd := &PeerDistance{
			Node:     p,
			Distance: p.DhtId().Xor(target),
		}
		psarr = append(psarr, pd)
	}
	sort.Sort(psarr)
	var out []RemoteNodeData
	for _, p := range psarr {
		out = append(out, p.Node)
	}

	return out
}
