package layerfetcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
)

func randomHash() types.Hash32 {
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
	peers       []peers.Peer
	layerHashes map[peers.Peer][]byte
	layerBlocks map[peers.Peer][]byte
	errors      map[peers.Peer]error
	timeouts    map[peers.Peer]struct{}
}

func newMockNet() *mockNet {
	return &mockNet{
		layerHashes: make(map[peers.Peer][]byte),
		layerBlocks: make(map[peers.Peer][]byte),
		errors:      make(map[peers.Peer]error),
		timeouts:    make(map[peers.Peer]struct{}),
	}
}
func (m *mockNet) GetPeers() []peers.Peer    { return m.peers }
func (m *mockNet) GetRandomPeer() peers.Peer { return m.peers[0] }
func (m *mockNet) SendRequest(_ context.Context, msgType server.MessageType, _ []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), timeoutHandler func(err error)) error {
	if _, ok := m.timeouts[address]; ok {
		timeoutHandler(errors.New("peer timeout"))
		return nil
	}
	switch msgType {
	case server.LayerHashMsg:
		if data, ok := m.layerHashes[address]; ok {
			resHandler(data)
			return nil
		}
	case server.LayerBlocksMsg:
		if data, ok := m.layerBlocks[address]; ok {
			resHandler(data)
			return nil
		}
	}
	return m.errors[address]
}
func (mockNet) Close() {}

type layerDBMock struct {
	layers  map[types.Hash32][]types.BlockID
	vectors map[types.Hash32][]types.BlockID
	gossip  []types.BlockID
	hashes  map[types.LayerID]types.Hash32
}

func newLayerDBMock() *layerDBMock {
	return &layerDBMock{
		layers:  make(map[types.Hash32][]types.BlockID),
		vectors: make(map[types.Hash32][]types.BlockID),
		gossip:  []types.BlockID{},
		hashes:  make(map[types.LayerID]types.Hash32),
	}
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
func (l *layerDBMock) GetLayerHash(ID types.LayerID) types.Hash32           { return l.hashes[ID] }
func (l *layerDBMock) GetLayerHashBlocks(hash types.Hash32) []types.BlockID { return l.layers[hash] }
func (l layerDBMock) Get() []types.BlockID                                  { return l.gossip }

type mockFetcher struct{}

func (m mockFetcher) Stop()                                {}
func (m mockFetcher) Start()                               {}
func (m mockFetcher) AddDB(_ fetch.Hint, _ database.Store) {}
func (m mockFetcher) GetHash(_ types.Hash32, _ fetch.Hint, _ bool) chan fetch.HashDataPromiseResult {
	ch := make(chan fetch.HashDataPromiseResult, 1)
	ch <- fetch.HashDataPromiseResult{
		IsLocal: true,
	}
	return ch
}

func (m mockFetcher) GetHashes(_ []types.Hash32, _ fetch.Hint, _ bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
	return nil
}

type mockBlocks struct{}

func (m mockBlocks) HandleBlockData(_ context.Context, _ []byte, _ service.Fetcher) error {
	panic("implement me")
}

type mockAtx struct{}

func (m mockAtx) HandleAtxData(_ context.Context, _ []byte, _ service.Fetcher) error {
	panic("implement me")
}

func NewMockLogic(net *mockNet, layers layerDB, blocksDB gossipBlocks, blocks blockHandler, atxs atxHandler, fetcher fetch.Fetcher, log log.Log) *Logic {
	l := &Logic{
		log:            log,
		fetcher:        fetcher,
		net:            net,
		layerHashesRes: make(map[types.LayerID]map[peers.Peer]*peerResult),
		layerHashesChs: make(map[types.LayerID][]chan LayerHashResult),
		layerBlocksRes: make(map[types.LayerID]map[peers.Peer]*peerResult),
		layerBlocksChs: make(map[types.LayerID][]chan LayerPromiseResult),
		atxs:           atxs,
		blockHandler:   blocks,
		layerDB:        layers,
		gossipBlocks:   blocksDB,
	}
	return l
}

func TestLayerHashReqReceiver(t *testing.T) {
	db := newLayerDBMock()
	layerID := types.NewLayerID(1)
	l := NewMockLogic(newMockNet(), db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))
	h := randomHash()
	db.hashes[layerID] = h
	out := l.layerHashReqReceiver(context.TODO(), layerID.Bytes())
	assert.Equal(t, h, types.BytesToHash(out))
}

func TestLayerHashBlocksReqReceiver(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(newMockNet(), db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))
	h := randomHash()
	db.layers[h] = []types.BlockID{types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID()}
	db.vectors[h] = []types.BlockID{types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID()}

	outB := l.layerHashBlocksReqReceiver(context.TODO(), h.Bytes())

	var act layerBlocks
	err := types.BytesToInterface(outB, &act)
	assert.NoError(t, err)
	assert.Equal(t, act.Blocks, db.layers[h])
	assert.Equal(t, act.VerifyingVector, db.vectors[h])
	assert.Nil(t, act.LatestBlocks)
}

func TestPollLayerHash_NoPeers(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(newMockNet(), db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))
	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Equal(t, ErrNoPeers, res.Err)
	assert.Nil(t, res.Hashes)
}

func TestPollLayerHash_TooManyErrors(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i == 0 {
			net.layerHashes[peer] = randomHash().Bytes()
		} else {
			net.errors[peer] = errors.New("SendRequest error")
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Equal(t, ErrTooManyPeerErrors, res.Err)
	assert.Nil(t, res.Hashes)
}

func TestPollLayerHash_TooManyErrors_Timeout(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i == 0 {
			net.layerHashes[peer] = randomHash().Bytes()
		} else {
			net.timeouts[peer] = struct{}{}
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Equal(t, ErrTooManyPeerErrors, res.Err)
	assert.Nil(t, res.Hashes)
}

func TestPollLayerHash_SomeError(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	hash := randomHash()
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i%2 == 0 {
			net.errors[peer] = errors.New("SendRequest error")
		} else {
			net.layerHashes[peer] = hash.Bytes()
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, 1, len(res.Hashes))
	assert.ElementsMatch(t, []peers.Peer{net.peers[1], net.peers[3]}, res.Hashes[hash])
}

func TestPollLayerHash_SomeTimeout(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	hash := randomHash()
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i%2 == 0 {
			net.timeouts[peer] = struct{}{}
		} else {
			net.layerHashes[peer] = hash.Bytes()
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, 1, len(res.Hashes))
	assert.ElementsMatch(t, []peers.Peer{net.peers[1], net.peers[3]}, res.Hashes[hash])
}

func TestPollLayerHash_Aggregated(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	evenHash := randomHash()
	oddHash := randomHash()
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i%2 == 0 {
			net.layerHashes[peer] = evenHash.Bytes()
		} else {
			net.layerHashes[peer] = oddHash.Bytes()
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, numPeers/2, len(res.Hashes))
	assert.ElementsMatch(t, []peers.Peer{net.peers[0], net.peers[2]}, res.Hashes[evenHash])
	assert.ElementsMatch(t, []peers.Peer{net.peers[1], net.peers[3]}, res.Hashes[oddHash])
}

func TestPollLayerHash_AllDifferent(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		net.layerHashes[peer] = randomHash().Bytes()
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, numPeers, len(res.Hashes))
}

func generateLayerBlocks() []byte {
	lb := layerBlocks{
		Blocks:          []types.BlockID{types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID()},
		VerifyingVector: []types.BlockID{types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID()},
	}
	out, _ := types.InterfaceToBytes(lb)
	return out
}

func TestPollLayerBlocks_AllHaveBlockData(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = generateLayerBlocks()
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	// currently first peer for each hash is queried
	layerHashes := map[types.Hash32][]peers.Peer{
		randomHash(): {net.peers[2]},
		randomHash(): {net.peers[1], net.peers[0]},
		randomHash(): {net.peers[3]},
	}

	res := <-l.PollLayerBlocks(context.TODO(), layerID, layerHashes)
	assert.Nil(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_OnlyOneHasBlockData(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i == 2 {
			net.layerBlocks[peer] = generateLayerBlocks()
		} else {
			net.errors[peer] = errors.New("SendRequest error")
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	// currently first peer for each hash is queried
	layerHashes := map[types.Hash32][]peers.Peer{
		randomHash(): {net.peers[2]},
		randomHash(): {net.peers[1], net.peers[0]},
		randomHash(): {net.peers[3]},
	}

	res := <-l.PollLayerBlocks(context.TODO(), layerID, layerHashes)
	assert.Nil(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_OneZeroLayerAmongstErrors(t *testing.T) {
	types.SetLayersPerEpoch(5)

	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		net.errors[peer] = errors.New("SendRequest error")
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	// currently first peer for each hash is queried
	layerHashes := map[types.Hash32][]peers.Peer{
		emptyHash:    {net.peers[2]},
		randomHash(): {net.peers[1], net.peers[0]},
		randomHash(): {net.peers[3]},
	}

	res := <-l.PollLayerBlocks(context.TODO(), layerID, layerHashes)
	assert.Equal(t, ErrZeroLayer, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_ZeroLayer(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		net.errors[peer] = errors.New("SendRequest error")
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, log.NewDefault("layerFetch"))

	layerID := types.NewLayerID(10)
	// currently first peer for each hash is queried
	layerHashes := map[types.Hash32][]peers.Peer{
		emptyHash: {net.peers[2], net.peers[1], net.peers[0], net.peers[3]},
	}

	res := <-l.PollLayerBlocks(context.TODO(), layerID, layerHashes)
	assert.Equal(t, ErrZeroLayer, res.Err)
	assert.Equal(t, layerID, res.Layer)
}
