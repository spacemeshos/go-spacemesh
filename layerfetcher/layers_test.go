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
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	fmocks "github.com/spacemeshos/go-spacemesh/fetch/mocks"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/layerfetcher/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	srvmocks "github.com/spacemeshos/go-spacemesh/p2p/server/mocks"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
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
	mMesh      *mocks.MockmeshProvider
	mAtxH      *mocks.MockatxHandler
	mBallotH   *mocks.MockballotHandler
	mBlocksH   *mocks.MockblockHandler
	mProposalH *mocks.MockproposalHandler
	mTxH       *mocks.MocktxHandler
	mPoetH     *mocks.MockpoetHandler
	mFetcher   *fmocks.MockFetcher
}

func createTestLogic(t *testing.T) *testLogic {
	ctrl := gomock.NewController(t)
	tl := &testLogic{
		mMesh:      mocks.NewMockmeshProvider(ctrl),
		mAtxH:      mocks.NewMockatxHandler(ctrl),
		mBallotH:   mocks.NewMockballotHandler(ctrl),
		mBlocksH:   mocks.NewMockblockHandler(ctrl),
		mProposalH: mocks.NewMockproposalHandler(ctrl),
		mTxH:       mocks.NewMocktxHandler(ctrl),
		mPoetH:     mocks.NewMockpoetHandler(ctrl),
		mFetcher:   fmocks.NewMockFetcher(ctrl),
	}
	tl.Logic = &Logic{
		log:             logtest.New(t),
		db:              sql.InMemory(),
		layerBlocksRes:  make(map[types.LayerID]*layerResult),
		layerBlocksChs:  make(map[types.LayerID][]chan LayerPromiseResult),
		msh:             tl.mMesh,
		atxHandler:      tl.mAtxH,
		ballotHandler:   tl.mBallotH,
		blockHandler:    tl.mBlocksH,
		proposalHandler: tl.mProposalH,
		txHandler:       tl.mTxH,
		poetHandler:     tl.mPoetH,
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

type lyrdata struct {
	hash, aggHash types.Hash32
	blts          []types.BallotID
	blks          []types.BlockID
}

func createLayer(t *testing.T, db *sql.Database, lid types.LayerID) *lyrdata {
	l := &lyrdata{}
	l.hash = types.RandomHash()
	require.NoError(t, layers.SetHash(db, lid, l.hash))
	l.aggHash = types.RandomHash()
	require.NoError(t, layers.SetAggregatedHash(db, lid, l.aggHash))
	for i := 0; i < 5; i++ {
		b := types.RandomBallot()
		b.LayerIndex = lid
		b.Signature = signing.NewEdSigner().Sign(b.Bytes())
		require.NoError(t, b.Initialize())
		require.NoError(t, ballots.Add(db, b))
		l.blts = append(l.blts, b.ID())

		bk := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{LayerIndex: lid})
		require.NoError(t, blocks.Add(db, bk))
		l.blks = append(l.blks, bk.ID())
	}
	return l
}

func TestLayerBlocksReqReceiver_Success(t *testing.T) {
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	lyrID := types.NewLayerID(100)
	expected := createLayer(t, tl.db, lyrID)
	hareOutput := expected.blks[0]
	require.NoError(t, layers.SetHareOutput(tl.db, lyrID, hareOutput))
	processed := lyrID.Add(10)
	tl.mMesh.EXPECT().ProcessedLayer().Return(processed).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerData
	err = codec.Decode(out, &got)
	require.NoError(t, err)
	assert.ElementsMatch(t, expected.blts, got.Ballots)
	assert.ElementsMatch(t, expected.blks, got.Blocks)
	assert.Equal(t, expected.blks[0], got.HareOutput)
	assert.Equal(t, processed, got.ProcessedLayer)
	assert.Equal(t, expected.hash, got.Hash)
	assert.Equal(t, expected.aggHash, got.AggregatedHash)
}

func TestLayerBlocksReqReceiver_SuccessEmptyLayer(t *testing.T) {
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	lyrID := types.NewLayerID(100)
	expected := createLayer(t, tl.db, lyrID)
	require.NoError(t, layers.SetHareOutput(tl.db, lyrID, types.EmptyBlockID))
	processed := lyrID.Add(10)
	tl.mMesh.EXPECT().ProcessedLayer().Return(processed).Times(1)

	out, err := tl.layerContentReqReceiver(context.TODO(), lyrID.Bytes())
	require.NoError(t, err)
	var got layerData
	err = codec.Decode(out, &got)
	require.NoError(t, err)
	assert.ElementsMatch(t, expected.blts, got.Ballots)
	assert.ElementsMatch(t, expected.blks, got.Blocks)
	assert.Equal(t, types.EmptyBlockID, got.HareOutput)
	assert.Equal(t, processed, got.ProcessedLayer)
	assert.Equal(t, expected.hash, got.Hash)
	assert.Equal(t, expected.aggHash, got.AggregatedHash)
}

func TestLayerBlocksReqReceiver_RequestedHigherLayer(t *testing.T) {
	lyrID := types.NewLayerID(100)
	processed := lyrID.Add(10)
	tl := createTestLogicWithMocknet(t, newMockNet(t))
	tl.mMesh.EXPECT().ProcessedLayer().Return(processed).Times(1)

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
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil).Times(numPeers)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, got)
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
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil).Times(numPeers)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
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
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).DoAndReturn(
		func([]types.Hash32, datastore.Hint, bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
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
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).DoAndReturn(
		func([]types.Hash32, datastore.Hint, bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
			ch := make(chan fetch.HashDataPromiseResult, 1)
			ch <- fetch.HashDataPromiseResult{
				Err: ErrInternal,
			}
			return map[types.Hash32]chan fetch.HashDataPromiseResult{types.RandomHash(): ch}
		}).Times(numPeers)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, got)
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
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(1)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.Nil(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, got)
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
	tl.mMesh.EXPECT().SetZeroBlockLayer(layerID).Return(nil).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
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
	tl.mMesh.EXPECT().SetZeroBlockLayer(layerID).Return(nil).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
}

func TestPollLayerBlocks_MissingBlocks(t *testing.T) {
	requested := types.NewLayerID(20)
	blks := &layerData{
		Blocks:         []types.BlockID{{1, 1, 1}, {2, 2, 2}, {3, 3, 3}},
		ProcessedLayer: requested,
	}
	data, err := codec.Encode(blks)
	require.NoError(t, err)
	net := newMockNet(t)
	numPeers := 2
	for i := 0; i < numPeers; i++ {
		peer := randPeer(t)
		net.peers = append(net.peers, peer)
		net.layerData[peer] = data
	}

	tl := createTestLogicWithMocknet(t, net)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).AnyTimes()
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).DoAndReturn(
		func(hashes []types.Hash32, _ datastore.Hint, _ bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
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
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).DoAndReturn(
		func(hashes []types.Hash32, _ datastore.Hint, _ bool) map[types.Hash32]chan fetch.HashDataPromiseResult {
			return nil
		},
	).AnyTimes()

	res := <-tl.PollLayerContent(context.TODO(), requested)
	assert.Nil(t, res.Err)
	got, err := layers.GetHareOutput(tl.db, requested)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
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
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil).Times(numPeers)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, got)
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
	tl.mMesh.EXPECT().SetZeroBlockLayer(layerID).Return(errors.New("whatever")).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
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
	tl.mMesh.EXPECT().SetZeroBlockLayer(layerID).Return(nil).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
}

func TestGetBlocks_FetchAllError(t *testing.T) {
	l := createTestLogic(t)
	blks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blks)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.BlockDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBlocks(context.TODO(), blockIDs), errUnknown)
}

func TestGetBlocks_FetchSomeError(t *testing.T) {
	l := createTestLogic(t)
	blks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blks)
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
			data, err := codec.Encode(blks[i])
			require.NoError(t, err)
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Data: data,
			}
			l.mBlocksH.EXPECT().HandleBlockData(gomock.Any(), data).Return(nil).Times(1)
		}

		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.BlockDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBlocks(context.TODO(), blockIDs), errUnknown)
}

func TestGetBlocks_HandlerError(t *testing.T) {
	l := createTestLogic(t)
	blks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blks)
	hashes := types.BlockIDsToHashes(blockIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(blks[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mBlocksH.EXPECT().HandleBlockData(gomock.Any(), data).Return(errUnknown).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.BlockDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBlocks(context.TODO(), blockIDs), errUnknown)
}

func TestGetBlocks(t *testing.T) {
	l := createTestLogic(t)
	blks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blks)
	hashes := types.BlockIDsToHashes(blockIDs)

	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(blks[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mBlocksH.EXPECT().HandleBlockData(gomock.Any(), data).Return(nil).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.BlockDB, false).Return(results).Times(1)
	assert.NoError(t, l.GetBlocks(context.TODO(), blockIDs))
}

func TestGetBallots_FetchAllError(t *testing.T) {
	l := createTestLogic(t)
	blts := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(blts)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.BallotDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBallots(context.TODO(), ballotIDs), errUnknown)
}

func TestGetBallots_FetchSomeError(t *testing.T) {
	l := createTestLogic(t)
	blts := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(blts)
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
			data, err := codec.Encode(blts[i])
			require.NoError(t, err)
			ch <- fetch.HashDataPromiseResult{
				Hash: h,
				Data: data,
			}
			l.mBallotH.EXPECT().HandleBallotData(gomock.Any(), data).Return(nil).Times(1)
		}

		results[h] = ch
	}

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.BallotDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBallots(context.TODO(), ballotIDs), errUnknown)
}

func TestGetBallots_HandlerError(t *testing.T) {
	l := createTestLogic(t)
	blts := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(blts)
	hashes := types.BallotIDsToHashes(ballotIDs)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(blts[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mBallotH.EXPECT().HandleBallotData(gomock.Any(), data).Return(errUnknown).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.BallotDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBallots(context.TODO(), ballotIDs), errUnknown)
}

func TestGetBallots(t *testing.T) {
	l := createTestLogic(t)
	blts := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(blts)
	hashes := types.BallotIDsToHashes(ballotIDs)

	results := make(map[types.Hash32]chan fetch.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan fetch.HashDataPromiseResult, 1)
		data, err := codec.Encode(blts[i])
		require.NoError(t, err)
		ch <- fetch.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mBallotH.EXPECT().HandleBallotData(gomock.Any(), data).Return(nil).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.BallotDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.ProposalDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.ProposalDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.ProposalDB, false).Return(results).Times(1)
	assert.NoError(t, l.GetProposals(context.TODO(), proposalIDs))
}

func genTx(t *testing.T, signer *signing.EdSigner, dest types.Address, amount, nonce, price uint64) types.Transaction {
	t.Helper()
	raw := wallet.Spend(signer.PrivateKey(), dest, amount,
		sdk.WithNonce(types.Nonce{Counter: nonce}),
	)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = 100
	tx.MaxSpend = amount
	tx.GasPrice = price
	tx.Nonce = types.Nonce{Counter: nonce}
	tx.Principal = types.BytesToAddress(signer.PublicKey().Bytes())
	return tx
}

func genTransactions(t *testing.T, num int) []*types.Transaction {
	t.Helper()
	txs := make([]*types.Transaction, 0, num)
	for i := 0; i < num; i++ {
		tx := genTx(t, signing.NewEdSigner(), types.Address{1}, 1, 1, 1)
		txs = append(txs, &tx)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.TXDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.TXDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.TXDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.ATXDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.ATXDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.ATXDB, false).Return(results).Times(1)
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

	l.mFetcher.EXPECT().GetHash(h, datastore.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(nil).Times(1)
	assert.NoError(t, l.GetPoetProof(context.TODO(), h))

	ch <- fetch.HashDataPromiseResult{
		Hash: h,
		Data: data,
	}
	l.mFetcher.EXPECT().GetHash(h, datastore.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(sql.ErrObjectExists).Times(1)
	assert.NoError(t, l.GetPoetProof(context.TODO(), h))

	ch <- fetch.HashDataPromiseResult{
		Hash: h,
		Data: data,
	}
	l.mFetcher.EXPECT().GetHash(h, datastore.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(errors.New("unknown")).Times(1)
	assert.Error(t, l.GetPoetProof(context.TODO(), h))
}
