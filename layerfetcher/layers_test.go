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
	"github.com/spacemeshos/go-spacemesh/fetch"
	fmocks "github.com/spacemeshos/go-spacemesh/fetch/mocks"
	"github.com/spacemeshos/go-spacemesh/layerfetcher/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	srvmocks "github.com/spacemeshos/go-spacemesh/p2p/server/mocks"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
)

func randPeer(tb testing.TB) p2p.Peer {
	tb.Helper()
	buf := make([]byte, 20)
	_, err := rand.Read(buf)
	require.NoError(tb, err)
	return p2p.Peer(buf)
}

type mockNet struct {
	server.Host
	peers     []p2p.Peer
	layerData map[p2p.Peer][]byte
	errors    map[p2p.Peer]error
	timeouts  map[p2p.Peer]struct{}
}

func newMockNet(tb testing.TB) *mockNet {
	ctrl := gomock.NewController(tb)
	h := srvmocks.NewMockHost(ctrl)
	h.EXPECT().SetStreamHandler(gomock.Any(), gomock.Any()).AnyTimes()
	return &mockNet{
		Host:      h,
		layerData: make(map[p2p.Peer][]byte),
		errors:    make(map[p2p.Peer]error),
		timeouts:  make(map[p2p.Peer]struct{}),
	}
}
func (m *mockNet) GetPeers() []p2p.Peer { return m.peers }
func (m *mockNet) PeerCount() uint64    { return uint64(len(m.peers)) }

func (m *mockNet) Request(_ context.Context, pid p2p.Peer, _ []byte, resHandler func(msg []byte), errorHandler func(err error)) error {
	if _, ok := m.timeouts[pid]; ok {
		errorHandler(errors.New("peer timeout"))
		return nil
	}
	if data, ok := m.layerData[pid]; ok {
		resHandler(data)
		return nil
	}
	return m.errors[pid]
}
func (mockNet) Close() {}

type testLogic struct {
	*Logic
	ctrl       *gomock.Controller
	mLayerDB   *mocks.MocklayerDB
	mAtxH      *mocks.MockatxHandler
	mBallotH   *mocks.MockballotHandler
	mBlocksH   *mocks.MockblockHandler
	mProposalH *mocks.MockproposalHandler
	mTxH       *mocks.MocktxHandler
	mPoetH     *mocks.MockpoetDB
	mFetcher   *fmocks.MockFetcher
}

func createTestLogic(t *testing.T) *testLogic {
	ctrl := gomock.NewController(t)
	tl := &testLogic{
		ctrl:       ctrl,
		mLayerDB:   mocks.NewMocklayerDB(ctrl),
		mAtxH:      mocks.NewMockatxHandler(ctrl),
		mBallotH:   mocks.NewMockballotHandler(ctrl),
		mBlocksH:   mocks.NewMockblockHandler(ctrl),
		mProposalH: mocks.NewMockproposalHandler(ctrl),
		mTxH:       mocks.NewMocktxHandler(ctrl),
		mPoetH:     mocks.NewMockpoetDB(ctrl),
		mFetcher:   fmocks.NewMockFetcher(ctrl),
	}
	tl.Logic = &Logic{
		log:             logtest.New(t),
		layerBlocksRes:  make(map[types.LayerID]*layerResult),
		layerBlocksChs:  make(map[types.LayerID][]chan LayerPromiseResult),
		layerDB:         tl.mLayerDB,
		atxHandler:      tl.mAtxH,
		ballotHandler:   tl.mBallotH,
		blockHandler:    tl.mBlocksH,
		proposalHandler: tl.mProposalH,
		txHandler:       tl.mTxH,
		poetProofs:      tl.mPoetH,
		fetcher:         tl.mFetcher,
	}
	return tl
}

func createTestLogicWithMocknet(t *testing.T, net *mockNet) *testLogic {
	tl := createTestLogic(t)
	tl.host = net
	tl.blocksrv = net
	tl.atxsrv = net
	return tl
}

func TestLayerBlocksReqReceiver_Success(t *testing.T) {
	lyrID := types.NewLayerID(100)
	processed := lyrID.Add(10)
	hash := types.RandomHash()
	aggHash := types.RandomHash()
	ballots := []types.BallotID{types.RandomBallotID(), types.RandomBallotID(), types.RandomBallotID(), types.RandomBallotID()}
	blocks := []types.BlockID{types.RandomBlockID(), types.RandomBlockID(), types.RandomBlockID()}
	hareOutput := blocks[0]

	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mLayerDB.EXPECT().ProcessedLayer().Return(processed).Times(1)
	tl.mLayerDB.EXPECT().GetLayerHash(lyrID).Return(hash, nil).Times(1)
	tl.mLayerDB.EXPECT().GetAggregatedLayerHash(lyrID).Return(aggHash, nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBallotIDs(lyrID).Return(ballots, nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBlockIds(lyrID).Return(blocks, nil).Times(1)
	tl.mLayerDB.EXPECT().GetHareConsensusOutput(lyrID).Return(hareOutput, nil).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerData
	err = codec.Decode(out, &got)
	require.NoError(t, err)
	assert.Equal(t, ballots, got.Ballots)
	assert.Equal(t, blocks, got.Blocks)
	assert.Equal(t, hareOutput, got.HareOutput)
	assert.Equal(t, processed, got.ProcessedLayer)
	assert.Equal(t, hash, got.Hash)
	assert.Equal(t, aggHash, got.AggregatedHash)
}

func TestLayerBlocksReqReceiver_SuccessEmptyLayer(t *testing.T) {
	lyrID := types.NewLayerID(100)
	processed := lyrID.Add(10)
	aggHash := types.RandomHash()
	ballots := []types.BallotID{types.RandomBallotID(), types.RandomBallotID(), types.RandomBallotID(), types.RandomBallotID()}
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mLayerDB.EXPECT().ProcessedLayer().Return(processed).Times(1)
	tl.mLayerDB.EXPECT().GetLayerHash(lyrID).Return(types.EmptyLayerHash, nil).Times(1)
	tl.mLayerDB.EXPECT().GetAggregatedLayerHash(lyrID).Return(aggHash, nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBallotIDs(lyrID).Return(ballots, nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBlockIds(lyrID).Return([]types.BlockID{}, nil).Times(1)
	tl.mLayerDB.EXPECT().GetHareConsensusOutput(lyrID).Return(types.EmptyBlockID, nil).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerData
	err = codec.Decode(out, &got)
	require.NoError(t, err)
	assert.Equal(t, ballots, got.Ballots)
	assert.Empty(t, got.Blocks)
	assert.Equal(t, types.EmptyBlockID, got.HareOutput)
	assert.Equal(t, processed, got.ProcessedLayer)
	assert.Equal(t, types.EmptyLayerHash, got.Hash)
	assert.Equal(t, aggHash, got.AggregatedHash)
}

func TestLayerBlocksReqReceiver_GetHareOutputError(t *testing.T) {
	lyrID := types.NewLayerID(100)
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mLayerDB.EXPECT().ProcessedLayer().Return(lyrID.Add(10)).Times(1)
	tl.mLayerDB.EXPECT().GetLayerHash(lyrID).Return(types.RandomHash(), nil).Times(1)
	tl.mLayerDB.EXPECT().GetAggregatedLayerHash(lyrID).Return(types.RandomHash(), nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBallotIDs(lyrID).Return([]types.BallotID{types.RandomBallotID()}, nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBlockIds(lyrID).Return([]types.BlockID{types.RandomBlockID()}, nil).Times(1)
	tl.mLayerDB.EXPECT().GetHareConsensusOutput(lyrID).Return(types.EmptyBlockID, sql.ErrNotFound).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Nil(t, out)
	assert.Equal(t, err, ErrInternal)
}

func TestLayerBlocksReqReceiver_GetBlockIDsError(t *testing.T) {
	lyrID := types.NewLayerID(100)
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mLayerDB.EXPECT().ProcessedLayer().Return(lyrID.Add(10)).Times(1)
	tl.mLayerDB.EXPECT().GetLayerHash(lyrID).Return(types.RandomHash(), nil).Times(1)
	tl.mLayerDB.EXPECT().GetAggregatedLayerHash(lyrID).Return(types.RandomHash(), nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBallotIDs(lyrID).Return([]types.BallotID{types.RandomBallotID()}, nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBlockIds(lyrID).Return(nil, sql.ErrNotFound).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Nil(t, out)
	assert.Equal(t, err, ErrInternal)
}

func TestLayerBlocksReqReceiver_GetBallotIDsError(t *testing.T) {
	lyrID := types.NewLayerID(100)
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mLayerDB.EXPECT().ProcessedLayer().Return(lyrID.Add(10)).Times(1)
	tl.mLayerDB.EXPECT().GetLayerHash(lyrID).Return(types.RandomHash(), nil).Times(1)
	tl.mLayerDB.EXPECT().GetAggregatedLayerHash(lyrID).Return(types.RandomHash(), nil).Times(1)
	tl.mLayerDB.EXPECT().LayerBallotIDs(lyrID).Return(nil, sql.ErrNotFound).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Nil(t, out)
	assert.Equal(t, err, ErrInternal)
}

func TestLayerBlocksReqReceiver_GetAggLayerHashError(t *testing.T) {
	lyrID := types.NewLayerID(100)
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mLayerDB.EXPECT().ProcessedLayer().Return(lyrID.Add(10)).Times(1)
	tl.mLayerDB.EXPECT().GetLayerHash(lyrID).Return(types.RandomHash(), nil).Times(1)
	tl.mLayerDB.EXPECT().GetAggregatedLayerHash(lyrID).Return(types.EmptyLayerHash, errors.New("unknown")).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Nil(t, out)
	assert.Equal(t, err, ErrInternal)
}

func TestLayerBlocksReqReceiver_GetLayerHashError(t *testing.T) {
	lyrID := types.NewLayerID(100)
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mLayerDB.EXPECT().ProcessedLayer().Return(lyrID.Add(10)).Times(1)
	tl.mLayerDB.EXPECT().GetLayerHash(lyrID).Return(types.EmptyLayerHash, errors.New("unknown")).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	assert.Nil(t, out)
	assert.Equal(t, err, ErrInternal)
}

func TestLayerBlocksReqReceiver_RequestedHigherLayer(t *testing.T) {
	lyrID := types.NewLayerID(100)
	processed := lyrID.Add(10)
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mLayerDB.EXPECT().ProcessedLayer().Return(processed).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), processed.Add(1).Bytes())
	assert.ErrorIs(t, err, errLayerNotProcessed)
	assert.Empty(t, out)
}

const (
	numBallots = 10
	numBlocks  = 3
)

func generateLayerContent(emptyHareOutput bool) []byte {
	ballotIDs := make([]types.BallotID, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballotIDs = append(ballotIDs, types.RandomBallotID())
	}
	blockIDs := make([]types.BlockID, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blockIDs = append(blockIDs, types.RandomBlockID())
	}
	hash := types.CalcBlocksHash32(types.SortBlockIDs(blockIDs), nil)
	hareOutput := types.EmptyBlockID
	if !emptyHareOutput {
		hareOutput = blockIDs[0]
	}
	lb := layerData{
		Ballots:        ballotIDs,
		Blocks:         blockIDs,
		HareOutput:     hareOutput,
		ProcessedLayer: types.NewLayerID(10),
		Hash:           hash,
		AggregatedHash: types.RandomHash(),
	}
	out, _ := codec.Encode(lb)
	return out
}

func generateEmptyLayer() []byte {
	lb := layerData{
		Ballots:        []types.BallotID{},
		Blocks:         []types.BlockID{},
		HareOutput:     types.EmptyBlockID,
		ProcessedLayer: types.NewLayerID(10),
		Hash:           types.EmptyLayerHash,
		AggregatedHash: types.RandomHash(),
	}
	out, _ := codec.Encode(lb)
	return out
}

func TestPollLayerBlocks_AllHaveLayerData(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = generateLayerContent(false)
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BlockDB, false).Return(nil).Times(numPeers)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, blockID types.BlockID) interface{} {
			assert.NotEqual(t, blockID, types.EmptyBlockID)
			return nil
		}).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_AllHaveLayerData_EmptyHareOutput(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = generateLayerContent(true)
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BlockDB, false).Return(nil).Times(numPeers)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, types.EmptyBlockID).Return(nil).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_FetchLayerBallotsError(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = generateLayerContent(false)
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BallotDB, false).DoAndReturn(
		func([]types.Hash32, fetch.Hint, bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
			ch := make(chan fetch.HashDataPromiseResult, 1)
			ch <- fetch.HashDataPromiseResult{
				Err: ErrInternal,
			}
			return map[types.Hash32]chan fetch.HashDataPromiseResult{types.RandomHash(): ch}
		}).Times(numPeers)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.Equal(t, ErrLayerDataNotFetched, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_FetchLayerBlocksErrorIgnored(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = generateLayerContent(false)
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BlockDB, false).DoAndReturn(
		func([]types.Hash32, fetch.Hint, bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
			ch := make(chan fetch.HashDataPromiseResult, 1)
			ch <- fetch.HashDataPromiseResult{
				Err: ErrInternal,
			}
			return map[types.Hash32]chan fetch.HashDataPromiseResult{types.RandomHash(): ch}
		}).Times(numPeers)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, blockID types.BlockID) interface{} {
			assert.NotEqual(t, blockID, types.EmptyBlockID)
			return nil
		}).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_OnlyOneHasLayerData(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		if i == 2 {
			net.layerData[peer] = generateLayerContent(false)
		} else {
			net.errors[peer] = errors.New("SendRequest error")
		}
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BallotDB, false).Return(nil).Times(1)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BlockDB, false).Return(nil).Times(1)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, blockID types.BlockID) interface{} {
			assert.NotEqual(t, blockID, types.EmptyBlockID)
			return nil
		}).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
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
			net.layerData[peer] = generateEmptyLayer()
		} else {
			net.errors[peer] = errors.New("SendRequest error")
		}
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, types.EmptyBlockID).Return(nil).Times(1)
	tl.mLayerDB.EXPECT().SetZeroBlockLayer(layerID).Return(nil).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_ZeroLayer(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = generateEmptyLayer()
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, types.EmptyBlockID).Return(nil).Times(1)
	tl.mLayerDB.EXPECT().SetZeroBlockLayer(layerID).Return(nil).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_MissingBlocks(t *testing.T) {
	requested := types.NewLayerID(20)
	blocks := &layerData{
		Blocks:         []types.BlockID{{1, 1, 1}, {2, 2, 2}, {3, 3, 3}},
		ProcessedLayer: requested,
	}
	data, err := codec.Encode(blocks)
	require.NoError(t, err)
	net := newMockNet(t)
	numPeers := 2
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = data
	}

	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BallotDB, false).Return(nil).AnyTimes()
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BlockDB, false).DoAndReturn(
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
	).Times(1)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BlockDB, false).DoAndReturn(
		func(hashes []types.Hash32, _ fetch.Hint, _ bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
			return nil
		},
	).AnyTimes()
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), requested, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, blockID types.BlockID) interface{} {
			assert.Equal(t, blockID, types.EmptyBlockID)
			return nil
		}).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), requested)
	assert.Nil(t, res.Err)
}

func TestPollLayerBlocks_DifferentHareOutputIgnored(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		content := generateLayerContent(false)
		if i == 0 {
			content = generateLayerContent(true)
		}
		net.layerData[peer] = content
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BlockDB, false).Return(nil).Times(numPeers)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, blockID types.BlockID) interface{} {
			assert.NotEqual(t, blockID, types.EmptyBlockID)
			return nil
		}).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_FailureToSaveZeroBlockLayerIgnored(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = generateEmptyLayer()
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, types.EmptyBlockID).Return(nil).Times(1)
	tl.mLayerDB.EXPECT().SetZeroBlockLayer(layerID).Return(errors.New("whatever")).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_FailureToSaveZeroBallotLayerIgnored(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = generateEmptyLayer()
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, types.EmptyBlockID).Return(nil).Times(1)
	tl.mLayerDB.EXPECT().SetZeroBlockLayer(layerID).Return(nil).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestPollLayerBlocks_FailedToSaveHareOutput(t *testing.T) {
	net := newMockNet(t)
	numPeers := 4
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = generateLayerContent(false)
	}

	layerID := types.NewLayerID(10)
	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), fetch.BlockDB, false).Return(nil).Times(numPeers)
	errUnknown := errors.New("whatever")
	tl.mLayerDB.EXPECT().SaveHareConsensusOutput(gomock.Any(), layerID, gomock.Any()).Return(errUnknown).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.Equal(t, errUnknown, res.Err)
	assert.Equal(t, layerID, res.Layer)
}

func TestGetBlocks_FetchAllError(t *testing.T) {
	l := createTestLogic(t)
	blocks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blocks)
	hashes := types.BlockIDsToHashes(blockIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for _, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Err:  errUnknown,
		}
		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.BlockDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBlocks(context.TODO(), blockIDs), errUnknown)
}

func TestGetBlocks_FetchSomeError(t *testing.T) {
	l := createTestLogic(t)
	blocks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blocks)
	hashes := types.BlockIDsToHashes(blockIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		if i == 0 {
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Err:  errUnknown,
			}
		} else {
			data, err := codec.Encode(blocks[i])
			require.NoError(t, err)
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Data: data,
			}
			l.mBlocksH.EXPECT().HandleBlockData(gomock.Any(), data).Return(nil).Times(1)
		}

		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.BlockDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBlocks(context.TODO(), blockIDs), errUnknown)
}

func TestGetBlocks_HandlerError(t *testing.T) {
	l := createTestLogic(t)
	blocks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blocks)
	hashes := types.BlockIDsToHashes(blockIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(blocks[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mBlocksH.EXPECT().HandleBlockData(gomock.Any(), data).Return(errUnknown).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.BlockDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBlocks(context.TODO(), blockIDs), errUnknown)
}

func TestGetBlocks(t *testing.T) {
	l := createTestLogic(t)
	blocks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blocks)
	hashes := types.BlockIDsToHashes(blockIDs)

	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(blocks[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mBlocksH.EXPECT().HandleBlockData(gomock.Any(), data).Return(nil).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.BlockDB, false).Return(results).Times(1)
	assert.NoError(t, l.GetBlocks(context.TODO(), blockIDs))
}

func TestGetBallots_FetchAllError(t *testing.T) {
	l := createTestLogic(t)
	ballots := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(ballots)
	hashes := types.BallotIDsToHashes(ballotIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for _, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Err:  errUnknown,
		}
		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.BallotDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBallots(context.TODO(), ballotIDs), errUnknown)
}

func TestGetBallots_FetchSomeError(t *testing.T) {
	l := createTestLogic(t)
	ballots := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(ballots)
	hashes := types.BallotIDsToHashes(ballotIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		if i == 0 {
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Err:  errUnknown,
			}
		} else {
			data, err := codec.Encode(ballots[i])
			require.NoError(t, err)
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Data: data,
			}
			l.mBallotH.EXPECT().HandleBallotData(gomock.Any(), data).Return(nil).Times(1)
		}

		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.BallotDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBallots(context.TODO(), ballotIDs), errUnknown)
}

func TestGetBallots_HandlerError(t *testing.T) {
	l := createTestLogic(t)
	ballots := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(ballots)
	hashes := types.BallotIDsToHashes(ballotIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(ballots[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mBallotH.EXPECT().HandleBallotData(gomock.Any(), data).Return(errUnknown).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.BallotDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBallots(context.TODO(), ballotIDs), errUnknown)
}

func TestGetBallots(t *testing.T) {
	l := createTestLogic(t)
	ballots := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(ballots)
	hashes := types.BallotIDsToHashes(ballotIDs)

	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(ballots[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mBallotH.EXPECT().HandleBallotData(gomock.Any(), data).Return(nil).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.BallotDB, false).Return(results).Times(1)
	assert.NoError(t, l.GetBallots(context.TODO(), ballotIDs))
}

func TestGetProposals_FetchSomeError(t *testing.T) {
	l := createTestLogic(t)
	proposals := []*types.Proposal{
		types.GenLayerProposal(types.NewLayerID(10), nil),
		types.GenLayerProposal(types.NewLayerID(20), nil),
	}
	proposalIDs := types.ToProposalIDs(proposals)
	hashes := types.ProposalIDsToHashes(proposalIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		if i == 0 {
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Err:  errUnknown,
			}
		} else {
			data, err := codec.Encode(proposals[i])
			require.NoError(t, err)
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Data: data,
			}
			l.mProposalH.EXPECT().HandleProposalData(gomock.Any(), data).Return(nil).Times(1)
		}

		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.ProposalDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetProposals(context.TODO(), proposalIDs), errUnknown)
}

func TestGetProposals_HandlerError(t *testing.T) {
	l := createTestLogic(t)
	proposals := []*types.Proposal{
		types.GenLayerProposal(types.NewLayerID(10), nil),
		types.GenLayerProposal(types.NewLayerID(20), nil),
	}
	proposalIDs := types.ToProposalIDs(proposals)
	hashes := types.ProposalIDsToHashes(proposalIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(proposals[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mProposalH.EXPECT().HandleProposalData(gomock.Any(), data).Return(errUnknown).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.ProposalDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetProposals(context.TODO(), proposalIDs), errUnknown)
}

func TestGetProposals(t *testing.T) {
	l := createTestLogic(t)
	proposals := []*types.Proposal{
		types.GenLayerProposal(types.NewLayerID(10), nil),
		types.GenLayerProposal(types.NewLayerID(20), nil),
	}
	proposalIDs := types.ToProposalIDs(proposals)
	hashes := types.ProposalIDsToHashes(proposalIDs)

	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(proposals[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mProposalH.EXPECT().HandleProposalData(gomock.Any(), data).Return(nil).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.ProposalDB, false).Return(results).Times(1)
	assert.NoError(t, l.GetProposals(context.TODO(), proposalIDs))
}

func genTransactions(t *testing.T, num int) []*types.Transaction {
	t.Helper()
	txs := make([]*types.Transaction, 0, num)
	for i := 0; i < num; i++ {
		tx, err := transaction.GenerateCallTransaction(signing.NewEdSigner(), types.Address{1, 2, 3}, 197, 991, 1, 10)
		require.NoError(t, err)
		txs = append(txs, tx)
	}
	return txs
}

func TestGetTxs_FetchSomeError(t *testing.T) {
	l := createTestLogic(t)
	txs := genTransactions(t, 19)
	tids := types.ToTransactionIDs(txs)
	hashes := types.TransactionIDsToHashes(tids)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		if i == 0 {
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Err:  errUnknown,
			}
		} else {
			data, err := codec.Encode(tids[i])
			require.NoError(t, err)
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Data: data,
			}
			l.mTxH.EXPECT().HandleSyncTransaction(gomock.Any(), data).Return(nil).Times(1)
		}
		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.TXDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetTxs(context.TODO(), tids), errUnknown)
}

func TestGetTxs_HandlerError(t *testing.T) {
	l := createTestLogic(t)
	txs := genTransactions(t, 19)
	tids := types.ToTransactionIDs(txs)
	hashes := types.TransactionIDsToHashes(tids)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(tids[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mTxH.EXPECT().HandleSyncTransaction(gomock.Any(), data).Return(errUnknown).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.TXDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetTxs(context.TODO(), tids), errUnknown)
}

func TestGetTxs(t *testing.T) {
	l := createTestLogic(t)
	txs := genTransactions(t, 19)
	tids := types.ToTransactionIDs(txs)
	hashes := types.TransactionIDsToHashes(tids)

	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(tids[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mTxH.EXPECT().HandleSyncTransaction(gomock.Any(), data).Return(nil).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.TXDB, false).Return(results).Times(1)
	assert.NoError(t, l.GetTxs(context.TODO(), tids))
}

func genATXs(t *testing.T, num int) []*types.ActivationTx {
	t.Helper()
	atxs := make([]*types.ActivationTx, 0, num)
	for i := 0; i < num; i++ {
		atx := types.NewActivationTx(types.NIPostChallenge{}, types.Address{1, 2, 3}, &types.NIPost{}, uint(i), nil)
		atxs = append(atxs, atx)
	}
	return atxs
}

func TestGetAtxs_FetchSomeError(t *testing.T) {
	l := createTestLogic(t)
	atxs := genATXs(t, 19)
	atxIDs := types.ToATXIDs(atxs)
	hashes := types.ATXIDsToHashes(atxIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		if i == 0 {
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Err:  errUnknown,
			}
		} else {
			data, err := codec.Encode(atxIDs[i])
			require.NoError(t, err)
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Data: data,
			}
			l.mAtxH.EXPECT().HandleAtxData(gomock.Any(), data).Return(nil).Times(1)
		}
		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.ATXDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetAtxs(context.TODO(), atxIDs), errUnknown)
}

func TestGetAtxs_HandlerError(t *testing.T) {
	l := createTestLogic(t)
	atxs := genATXs(t, 19)
	atxIDs := types.ToATXIDs(atxs)
	hashes := types.ATXIDsToHashes(atxIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(atxs[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mAtxH.EXPECT().HandleAtxData(gomock.Any(), data).Return(errUnknown).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.ATXDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetAtxs(context.TODO(), atxIDs), errUnknown)
}

func TestGetAtxs(t *testing.T) {
	l := createTestLogic(t)
	atxs := genATXs(t, 19)
	atxIDs := types.ToATXIDs(atxs)
	hashes := types.ATXIDsToHashes(atxIDs)

	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(atxIDs[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mAtxH.EXPECT().HandleAtxData(gomock.Any(), data).Return(nil).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, fetch.ATXDB, false).Return(results).Times(1)
	assert.NoError(t, l.GetAtxs(context.TODO(), atxIDs))
}

func TestGetPoetProof(t *testing.T) {
	l := createTestLogic(t)
	proof := types.PoetProofMessage{}
	h := types.RandomHash()

	ch := make(chan fetch.HashDataPromiseResult, 1)
	data, err := codec.Encode(proof)
	require.NoError(t, err)
	ch <- fetch.HashDataPromiseResult{
		Hash: h,
		Data: data,
	}

	l.mFetcher.EXPECT().GetHash(h, fetch.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(nil).Times(1)
	assert.NoError(t, l.GetPoetProof(context.TODO(), h))

	ch <- fetch.HashDataPromiseResult{
		Hash: h,
		Data: data,
	}
	l.mFetcher.EXPECT().GetHash(h, fetch.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(sql.ErrObjectExists).Times(1)
	assert.NoError(t, l.GetPoetProof(context.TODO(), h))

	ch <- fetch.HashDataPromiseResult{
		Hash: h,
		Data: data,
	}
	l.mFetcher.EXPECT().GetHash(h, fetch.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(errors.New("unknown")).Times(1)
	assert.Error(t, l.GetPoetProof(context.TODO(), h))
}
