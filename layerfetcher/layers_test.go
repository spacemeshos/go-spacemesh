package layerfetcher

import (
	"context"
	"errors"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func randomHash() types.Hash32 {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	// Note that Err == nil only if we read len(b) bytes.
	if err != nil {
		return types.Hash32{}
	}
	return types.CalcHash32(b)
}

// RandomBlockID generates random block id
func randomBlockID() types.BlockID {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return types.BlockID{}
	}
	return types.BlockID(types.CalcHash32(b).ToHash20())
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
func (m *mockNet) SendRequest(_ context.Context, msgType server.MessageType, _ []byte, address p2pcrypto.PublicKey, resHandler func(resp server.Response), timeoutHandler func(err error)) error {
	if _, ok := m.timeouts[address]; ok {
		timeoutHandler(errors.New("peer timeout"))
		return nil
	}
	switch msgType {
	case server.LayerHashMsg:
		if data, ok := m.layerHashes[address]; ok {
			resHandler(&mockResponse{data: data})
			return nil
		}
	case server.LayerBlocksMsg:
		if data, ok := m.layerBlocks[address]; ok {
			resHandler(&mockResponse{data: data})
			return nil
		}
	}
	return m.errors[address]
}
func (mockNet) Close() {}

type mockResponse struct {
	data []byte
}

func (r *mockResponse) GetData() []byte { return r.data }
func (r *mockResponse) GetError() error { return nil }

type layerDBMock struct {
	layers    map[types.Hash32][]types.BlockID
	vectors   map[types.Hash32][]types.BlockID
	gossip    []types.BlockID
	hashes    map[types.LayerID]types.Hash32
	aggHashes map[types.LayerID]types.Hash32
	processed types.LayerID
}

func newLayerDBMock() *layerDBMock {
	return &layerDBMock{
		layers:    make(map[types.Hash32][]types.BlockID),
		vectors:   make(map[types.Hash32][]types.BlockID),
		gossip:    []types.BlockID{},
		hashes:    make(map[types.LayerID]types.Hash32),
		aggHashes: make(map[types.LayerID]types.Hash32),
		processed: types.NewLayerID(10),
	}
}
func (l *layerDBMock) GetLayerInputVector(hash types.Hash32) ([]types.BlockID, error) {
	return l.vectors[hash], nil
}
func (l *layerDBMock) SaveLayerInputVectorByID(ctx context.Context, id types.LayerID, blocks []types.BlockID) error {
	l.vectors[types.CalcHash32(id.Bytes())] = blocks
	return nil
}
func (l *layerDBMock) ProcessedLayer() types.LayerID                        { return l.processed }
func (l *layerDBMock) GetLayerHash(ID types.LayerID) types.Hash32           { return l.hashes[ID] }
func (l *layerDBMock) GetAggregatedLayerHash(ID types.LayerID) types.Hash32 { return l.aggHashes[ID] }
func (l *layerDBMock) GetLayerHashBlocks(hash types.Hash32) []types.BlockID { return l.layers[hash] }
func (l *layerDBMock) Get() []types.BlockID                                 { return l.gossip }

type mockFetcher struct{}

func (m mockFetcher) Stop()                                 {}
func (m mockFetcher) Start()                                {}
func (m mockFetcher) AddDB(_ fetch.Hint, _ database.Getter) {}
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
	l := NewMockLogic(newMockNet(), db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	hash := randomHash()
	aggHash := randomHash()
	db.hashes[layerID] = hash
	db.aggHashes[layerID] = aggHash
	out := l.layerHashReqReceiver(context.TODO(), layerID.Bytes())
	// out is serialized by server.SerializeResponse()
	resp, err := server.DeserializeResponse(l.log, out)
	require.NoError(t, err)
	var lyrHash layerHash
	assert.NoError(t, types.BytesToInterface(resp.GetData(), &lyrHash))
	assert.Equal(t, db.processed, lyrHash.ProcessedLayer)
	assert.Equal(t, hash, lyrHash.Hash)
	assert.Equal(t, aggHash, lyrHash.AggregatedHash)
}

func TestLayerHashBlocksReqReceiver(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(newMockNet(), db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	h := randomHash()
	db.layers[h] = []types.BlockID{randomBlockID(), randomBlockID(), randomBlockID(), randomBlockID()}
	db.vectors[h] = []types.BlockID{randomBlockID(), randomBlockID(), randomBlockID()}

	out := l.layerHashBlocksReqReceiver(context.TODO(), h.Bytes())
	// out is serialized by server.SerializeResponse()
	resp, err := server.DeserializeResponse(l.log, out)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NoError(t, resp.GetError())

	var act layerBlocks
	err = types.BytesToInterface(resp.GetData(), &act)
	assert.NoError(t, err)
	assert.Equal(t, act.Blocks, db.layers[h])
	assert.Equal(t, act.VerifyingVector, db.vectors[h])
	assert.Nil(t, act.LatestBlocks)
}

func TestPollLayerHash_NoPeers(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(newMockNet(), db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
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
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

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
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Equal(t, ErrTooManyPeerErrors, res.Err)
	assert.Nil(t, res.Hashes)
}

func TestPollLayerHash_SomeError(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	lyrHash := layerHash{ProcessedLayer: types.NewLayerID(20), Hash: randomHash(), AggregatedHash: randomHash()}
	data, err := types.InterfaceToBytes(&lyrHash)
	assert.NoError(t, err)
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i%2 == 0 {
			net.errors[peer] = errors.New("SendRequest error")
		} else {
			net.layerHashes[peer] = data
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, 1, len(res.Hashes))
	assert.ElementsMatch(t, []peers.Peer{net.peers[1], net.peers[3]}, res.Hashes[lyrHash])
}

func TestPollLayerHash_SomeTimeout(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	lyrHash := layerHash{ProcessedLayer: types.NewLayerID(20), Hash: randomHash(), AggregatedHash: randomHash()}
	data, err := types.InterfaceToBytes(&lyrHash)
	assert.NoError(t, err)
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i%2 == 0 {
			net.timeouts[peer] = struct{}{}
		} else {
			net.layerHashes[peer] = data
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, 1, len(res.Hashes))
	assert.ElementsMatch(t, []peers.Peer{net.peers[1], net.peers[3]}, res.Hashes[lyrHash])
}

func TestPollLayerHash_Aggregated(t *testing.T) {
	db := newLayerDBMock()
	net := newMockNet()
	numPeers := 4
	evenHash := layerHash{ProcessedLayer: types.NewLayerID(20), Hash: randomHash(), AggregatedHash: randomHash()}
	evenData, err := types.InterfaceToBytes(&evenHash)
	assert.NoError(t, err)
	oddHash := layerHash{ProcessedLayer: types.NewLayerID(21), Hash: randomHash(), AggregatedHash: randomHash()}
	oddData, err := types.InterfaceToBytes(&oddHash)
	assert.NoError(t, err)
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		if i%2 == 0 {
			net.layerHashes[peer] = evenData
		} else {
			net.layerHashes[peer] = oddData
		}
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

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
		lyrHash := layerHash{ProcessedLayer: types.NewLayerID(20 + uint32(i)), Hash: randomHash(), AggregatedHash: randomHash()}
		data, err := types.InterfaceToBytes(&lyrHash)
		assert.NoError(t, err)
		net.layerHashes[peer] = data
	}
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerHash(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, numPeers, len(res.Hashes))
}

func generateLayerBlocks() []byte {
	lb := layerBlocks{
		Blocks:          []types.BlockID{randomBlockID(), randomBlockID(), randomBlockID(), randomBlockID()},
		VerifyingVector: []types.BlockID{randomBlockID(), randomBlockID(), randomBlockID()},
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
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

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
	types.SetLayersPerEpoch(5)

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
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

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
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	// currently first peer for each hash is queried
	layerHashes := map[types.Hash32][]peers.Peer{
		mesh.EmptyLayerHash: {net.peers[2]},
		randomHash():        {net.peers[1], net.peers[0]},
		randomHash():        {net.peers[3]},
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
	l := NewMockLogic(net, db, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	// currently first peer for each hash is queried
	layerHashes := map[types.Hash32][]peers.Peer{
		mesh.EmptyLayerHash: {net.peers[2], net.peers[1], net.peers[0], net.peers[3]},
	}

	res := <-l.PollLayerBlocks(context.TODO(), layerID, layerHashes)
	assert.Equal(t, ErrZeroLayer, res.Err)
	assert.Equal(t, layerID, res.Layer)
}
