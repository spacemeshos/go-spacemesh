package sync

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

type Peer crypto.PublicKey

type Peers interface {
	p2p.Service
	Count() int
	GetLayerHash(peer int) string
	GetLayerBlockIDs(peer Peer, i int, hash string) ([]string, error)
	GetBlockByID(peer Peer, id string) (mesh.Block, error)
	GetPeers() []Peer
	LatestLayer() int
}
