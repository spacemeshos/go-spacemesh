// Package layerfetcher fetches layers from remote peers
package layerfetcher

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// atxHandler defines handling function for incoming ATXs
type atxHandler interface {
	HandleAtxData(data []byte, syncer service.Fetcher) error
}

// blockHandler defines handling function for blocks
type blockHandler interface {
	HandleBlockData(ctx context.Context, date []byte, fetcher service.Fetcher) error
}

// layerDB is an interface that returns layer data and blocks
type layerDB interface {
	GetLayerHash(ID types.LayerID) types.Hash32
	GetLayerHashBlocks(hash types.Hash32) []types.BlockID
	GetLayerVerifyingVector(hash types.Hash32) []types.BlockID
}

// gossipBlocks returns gossip block received byu this node in the last x time frame
type gossipBlocks interface {
	Get() []types.BlockID
}

// poetDb is an interface to reading and storing poet proofs
type poetDb interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	ValidateAndStoreMsg(data []byte) error
}

// network defines network capabilities used
type network interface {
	GetPeers() []p2ppeers.Peer
	GetRandomPeer() p2ppeers.Peer
	SendRequest(ctx context.Context, msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), timeoutHandler func(err error)) error
}

// Logic is the struct containing components needed to follow layer fetching logic
type Logic struct {
	log              log.Log
	fetcher          fetch.Fetcher
	net              network
	layerHashResults map[types.LayerID]map[p2ppeers.Peer]types.Hash32
	blockHashErrors  map[types.LayerID]int
	layerResults     map[types.LayerID][]chan LayerPromiseResult
	poetProofs       poetDb
	atxs             atxHandler
	blockHandler     blockHandler
	layerDB          layerDB
	gossipBlocks     gossipBlocks
	layerResM        sync.RWMutex
	layerHashResM    sync.RWMutex
	goldenATXID      types.ATXID
}

// Config defines configuration for layer fetching logic
type Config struct {
	RequestTimeout int
	GoldenATXID    types.ATXID
}

// DB hints per DB
const (
	BlockDB       = 1
	LayerBlocksDB = 2
	ATXDB         = 3
	TXDB          = 4
	POETDB        = 5
	LayerHashDB   = 6
	ATXIDsDB      = 7

	layersProtocol = "/layers/2.0/"
)

// FetchFlow is the main syncing flow TBD
func (l *Logic) FetchFlow() {

}

// LayerPromiseResult is the result of trying to fetch an entire layer- if this fails the error will be added to result
type LayerPromiseResult struct {
	err   error
	Layer types.LayerID
}

// LayerHashReqReceiver returns the layer hash for the given layer ID
func (l *Logic) LayerHashReqReceiver(msg []byte) []byte {
	lyr := types.LayerID(util.BytesToUint64(msg))
	return l.layerDB.GetLayerHash(lyr).Bytes()
}

// LayerHashBlocksReceiver returns the block IDs for the specified layer hash,
// it also returns the validation vector for this hash and latest blocks received in gossip
func (l *Logic) LayerHashBlocksReceiver(msg []byte) []byte {
	h := types.BytesToHash(msg)

	blocks := l.layerDB.GetLayerHashBlocks(h)
	vector := l.layerDB.GetLayerVerifyingVector(h)
	latest := l.gossipBlocks.Get()
	b := layerBlocks{
		blocks,
		latest,
		vector,
	}

	out, err := types.InterfaceToBytes(b)
	if err != nil {
		l.log.Error("cannot serialize response")
	}

	return out
}

// PollLayer performs the following
// send layer number to all parties
// receive layer hash from all peers
// get layer hash from corresponding peer
// fetch block ids from all peers
// fetch ATXs and Txs per block
func (l *Logic) PollLayer(ctx context.Context, layer types.LayerID) chan LayerPromiseResult {
	result := make(chan LayerPromiseResult)

	l.layerResM.Lock()
	l.layerResults[layer] = append(l.layerResults[layer], result)
	l.layerResM.Unlock()

	peers := l.net.GetPeers()
	// request layers from all peers since different peers can have different layer structures (in extreme cases)
	// we ask for all blocks so that we know
	for _, p := range peers {
		// build custom receiver for each peer so that receiver will know which peer the hash came from
		// so that it could request relevant block ids from the same peer
		receiveForPeerFunc := func(b []byte) {
			l.receiveLayerHash(ctx, layer, p, len(peers), b, nil)
		}

		timeoutFunc := func(err error) {
			l.receiveLayerHash(ctx, layer, p, len(peers), nil, err)
		}
		err := l.net.SendRequest(context.TODO(), LayerBlocksDB, layer.Bytes(), p, receiveForPeerFunc, timeoutFunc)
		if err != nil {
			l.receiveLayerHash(ctx, layer, p, len(peers), nil, err)
		}
	}
	return result
}

// receiver function for block hash result per layer, this function aggregates all responses from all peers
// and then unifies them. it also fails if a threshold of failed calls to peers have been reached
func (l *Logic) receiveLayerHash(ctx context.Context, id types.LayerID, p p2ppeers.Peer, peers int, data []byte, err error) {
	hash := types.Hash32{}
	// if no error from peer, try to parse data
	if err == nil {
		hash = types.BytesToHash(data)
	}

	// log result for peer
	l.layerHashResM.Lock()
	if _, ok := l.layerHashResults[id]; !ok {
		l.layerHashResults[id] = make(map[p2ppeers.Peer]types.Hash32)
	}
	l.layerHashResults[id][p] = hash

	// not enough results
	if len(l.layerHashResults[id]) < peers {
		l.layerHashResM.Unlock()
		return
	}
	l.layerHashResM.Unlock()
	h := types.Hash32{}
	errors := 0
	//aggregate hashes so that same hash will not be requested several times
	hashes := make(map[types.Hash32][]p2ppeers.Peer)
	l.layerHashResM.RLock()
	for peer, hash := range l.layerHashResults[id] {
		//count zero hashes - mark errors.
		if hash == h {
			errors++
		}
		if _, ok := hashes[hash]; !ok {
			hashes[hash] = append(hashes[hash], peer)
		}
	}
	l.layerHashResM.RUnlock()

	//delete the receiver since we got all the needed messages
	l.layerHashResM.Lock()
	delete(l.layerHashResults, id)
	l.layerHashResM.Unlock()

	// if more than half the peers returned an error, fail the sync of the entire layer
	// todo: think whether we should panic here
	if errors > peers/2 {
		l.notifyLayerPromiseResult(id, fmt.Errorf("too many peers returned error"))
		log.Error("cannot sync layer %v", id)
		return
	}

	// send a request to get blocks from a single peer if multiple peers declare same hash per layer
	// if the peers fails to respond request will be sen to next peer in line
	//todo: think if we should aggregate or ask from multiple peers to have some redundancy in requests
	for hash, peer := range hashes {
		//build receiver function
		receiveForPeerFunc := func(data []byte) {
			l.receiveBlockHashes(ctx, id, data, nil)
		}
		remainingPeers := 0
		errFunc := func(err error) {
			l.receiveBlockHashes(ctx, id, nil, err)
		}
		err := l.net.SendRequest(context.TODO(), LayerHashDB, hash.Bytes(), peer[remainingPeers], receiveForPeerFunc, errFunc)
		if err != nil {
			l.receiveBlockHashes(ctx, id, nil, err)
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
	l.layerResM.RUnlock()
}

// receiveBlockHashes is called when receiving block hashes for specified layer layer from remote peer
func (l *Logic) receiveBlockHashes(ctx context.Context, layer types.LayerID, data []byte, extErr error) {
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
	retErr := l.GetBlocks(ctx, blocks.Blocks)
	// if there is an error this means that the entire layer cannot be validated and therefore sync should fail
	if retErr != nil {
		l.notifyLayerPromiseResult(layer, retErr)
	}

	// here we need to update layer hash

	// we are done with no block iteration errors
	l.notifyLayerPromiseResult(layer, nil)
}

// GetEpochATXs fetches all atxs received by peer for given layer
func (l *Logic) GetEpochATXs(id types.EpochID) error {
	res := <-l.fetcher.GetHash(types.CalcHash32(id.ToBytes()), fetch.Hint(strconv.Itoa(ATXIDsDB)), true)
	return res.Err
}

// getAtxResults is called when an ATX result is received
func (l *Logic) getAtxResults(hash types.Hash32, data []byte) error {
	return l.atxs.HandleAtxData(data, l)
}

func (l *Logic) getTxResult(hash types.Hash32, data []byte) error {
	//TODO: this
	return nil
}

// getPoetResult is handler function to poet proof fetch result
func (l *Logic) getPoetResult(hash types.Hash32, data []byte) error {
	return l.poetProofs.ValidateAndStoreMsg(data)
}

// blockReceiveFunc handles blocks received via fetch
func (l *Logic) blockReceiveFunc(data []byte) error {
	return l.blockHandler.HandleBlockData(context.TODO(), data, l)
}

// IsSynced indocates if this node is synced
func (l *Logic) IsSynced(context.Context) bool {
	//todo: add this logic
	return true
}

// ListenToGossip indicates if node is currently accepting packets from gossip
func (l *Logic) ListenToGossip() bool {
	//todo: add this logic
	return true
}

// Future is a preparation for using actual futures in the code, this will allow to truly execute
// asynchronous reads and receive result only when needed
type Future struct {
	res chan fetch.HashDataPromiseResult
}

// Result actually evaluates the result of the fetch task
func (f *Future) Result() error {
	ret := <-f.res
	return ret.Err
}

// FetchAtx returns error if ATX was not found
func (l *Logic) FetchAtx(ctx context.Context, id types.ATXID) error {
	f := Future{l.fetcher.GetHash(id.Hash32(), fetch.Hint(strconv.Itoa(BlockDB)), true)}
	return f.Result()
}

// FetchBlock gets data for a single block id and validates it
func (l *Logic) FetchBlock(ctx context.Context, id types.BlockID) error {
	res := <-l.fetcher.GetHash(id.AsHash32(), fetch.Hint(strconv.Itoa(BlockDB)), true)
	return res.Err
}

// GetAtxs gets the data for given atx ids IDs and validates them. returns an error if at least one ATX cannot be fetched
func (l *Logic) GetAtxs(ctx context.Context, IDs []types.ATXID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.Hash32())
	}
	results := l.fetcher.GetHashes(hashes, fetch.Hint(strconv.Itoa(BlockDB)), true)
	for hash, resC := range results {
		res := <-resC
		err := l.getAtxResults(hash, res.Data)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetBlocks gets the data for given block ids and validates the blocks. returns an error if a single atx failed to be fetched
// or validated
func (l *Logic) GetBlocks(ctx context.Context, IDs []types.BlockID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.AsHash32())
	}
	results := l.fetcher.GetHashes(hashes, fetch.Hint(strconv.Itoa(BlockDB)), true)
	for _, resC := range results {
		res := <-resC
		err := l.blockReceiveFunc(res.Data)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetTxs fetches the txs provided as IDs and validates them, returns an error if one TX failed to be fetched
func (l *Logic) GetTxs(ctx context.Context, IDs []types.TransactionID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.Hash32())
	}
	results := l.fetcher.GetHashes(hashes, fetch.Hint(strconv.Itoa(TXDB)), true)
	for hash, resC := range results {
		res := <-resC
		err := l.getTxResult(hash, res.Data)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetPoetProof gets poet proof from remote peer
func (l *Logic) GetPoetProof(ctx context.Context, id types.Hash32) error {
	res := <-l.fetcher.GetHash(id, fetch.Hint(strconv.Itoa(POETDB)), true)
	if res.Err != nil {
		return res.Err
	}
	return l.getPoetResult(res.Hash, res.Data)
}
