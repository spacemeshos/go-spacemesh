// Package layerfetcher fetches layers from remote peers
package layerfetcher

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"sync"
)

type network interface {
	GetPeers() []p2ppeers.Peer
	SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), errHandler func(err error)) error
}

type AtxValidator interface {
	SyntacticallyValidateAtx(atx *types.ActivationTx) error
}

type BlockValidator interface {
	fastValidation(block *types.Block) error
	validateVotes(block *types.Block) error
}

type Logic struct {
	log              log.Log
	fetcher          fetch.Fetcher
	mesh             mesh.Mesh
	net              network
	layerHashResults map[types.LayerID]map[p2ppeers.Peer]types.Hash32
	blockHashErrors  map[types.LayerID]int
	layerResults     map[types.LayerID][]chan LayerPromiseResult
	atxValidator     AtxValidator
	blockValidator   BlockValidator
	layerResM        sync.RWMutex
}

const (
	BlockDB       = 1
	LayerBlocksDB = 2
	ATXDB         = 3
	TXDB          = 3
)

func (l *Logic) FetchFlow() {

}

type LayerPromiseResult struct {
	err   error
	Layer types.LayerID
}

// send layer number to all parties
// receive layer hash from all peers
// get layer hash from corresponding peer
// fetch block ids from all peers
// fetch ATXs and Txs per block
func (l *Logic) PollLayer(layer types.LayerID) chan LayerPromiseResult {
	result := make(chan LayerPromiseResult)

	l.layerResM.Lock()
	l.layerResults[layer] = append(l.layerResults[layer], result)
	l.layerResM.Unlock()

	peers := l.net.GetPeers()
	for _, p := range peers {
		// build custom receiver for each peer so that receiver will know which peer the hash came from
		// so that it could request relevant block ids from the same peer
		receiveForPeerFunc := func(b []byte) {
			l.receiveLayerHash(layer, p, len(peers), b, nil)
		}

		timeoutFunc := func(err error) {
			l.receiveLayerHash(layer, p, len(peers), nil, err)
		}
		err := l.net.SendRequest(LayerBlocksDB, layer.Bytes(), p, receiveForPeerFunc, timeoutFunc)
		if err != nil {
			l.receiveLayerHash(layer, p, len(peers), nil, err)
		}
	}
	return result
}

func (l *Logic) receiveLayerHash(id types.LayerID, p p2ppeers.Peer, peers int, data []byte, err error) {
	hash := types.Hash32{}
	// if no error from peer, try to parse data
	if err == nil {
		hash = types.BytesToHash(data)
	}

	// log result for peer
	if _, ok := l.layerHashResults[id]; !ok {
		l.layerHashResults[id] = make(map[p2ppeers.Peer]types.Hash32)
	}
	l.layerHashResults[id][p] = hash
	// not enough results
	if len(l.layerHashResults[id]) < peers {
		return
	}
	h := types.Hash32{}
	errors := 0
	//aggregate hashes so that same hash will not be requested several times
	hashes := make(map[types.Hash32][]p2ppeers.Peer)
	for peer, hash := range l.layerHashResults[id] {
		//count zero hashes - mark errors.
		if hash == h {
			errors++
		}
		if _, ok := hashes[hash]; !ok {
			hashes[hash] = append(hashes[hash], peer)
		}
	}

	//delete the receiver since we got all the needed messages
	delete(l.layerHashResults, id)

	// if more than half the peers returned an error, fail the sync of the entire layer
	// todo: think whether we should panic here
	if errors > peers/2 {
		l.notifyLayerPromiseResult(id, fmt.Errorf("too many peers returned error"))
		log.Error("cannot sync layer %v", id)
		return
	}

	//todo: think if we should aggregate or ask from multiple peers to have some redundancy in requests
	for hash, peer := range hashes {
		//build receiver function
		receiveForPeerFunc := func(data []byte) {
			l.receiveBlockHashes(id, data, nil)
		}
		remainingPeers := 0
		errFunc := func(err error) {
			l.receiveBlockHashes(id, nil, err)
		}
		err := l.net.SendRequest(LayerBlocksDB, hash.Bytes(), peer[remainingPeers], receiveForPeerFunc, errFunc)
		if err != nil {
			l.receiveBlockHashes(id, nil, err)
		}
	}

}

// notifyLayerPromiseResult notifies that a layer result has been received or wasn't received
func (l *Logic) notifyLayerPromiseResult(id types.LayerID, err error) {
	res := LayerPromiseResult{
		err,
		id,
	}
	l.layerResM.RLock()
	for _, ch := range l.layerResults[id] {
		ch <- res
	}
	l.layerResM.Unlock()
}

// receiveBlockHashes is called when receiving block hashes for specified layer layer from remote peer
func (l *Logic) receiveBlockHashes(layer types.LayerID, data []byte, extErr error) {
	//if we failed getting layer data - notify
	if extErr != nil {
		l.notifyLayerPromiseResult(layer, extErr)
		return
	}

	var blocks layerBlocks
	err := types.BytesToInterface(data, &blocks)
	if err != nil {
		l.notifyLayerPromiseResult(layer, err)
		return
	}

	// fetch all blocks
	retErr := l.GetBlocks(blocks.Blocks)
	// if there is an error this means that the entire layer cannot be validated and therefore sync should fail
	if retErr != nil {
		l.notifyLayerPromiseResult(layer, retErr)
	}

	// here we neeed to update layer hash

	// we are done with no block iteration errors
	l.notifyLayerPromiseResult(layer, nil)
}

// GetAtxResults is called when an ATX result is received
func (l *Logic) GetAtxResults(hash types.Hash32, data []byte) error {
	var atx types.ActivationTx
	err := types.BytesToInterface(data, &atx)
	if err != nil {
		return err
	}

	return l.atxValidator.SyntacticallyValidateAtx(&atx)
}

func (l *Logic) GetTxResult(hash types.Hash32, data []byte) error {
	//TODO: this
	return nil
}

// GetBlock gets data for a single block id and validates it
func (l *Logic) GetBlock(id types.BlockID) error {
	res := <-l.fetcher.GetHash(id.AsHash32(), fetch.Hint(BlockDB), l.BlockReceiveFunc, true)
	return res.Err
}

// GetAtxs gets the data for given atx ids IDs and validates them. returns an error if at least one ATX cannot be fetched
func (l *Logic) GetAtxs(IDs []types.ATXID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.Hash32())
	}
	return l.fetcher.GetAllHashes(hashes, fetch.Hint(ATXDB), l.GetAtxResults, true)
}

// GetBlocks gets the data for given block ids and validates the blocks. returns an error if a single atx failed to be fetched
// or validated
func (l *Logic) GetBlocks(IDs []types.BlockID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.AsHash32())
	}
	return l.fetcher.GetAllHashes(hashes, fetch.Hint(BlockDB), l.BlockReceiveFunc, true)
}

// GetTxs fetches the txs provided as IDs and validates them, returns an error if one TX failed to be fetched
func (l *Logic) GetTxs(IDs []types.TransactionID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.Hash32())
	}
	return l.fetcher.GetAllHashes(hashes, fetch.Hint(TXDB), l.GetTxResult, true)
}
