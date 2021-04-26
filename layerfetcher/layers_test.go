package layerfetcher

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

func RandomHash() types.Hash32 {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, 8)
	_, err := rand.Read(b)
	// Note that Err == nil only if we read len(b) bytes.
	if err != nil {
		return types.Hash32{}
	}
	return types.CalcHash32(b)
}

type mockNet struct {
	peers               []p2ppeers.Peer
	callCallback        bool
	callSuccessCallback bool
	errToSend           error
	sendCalled          int
}

func (m *mockNet) GetPeers() []p2ppeers.Peer {
	return m.peers
}

func (m *mockNet) GetRandomPeer() p2ppeers.Peer {
	return m.peers[0]
}

func (mockNet) Close() {

}

func (m *mockNet) SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), timeoutHandler func(err error)) error {
	m.sendCalled++
	if m.errToSend != nil {
		return m.errToSend
	}

	if !m.callCallback {
		return nil
	}

	/*if m.callSuccessCallback {
		return resHandler()
	}*/

	return nil
}

type layerDBMock struct {
	layers  map[types.Hash32][]types.BlockID
	vectors map[types.Hash32][]types.BlockID
	gossip  []types.BlockID
	hashes  map[types.LayerID]types.Hash32
}

func (l *layerDBMock) GetLayerInputVector(hash types.Hash32) ([]types.BlockID, error) {
	return l.vectors[hash], nil
}

func (l *layerDBMock) SaveLayerHashInputVector(id types.Hash32, data []byte) error {
	var blocks []types.BlockID
	err := types.BytesToInterface(data, blocks)
	if err != nil {
		return err
	}
	l.vectors[id] = blocks
	return nil
}

func newLayerDBMock() *layerDBMock {
	return &layerDBMock{
		layers:  make(map[types.Hash32][]types.BlockID),
		vectors: make(map[types.Hash32][]types.BlockID),
		gossip:  []types.BlockID{},
		hashes:  make(map[types.LayerID]types.Hash32),
	}
}

func (l *layerDBMock) GetLayerHash(ID types.LayerID) types.Hash32 {
	return l.hashes[ID]
}

func (l *layerDBMock) GetLayerHashBlocks(hash types.Hash32) []types.BlockID {
	return l.layers[hash]
}

func (l *layerDBMock) GetLayerVerifyingVector(hash types.Hash32) []types.BlockID {
	return l.vectors[hash]
}

func (l layerDBMock) Get() []types.BlockID {
	return l.gossip
}

type mockFetcher struct {
}

func (m mockFetcher) Stop() {
}

func (m mockFetcher) Start() {
}

func (m mockFetcher) AddDB(hint fetch.Hint, db database.Store) {

}

func (m mockFetcher) GetHash(hash types.Hash32, h fetch.Hint, validateAndSubmit bool) chan fetch.HashDataPromiseResult {
	return nil
}

func (m mockFetcher) GetHashes(hash []types.Hash32, hint fetch.Hint, validateAndSubmit bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
	return nil
}

type mockBlocks struct {
}

func (m mockBlocks) HandleBlockData(date []byte, fetcher service.Fetcher) error {
	panic("implement me")
}

type mockAtx struct {
}

func (m mockAtx) HandleAtxData(data []byte, syncer service.Fetcher) error {
	panic("implement me")
}

func NewMockLogic(net *mockNet, layers layerDB, blocksDB gossipBlocks, blocks blockHandler, atxs atxHandler, fetcher fetch.Fetcher, log log.Log) *Logic {
	var l = &Logic{
		log:                  log,
		fetcher:              fetcher,
		net:                  net,
		layerHashResults:     make(map[types.LayerID]map[p2ppeers.Peer]*types.Hash32),
		blockHashResults:     make(map[types.LayerID][]bool),
		layerResultsChannels: make(map[types.LayerID][]chan LayerPromiseResult),
		atxs:                 atxs,
		blockHandler:         blocks,
		layerDB:              layers,
		gossipBlocks:         blocksDB,
		layerResM:            sync.RWMutex{},
	}
	return l
}

func Test_LayerHashReceiver(t *testing.T) {
	db := newLayerDBMock()
	layerID := types.LayerID(1)
	l := NewMockLogic(&mockNet{}, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerHash"))
	h := RandomHash()
	db.hashes[layerID] = h
	l.LayerHashBlocksReceiver(layerID.Bytes())
}

func TestLogic_LayerHashBlocksReceiver(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(&mockNet{}, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerHash"))
	h := RandomHash()
	db.layers[h] = []types.BlockID{types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID()}

	outB := l.LayerHashBlocksReceiver(h.Bytes())
	var act []types.BlockID
	err := types.BytesToInterface(outB, &act)
	assert.NoError(t, err)
	assert.Equal(t, db.layers[h], act)
}

func Test_receiveLayerHash(t *testing.T) {
	db := newLayerDBMock()
	net := &mockNet{}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerHash"))
	numOfPeers := 4
	for i := 0; i < numOfPeers; i++ {
		net.peers = append(net.peers, p2pcrypto.NewRandomPubkey())
	}

	hashRes := RandomHash()
	// test happy flow - get 4 responses
	l.receiveLayerHash(1, net.peers[0], numOfPeers, hashRes.Bytes(), nil)
	assert.Equal(t, net.sendCalled, 0)

	// test aggregation by hash
	hashRes2 := RandomHash()
	for i := 1; i < numOfPeers; i++ {
		l.receiveLayerHash(1, net.peers[i], numOfPeers, hashRes2.Bytes(), nil)
	}

	assert.Equal(t, net.sendCalled, 2)

	//test error flow
	l.receiveLayerHash(1, net.peers[0], numOfPeers, hashRes.Bytes(), nil)

	for i := 1; i < numOfPeers; i++ {
		l.receiveLayerHash(1, net.peers[i], numOfPeers, nil, fmt.Errorf("error"))
	}
	// no additional sends should happen
	assert.Equal(t, net.sendCalled, 2)
}

func TestLogic_PollLayer(t *testing.T) {
	db := layerDBMock{}
	net := &mockNet{}
	l := NewMockLogic(net, &db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerHash"))
	numOfPeers := 4
	for i := 0; i < numOfPeers; i++ {
		net.peers = append(net.peers, p2pcrypto.NewRandomPubkey())
	}

	l.PollLayer(1)

	assert.Equal(t, numOfPeers, net.sendCalled)
}
