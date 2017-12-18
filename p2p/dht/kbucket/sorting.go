package kbucket

import (
	//"container/list"
	"github.com/UnrulyOS/go-unruly/p2p"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"sort"
)

// A helper struct to sort peers by their distance to the local node
type peerDistance struct {
	node     p2p.RemoteNodeData
	distance dht.ID
}

// peerSorterArr implements sort.Interface to sort peers by xor distance
type peerSorterArr []*peerDistance

func (p peerSorterArr) Len() int      { return len(p) }
func (p peerSorterArr) Swap(a, b int) { p[a], p[b] = p[b], p[a] }
func (p peerSorterArr) Less(a, b int) bool {
	return p[a].distance.Less(p[b].distance)
}

/*
func copyPeersFromList(target ID, peerArr peerSorterArr, peerList *list.List) peerSorterArr {
	for e := peerList.Front(); e != nil; e = e.Next() {
		p := e.Value.(peer.ID)
		pID := ConvertPeerID(p)
		pd := peerDistance{
			p:        p,
			distance: xor(target, pID),
		}
		peerArr = append(peerArr, &pd)
	}
	return peerArr
}*/

func SortClosestPeers(peers []p2p.RemoteNodeData, target dht.ID) []p2p.RemoteNodeData {
	var psarr peerSorterArr
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
