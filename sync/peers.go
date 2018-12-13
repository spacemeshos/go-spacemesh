package sync

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

type Peer crypto.PublicKey

type Peers interface {
	server.ServerService
	GetPeers() []Peer
}

type PeersImpl struct {
	server.ServerService
	getPeers func() []Peer
}

func NewPeers(p server.ServerService) Peers {
	return &PeersImpl{p, nil}
}

func (pi PeersImpl) GetPeers() []Peer {
	return pi.getPeers()
}
