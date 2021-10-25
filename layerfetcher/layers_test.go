package layerfetcher

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	lyrMocks "github.com/spacemeshos/go-spacemesh/layerfetcher/mocks"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	srvmocks "github.com/spacemeshos/go-spacemesh/p2p/server/mocks"
	"github.com/spacemeshos/go-spacemesh/rand"
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

// RandomBlockID generates random block id.
func randomBlockID() types.BlockID {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return types.BlockID{}
	}
	return types.BlockID(types.CalcHash32(b).ToHash20())
}

func randPeer(tb testing.TB) p2p.Peer {
	tb.Helper()
	buf := make([]byte, 20)
	_, err := rand.Read(buf)
	require.NoError(tb, err)
	return p2p.Peer(buf)
}

type mockNet struct {
	server.Host
	peers       []p2p.Peer
	layerBlocks map[p2p.Peer][]byte
	errors      map[p2p.Peer]error
	timeouts    map[p2p.Peer]struct{}
}

func newMockNet(tb testing.TB) *mockNet {
	ctrl := gomock.NewController(tb)
	h := srvmocks.NewMockHost(ctrl)
	h.EXPECT().SetStreamHandler(gomock.Any(), gomock.Any()).AnyTimes()
	return &mockNet{
		Host:        h,
		layerBlocks: make(map[p2p.Peer][]byte),
		errors:      make(map[p2p.Peer]error),
		timeouts:    make(map[p2p.Peer]struct{}),
	}
}
func (m *mockNet) GetPeers() []p2p.Peer { return m.peers }
func (m *mockNet) PeerCount() uint64    { return uint64(len(m.peers)) }

func (m *mockNet) Request(_ context.Context, pid p2p.Peer, _ []byte, resHandler func(msg []byte), errorHandler func(err error)) error {
	if _, ok := m.timeouts[pid]; ok {
		errorHandler(errors.New("peer timeout"))
		return nil
	}
	if data, ok := m.layerBlocks[pid]; ok {
		resHandler(data)
		return nil
	}
	return m.errors[pid]
}
func (mockNet) Close() {}

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

func (m mockBlocks) HandleBlockData(_ context.Context, _ []byte) error {
	panic("implement me")
}

type mockAtx struct{}

func (m mockAtx) HandleAtxData(_ context.Context, _ []byte) error {
	panic("implement me")
}

func NewMockLogic(net *mockNet, layers layerDB, blocks blockHandler, atxs atxHandler, fetcher fetch.Fetcher, log log.Log) *Logic {
	l := &Logic{
		log:            log,
		fetcher:        fetcher,
		host:           net,
		layerBlocksRes: make(map[types.LayerID]*layerResult),
		layerBlocksChs: make(map[types.LayerID][]chan LayerPromiseResult),
		atxs:           atxs,
		blockHandler:   blocks,
		layerDB:        layers,
	}
	l.blocksrv = net
	l.atxsrv = net
	return l
}

func TestLayerBlocksReqReceiver_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lyrID := types.NewLayerID(100)
	processed := lyrID.Add(10)
	hash := randomHash()
	aggHash := randomHash()
	blocks := []types.BlockID{randomBlockID(), randomBlockID(), randomBlockID(), randomBlockID()}
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().ProcessedLayer().Return(processed).Times(1)
	db.EXPECT().GetLayerHash(lyrID).Return(hash).Times(1)
	db.EXPECT().GetAggregatedLayerHash(lyrID).Return(aggHash).Times(1)
	db.EXPECT().LayerBlockIds(lyrID).Return(blocks, nil).Times(1)
	db.EXPECT().GetLayerInputVectorByID(lyrID).Return(blocks[1:], nil).Times(1)

	l := NewMockLogic(newMockNet(t), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerBlocks
	err = types.BytesToInterface(out, &got)
	require.NoError(t, err)
	assert.Equal(t, blocks, got.Blocks)
	assert.Equal(t, blocks[1:], got.InputVector)
	assert.Equal(t, processed, got.ProcessedLayer)
	assert.Equal(t, hash, got.Hash)
	assert.Equal(t, aggHash, got.AggregatedHash)
}

func TestLayerBlocksReqReceiver_SuccessEmptyLayer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lyrID := types.NewLayerID(100)
	processed := lyrID.Add(10)
	aggHash := randomHash()
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().ProcessedLayer().Return(processed).Times(1)
	db.EXPECT().GetLayerHash(lyrID).Return(types.EmptyLayerHash).Times(1)
	db.EXPECT().GetAggregatedLayerHash(lyrID).Return(aggHash).Times(1)
	db.EXPECT().LayerBlockIds(lyrID).Return([]types.BlockID{}, nil).Times(1)
	db.EXPECT().GetLayerInputVectorByID(lyrID).Return([]types.BlockID{}, nil).Times(1)

	l := NewMockLogic(newMockNet(t), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerBlocks
	err = types.BytesToInterface(out, &got)
	require.NoError(t, err)
	assert.Empty(t, got.Blocks)
	assert.Empty(t, got.InputVector)
	assert.Equal(t, processed, got.ProcessedLayer)
	assert.Equal(t, types.EmptyLayerHash, got.Hash)
	assert.Equal(t, aggHash, got.AggregatedHash)
}

func TestLayerBlocksReqReceiver_LayerNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lyrID := types.NewLayerID(100)
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().ProcessedLayer().Return(lyrID.Add(10)).Times(1)
	db.EXPECT().GetLayerHash(lyrID).Return(randomHash()).Times(1)
	db.EXPECT().GetAggregatedLayerHash(lyrID).Return(randomHash()).Times(1)
	db.EXPECT().LayerBlockIds(lyrID).Return(nil, database.ErrNotFound).Times(1)

	l := NewMockLogic(newMockNet(t), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Equal(t, ErrInternal, err)
	assert.Empty(t, out)
}

func TestLayerBlocksReqReceiver_GetBlockIDsUnknownError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lyrID := types.NewLayerID(100)
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().ProcessedLayer().Return(lyrID.Add(10)).Times(1)
	db.EXPECT().GetLayerHash(lyrID).Return(randomHash()).Times(1)
	db.EXPECT().GetAggregatedLayerHash(lyrID).Return(randomHash()).Times(1)
	db.EXPECT().LayerBlockIds(lyrID).Return(nil, errors.New("whatever")).Times(1)
	l := NewMockLogic(newMockNet(t), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Nil(t, out)
	assert.Equal(t, err, ErrInternal)
}

func TestLayerBlocksReqReceiver_GetInputVectorError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lyrID := types.NewLayerID(100)
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().ProcessedLayer().Return(lyrID.Add(10)).Times(1)
	db.EXPECT().GetLayerHash(lyrID).Return(randomHash()).Times(1)
	db.EXPECT().GetAggregatedLayerHash(lyrID).Return(randomHash()).Times(1)
	db.EXPECT().LayerBlockIds(lyrID).Return([]types.BlockID{}, nil).Times(1)
	db.EXPECT().GetLayerInputVectorByID(lyrID).Return(nil, errors.New("whatever")).Times(1)

	l := NewMockLogic(newMockNet(t), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	out, err := l.layerBlocksReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Equal(t, ErrInternal, err)
	assert.Empty(t, out)
}

func TestLayerBlocksReqReceiver_RequestedHigherLayer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	processed := types.NewLayerID(99)
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().ProcessedLayer().Return(processed).Times(1)

	l := NewMockLogic(newMockNet(t), db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	out, err := l.layerBlocksReqReceiver(context.TODO(), processed.Add(1).Bytes())
	assert.ErrorIs(t, err, errLayerNotProcessed)
	assert.Empty(t, out)
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
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = generateLayerBlocks(i + 1)
	}

	layerID := types.NewLayerID(10)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().SaveLayerInputVectorByID(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, iv []types.BlockID) interface{} {
			assert.Equal(t, numPeers, len(iv))
			return nil
		}).Times(1)

	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_FetchBlockError(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = generateLayerBlocks(i + 1)
	}

	layerID := types.NewLayerID(10)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	db := lyrMocks.NewMocklayerDB(ctrl)

	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{fetchError: ErrInternal}, logtest.New(t))
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.Equal(t, ErrBlockNotFetched, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_OnlyOneHasBlockData(t *testing.T) {
	types.SetLayersPerEpoch(5)

	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		if i == 2 {
			net.layerBlocks[peer] = generateLayerBlocks(i + 1)
		} else {
			net.errors[peer] = errors.New("SendRequest error")
		}
	}

	layerID := types.NewLayerID(10)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().SaveLayerInputVectorByID(gomock.Any(), layerID, gomock.Any()).Return(nil).Times(1)

	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_OneZeroLayerAmongstErrors(t *testing.T) {
	types.SetLayersPerEpoch(5)

	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		if i == numPeers-1 {
			net.layerBlocks[peer] = generateEmptyLayer()
		} else {
			net.errors[peer] = errors.New("SendRequest error")
		}
	}

	layerID := types.NewLayerID(10)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().SetZeroBlockLayer(layerID).Return(nil).Times(1)

	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_ZeroLayer(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = generateEmptyLayer()
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	db := lyrMocks.NewMocklayerDB(ctrl)
	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	db.EXPECT().SetZeroBlockLayer(layerID).Return(nil).Times(1)
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
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
	net := newMockNet(t)
	for i := 0; i < 2; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = data
	}

	l := NewMockLogic(net, lyrMocks.NewMocklayerDB(ctrl), &mockBlocks{}, &mockAtx{}, fetcher, logtest.New(t))
	res := <-l.PollLayerContent(context.TODO(), requested)
	require.ErrorIs(t, res.Err, ErrBlockNotFetched)
}

func TestPollLayerBlocks_FailureToSaveZeroLayerIgnored(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = generateEmptyLayer()
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	db := lyrMocks.NewMocklayerDB(ctrl)
	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))

	layerID := types.NewLayerID(10)
	errUnknown := errors.New("whatever")
	db.EXPECT().SetZeroBlockLayer(layerID).Return(errUnknown).Times(1)
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_FailedToSaveInputVector(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerBlocks[peer] = generateLayerBlocks(i + 1)
	}

	layerID := types.NewLayerID(10)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	errUnknown := errors.New("whatever")
	db := lyrMocks.NewMocklayerDB(ctrl)
	db.EXPECT().SaveLayerInputVectorByID(gomock.Any(), layerID, gomock.Any()).Return(errUnknown).Times(1)

	l := NewMockLogic(net, db, &mockBlocks{}, &mockAtx{}, &mockFetcher{}, logtest.New(t))
	res := <-l.PollLayerContent(context.TODO(), layerID)
	assert.Equal(t, errUnknown, res.Err)
	assert.Equal(t, layerID, res.Layer)
}
