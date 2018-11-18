package sync

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

type Peer crypto.PublicKey

type Peers interface {
	p2p.Service
	GetPeers() []Peer
}

type PeersImpl struct {
	p2p.Service
}

func NewPeers(p p2p.Service) Peers {
	return &PeersImpl{p}
}

func (pi PeersImpl) GetPeers() []Peer {
	return nil
}
