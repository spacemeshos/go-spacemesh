package layerfetcher

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
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
	layerBlocks map[peers.Peer][]byte
	errors      map[peers.Peer]error
	timeouts    map[peers.Peer]struct{}
}

func newMockNet() *mockNet {
	return &mockNet{
		layerBlocks: make(map[peers.Peer][]byte),
		errors:      make(map[peers.Peer]error),
		timeouts:    make(map[peers.Peer]struct{}),
	}
}
func (m *mockNet) GetPeers() []peers.Peer    { return m.peers }
func (m *mockNet) PeerCount() uint64         { return uint64(len(m.peers)) }
func (m *mockNet) GetRandomPeer() peers.Peer { return m.peers[0] }
func (m *mockNet) SendRequest(_ context.Context, msgType server.MessageType, _ []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), errorHandler func(err error)) error {
	if _, ok := m.timeouts[address]; ok {
		errorHandler(errors.New("peer timeout"))
		return nil
	}
	switch msgType {
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
	layers         map[types.LayerID][]types.BlockID
	vectors        map[types.LayerID][]types.BlockID
	hashes         map[types.LayerID]types.Hash32
	aggHashes      map[types.LayerID]types.Hash32
	processed      types.LayerID
	getBlockIdsErr error
}

func newLayerDBMock() *layerDBMock {
	return &layerDBMock{
		layers:    make(map[types.LayerID][]types.BlockID),
		vectors:   make(map[types.LayerID][]types.BlockID),
		hashes:    make(map[types.LayerID]types.Hash32),
		aggHashes: make(map[types.LayerID]types.Hash32),
		processed: types.NewLayerID(999),
	}
}
func (l *layerDBMock) GetLayerInputVectorByID(id types.LayerID) ([]types.BlockID, error) {
	return l.vectors[id], nil
}
func (l *layerDBMock) SaveLayerInputVectorByID(_ context.Context, id types.LayerID, blocks []types.BlockID) error {
	l.vectors[id] = blocks
	return nil
}
func (l *layerDBMock) ProcessedLayer() types.LayerID                        { return l.processed }
func (l *layerDBMock) GetLayerHash(ID types.LayerID) types.Hash32           { return l.hashes[ID] }
func (l *layerDBMock) GetAggregatedLayerHash(ID types.LayerID) types.Hash32 { return l.aggHashes[ID] }
func (l *layerDBMock) LayerBlockIds(ID types.LayerID) ([]types.BlockID, error) {
	if l.getBlockIdsErr != nil {
		return nil, l.getBlockIdsErr
	}
	return l.layers[ID], nil
}

type mockFetcher struct {
	fetchError error
}

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
	if m.fetchError == nil {
		return nil
	}
	ch := make(chan fetch.HashDataPromiseResult, 1)
	ch <- fetch.HashDataPromiseResult{
		Err: m.fetchError,
	}
	return map[types.Hash32]chan fetch.HashDataPromiseResult{randomHash(): ch}
}

type mockBlocks struct{}

func (m mockBlocks) HandleBlockData(_ context.Context, _ []byte, _ service.Fetcher) error {
	panic("implement me")
}

type mockAtx struct{}

func (m mockAtx) HandleAtxData(_ context.Context, _ []byte, _ service.Fetcher) error {
	panic("implement me")
}

func NewMockLogic(net *mockNet, layers layerDB, blocks blockHandler, atxs atxHandler, fetcher fetch.Fetcher, log log.Log) *Logic {
	l := &Logic{
		log:            log,
		fetcher:        fetcher,
		net:            net,
		layerBlocksRes: make(map[types.LayerID]*layerResult),
		layerBlocksChs: make(map[types.LayerID][]chan LayerPromiseResult),
		atxs:           atxs,
		blockHandler:   blocks,
		layerDB:        layers,
	}
	return l
}

func TestLayerBlocksReqReceiver(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(newMockNet(), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	lyrID := types.NewLayerID(100)
	blockIDs := []types.BlockID{randomBlockID(), randomBlockID(), randomBlockID(), randomBlockID()}
	db.layers[lyrID] = blockIDs
	db.hashes[lyrID] = types.CalcBlocksHash32(types.SortBlockIDs(blockIDs), nil)
	db.aggHashes[lyrID] = randomHash()
	db.vectors[lyrID] = []types.BlockID{randomBlockID(), randomBlockID(), randomBlockID()}

	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerBlocks
	err = types.BytesToInterface(out, &got)
	assert.NoError(t, err)
	assert.Equal(t, db.layers[lyrID], got.Blocks)
	assert.Equal(t, db.vectors[lyrID], got.InputVector)
	assert.Equal(t, db.processed, got.ProcessedLayer)
	assert.Equal(t, db.hashes[lyrID], got.Hash)
	assert.Equal(t, db.aggHashes[lyrID], got.AggregatedHash)
}

func TestLayerBlocksReqReceiverEmptyLayer(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(newMockNet(), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	lyrID := types.NewLayerID(100)
	var blockIDs []types.BlockID
	db.layers[lyrID] = blockIDs
	db.hashes[lyrID] = types.EmptyLayerHash
	db.aggHashes[lyrID] = randomHash()

	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerBlocks
	err = types.BytesToInterface(out, &got)
	assert.NoError(t, err)
	assert.Equal(t, db.layers[lyrID], got.Blocks)
	assert.Nil(t, got.InputVector)
	assert.Equal(t, db.processed, got.ProcessedLayer)
	assert.Equal(t, types.EmptyLayerHash, got.Hash)
	assert.Equal(t, db.aggHashes[lyrID], got.AggregatedHash)
}

func TestLayerBlocksReqReceiverLayerNotPresent(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(newMockNet(), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	lyrID := types.NewLayerID(100)
	db.getBlockIdsErr = database.ErrNotFound
	db.aggHashes[lyrID] = randomHash()

	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerBlocks
	err = types.BytesToInterface(out, &got)
	assert.NoError(t, err)
	assert.Nil(t, got.Blocks)
	assert.Nil(t, got.InputVector)
	assert.Equal(t, db.processed, got.ProcessedLayer)
	assert.Equal(t, types.EmptyLayerHash, got.Hash)
	assert.Equal(t, db.aggHashes[lyrID], got.AggregatedHash)
}

func TestLayerBlocksReqReceiverUnknownError(t *testing.T) {
	db := newLayerDBMock()
	l := NewMockLogic(newMockNet(), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	lyrID := types.NewLayerID(100)
	db.getBlockIdsErr = errors.New("unknown")

	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Nil(t, out)
	assert.Equal(t, err, ErrInternal)
}

func TestLayerBlocksReqReceiverRequestedHigherLayer(t *testing.T) {
	db := newLayerDBMock()
	db.processed = types.NewLayerID(99)
	l := NewMockLogic(newMockNet(), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	_, err := l.layerBlocksReqReceiver(context.TODO(), db.processed.Add(1).Bytes())
	require.ErrorIs(t, err, errLayerNotProcessed)
}

func generateLayerBlocks(numInputVector int) []byte {
	return generateLayerBlocksWithHash(true, numInputVector)
}

func generateLayerBlocksWithHash(consistentHash bool, numInputVector int) []byte {
	blockIDs := []types.BlockID{randomBlockID(), randomBlockID(), randomBlockID(), randomBlockID()}
	var hash types.Hash32
	if consistentHash {
		hash = types.CalcBlocksHash32(types.SortBlockIDs(blockIDs), nil)
	} else {
		hash = randomHash()
	}
	iv := make([]types.BlockID, numInputVector)
	for i := 0; i < numInputVector; i++ {
		iv[i] = randomBlockID()
	}
	lb := layerBlocks{
		Blocks:         blockIDs,
		InputVector:    iv,
		ProcessedLayer: types.NewLayerID(10),
		Hash:           hash,
		AggregatedHash: randomHash(),
	}
	out, _ := types.InterfaceToBytes(lb)
	return out
}

func generateEmptyLayer() []byte {
	lb := layerBlocks{
		Blocks:         []types.BlockID{},
		InputVector:    nil,
		ProcessedLayer: types.NewLayerID(10),
		Hash:           types.EmptyLayerHash,
		AggregatedHash: randomHash(),
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
		net.layerBlocks[peer] = generateLayerBlocks(i + 1)
	}
	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, layerID, res.Layer)

	// should be the longest input vector from all peers
	iv, err := db.GetLayerInputVectorByID(layerID)
	assert.Nil(t, err)
	assert.Equal(t, numPeers, len(iv))
}

func TestPollLayerBlocks_FetchBlockError(t *testing.T) {
	net := newMockNet()
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = generateLayerBlocks(i + 1)
	}
	l := NewMockLogic(net, newLayerDBMock(), &mockBlocks{}, &mockAtx{}, &mockFetcher{fetchError: ErrInternal}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.Equal(t, ErrBlockNotFetched, res.Err)
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
			net.layerBlocks[peer] = generateLayerBlocks(i + 1)
		} else {
			net.errors[peer] = errors.New("SendRequest error")
		}
	}
	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerContent(context.TODO(), layerID)
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
		if i == numPeers-1 {
			net.layerBlocks[peer] = generateEmptyLayer()
		} else {
			net.errors[peer] = errors.New("SendRequest error")
		}
	}
	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerContent(context.TODO(), layerID)
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
		net.layerBlocks[peer] = generateEmptyLayer()
	}
	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.Equal(t, ErrZeroLayer, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_MissingBlocks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	fetcher := mocks.NewMockFetcher(ctrl)
	fetcher.EXPECT().GetHashes(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(hashes []types.Hash32, _ fetch.Hint, _ bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
			rst := map[types.Hash32]chan fetch.HashDataPromiseResult{}
			for _, hash := range hashes {
				rst[hash] = make(chan fetch.HashDataPromiseResult, 1)
				rst[hash] <- fetch.HashDataPromiseResult{
					Hash: hash,
					Err:  errors.New("failed request"),
				}
			}
			return rst
		},
	)
	fetcher.EXPECT().GetHashes(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(hashes []types.Hash32, _ fetch.Hint, _ bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
			return nil
		},
	)

	requested := types.NewLayerID(20)
	blocks := &layerBlocks{
		Blocks:         []types.BlockID{{1, 1, 1}, {2, 2, 2}, {3, 3, 3}},
		ProcessedLayer: requested,
	}
	data, err := codec.Encode(blocks)
	require.NoError(t, err)
	net := newMockNet()
	for i := 0; i < 2; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = data
	}

	l := NewMockLogic(net, newLayerDBMock(), &mockBlocks{}, &mockAtx{}, fetcher, logtest.New(t))
	res := <-l.PollLayerContent(context.TODO(), requested)
	require.ErrorIs(t, res.Err, ErrBlockNotFetched)
}
