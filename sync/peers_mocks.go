package sync

import "github.com/spacemeshos/go-spacemesh/p2p"

type PeersMocks struct {
	p2p.Service
}

func (ml PeersMocks) Count() int {
	return 10
}

func (ml PeersMocks) LatestLayer() int {
	return 10
}

func (ml PeersMocks) GetLayerHash(peer int) string {
	return "asiodfu45987345" //some random string
}

func (ml PeersMocks) GetBlockByID(peer Peer, id string) (Block, error) {
	return nil, nil
}

func (ml PeersMocks) ChoosePeers(pNum int) []Peer {
	return nil
}

func (ml PeersMocks) GetLayerBlockIDs(peers Peer, i int, hash string) ([]string, error) {

	return nil, nil
}

func (ml PeersMocks) GetPeers() []Peer {

	return nil
}
