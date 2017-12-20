package table

import (
	"container/list"
	"github.com/UnrulyOS/go-unruly/p2p"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"sort"
)

type peerDistance struct {
	node     p2p.RemoteNodeData
	distance dht.ID
}

// peerSorter is a sort.Interface of RemoteNodeData by distance
type peerSorter []*peerDistance

func (p peerSorter) Len() int      { return len(p) }
func (p peerSorter) Swap(a, b int) { p[a], p[b] = p[b], p[a] }
func (p peerSorter) Less(a, b int) bool {
	return p[a].distance.Less(p[b].distance)
}

func copyPeersFromList(target dht.ID, dest peerSorter, src *list.List) peerSorter {
	for e := src.Front(); e != nil; e = e.Next() {
		p := e.Value.(p2p.RemoteNodeData)
		pd := peerDistance{
			node:     p,
			distance: p.DhtId().Xor(target),
		}
		dest = append(dest, &pd)
	}
	return dest
}

func SortClosestPeers(peers []p2p.RemoteNodeData, target dht.ID) []p2p.RemoteNodeData {
	var psarr peerSorter
	for _, p := range peers {
		pd := &peerDistance{
			node:     p,
			distance: p.DhtId().Xor(target),
		}
		psarr = append(psarr, pd)
	}
	sort.Sort(psarr)
	var out []p2p.RemoteNodeData
	for _, p := range psarr {
		out = append(out, p.node)
	}

	return out
}
