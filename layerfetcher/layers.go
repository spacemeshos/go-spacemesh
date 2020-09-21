// Package layerfetcher fetches layers from remote peers
package layerfetcher

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

type network interface {
	GetPeers() []p2ppeers.Peer
	SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), errHandler func(err error)) error
}

type Logic struct {
	fetcher fetch.Fetcher
	mesh    mesh.Mesh
	net     network
}

const (
	BlockDB       = "blocks"
	LayerBlocksDB = "layers"
	ATXDB         = "atxs"
)

func (p *Logic) FetchLayer(layer types.LayerID) {
	layerHash := types.CalcHash32(layer.Bytes())
	// Send layer ID hash with db as hint
	p.fetcher.GetHash(layerHash, LayerBlocksDB, p.ProcessLayerIDResults, p.HandleLayerNotFounct)
}

func (p *Logic) ProcessLayerIDResults(layerIDHash types.Hash32, data []byte) {
	var blockIDs []types.BlockID
	p.mesh.LayerBlockIds(layerIDHash)
}

func (p *Logic) HandleLayerNotFounct(layerIDHash types.Hash32, err error) {

}

func (p *Logic) HandleLayerRequest() {
	// return hash of all blocks
}

func (p *Logic) H(){
	
}

func (l *Logic) PollHash(hash types.Hash32, h Hint, dataCallback HandleDataCallback, failCallback HandleFailCallback) {
	peers := l.net.GetPeers()
	for _, p := range peers {
		err := l.net.SendRequest(fetch, bytes, p, f.receiveResponse, timeoutFunc)
	}
}

func (l *Logic) receivePollResponse(data []byte) {
	var response responseMessage
	err := types.BytesToInterface(data, &response)
	if err != nil {
		log.Error("we shold panic here, response was unclear, probably leaking")
	}

}
