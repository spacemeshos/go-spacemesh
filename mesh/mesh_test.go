package mesh

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	numBallots = 10
	numBlocks  = 5
	numTXs     = 20
)

type MockTxMemPool struct {
	db map[types.TransactionID]*types.Transaction
}

func newMockTxMemPool() *MockTxMemPool {
	return &MockTxMemPool{
		db: make(map[types.TransactionID]*types.Transaction),
	}
}

func (m *MockTxMemPool) Get(ID types.TransactionID) (*types.Transaction, error) {
	tx, ok := m.db[ID]
	if !ok {
		return nil, errors.New("tx not found")
	}
	return tx, nil
}

func (m *MockTxMemPool) Put(ID types.TransactionID, t *types.Transaction) {
	m.db[ID] = t
}

func (m *MockTxMemPool) Invalidate(ID types.TransactionID) {
	delete(m.db, ID)
}

type testMesh struct {
	*Mesh
	ctrl         *gomock.Controller
	mockState    *mocks.Mockstate
	mockTortoise *mocks.Mocktortoise
}

func createTestMesh(t *testing.T) *testMesh {
	t.Helper()
	types.SetLayersPerEpoch(3)
	lg := logtest.New(t)
	mmdb := NewMemMeshDB(lg)
	ctrl := gomock.NewController(t)
	tm := &testMesh{
		ctrl:         ctrl,
		mockState:    mocks.NewMockstate(ctrl),
		mockTortoise: mocks.NewMocktortoise(ctrl),
	}
	tm.Mesh = NewMesh(mmdb, nil, tm.mockTortoise, newMockTxMemPool(), tm.mockState, lg)
	return tm
}

func addTransactions(t testing.TB, mesh *DB, layerID types.LayerID) []types.TransactionID {
	t.Helper()
	txs := make([]*types.Transaction, 0, numTXs)
	txIDs := make([]types.TransactionID, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		tx, err := types.NewSignedTx(1, types.HexToAddress("1"), 10, 100, rand.Uint64(), signing.NewEdSigner())
		require.NoError(t, err)
		txs = append(txs, tx)
		txIDs = append(txIDs, tx.ID())
	}
	require.NoError(t, mesh.writeTransactions(layerID, types.EmptyBlockID, txs...))
	return txIDs
}

func createBlock(t testing.TB, mesh *Mesh, layerID types.LayerID, nodeID types.NodeID) *types.Block {
	t.Helper()
	coinbase := types.HexToAddress(nodeID.Key)
	txIDs := addTransactions(t, mesh.DB, layerID)
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			Rewards: []types.AnyReward{
				{
					Address:     coinbase,
					SmesherID:   nodeID,
					Amount:      unitReward,
					LayerReward: unitLayerReward,
				},
			},
			TxIDs: txIDs,
		},
	}
	b.Initialize()
	require.NoError(t, mesh.AddBlock(b))
	require.NoError(t, mesh.SaveContextualValidity(b.ID(), layerID, true))
	return b
}

func createLayerBallotsAndBlocks(t *testing.T, mesh *Mesh, lyrID types.LayerID) ([]*types.Ballot, []*types.Block) {
	t.Helper()
	return createLayerBallots(t, mesh, lyrID), createLayerBlocks(t, mesh, lyrID)
}

func createLayerBlocks(t *testing.T, mesh *Mesh, lyrID types.LayerID) []*types.Block {
	t.Helper()
	blocks := make([]*types.Block, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		nodeID := types.NodeID{Key: strconv.Itoa(i), VRFPublicKey: []byte("bbbbb")}
		blk := createBlock(t, mesh, lyrID, nodeID)
		blocks = append(blocks, blk)
	}
	return blocks
}

func createLayerBallots(t *testing.T, mesh *Mesh, lyrID types.LayerID) []*types.Ballot {
	t.Helper()
	ballots := make([]*types.Ballot, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballot := types.GenLayerBallot(lyrID)
		ballots = append(ballots, ballot)
		require.NoError(t, mesh.addBallot(ballot))
	}
	return ballots
}

func TestMesh_LayerHash(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	numLayers := 5
	latestLyr := gLyr.Add(uint32(numLayers))
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		_, blocks := createLayerBallotsAndBlocks(t, tm.Mesh, i)
		require.NoError(t, tm.SaveContextualValidity(types.SortBlocks(blocks)[0].ID(), i, false))
	}

	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(numLayers - 1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		require.NoError(t, err)
		// when tortoise verify a layer N, it can only determine blocks' contextual validity for layer N-1
		prevLyr, err := tm.GetLayer(i.Sub(1))
		require.NoError(t, err)
		assert.Equal(t, thisLyr.Hash(), tm.GetLayerHash(i))
		assert.Equal(t, prevLyr.Hash(), tm.GetLayerHash(i.Sub(1)))

		if prevLyr.Index().After(gLyr) {
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(2), i.Sub(1), false).Times(1)
			tm.mockState.EXPECT().ApplyLayer(prevLyr.Index(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
		} else {
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(gLyr, gLyr, false).Times(1)
		}
		require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
		// for this layer, hash is unchanged because the block contextual validity is not determined yet
		assert.Equal(t, thisLyr.Hash(), tm.GetLayerHash(i))
		if prevLyr.Index().After(gLyr) {
			// but for previous layer hash should already be changed to contain only valid proposals
			prevExpHash := types.CalcBlocksHash32(types.SortBlockIDs(prevLyr.BlocksIDs())[1:], nil)
			require.Equal(t, prevExpHash, tm.GetLayerHash(i.Sub(1)), "layer %s", i.Sub(1))
			assert.NotEqual(t, prevLyr.Hash(), tm.GetLayerHash(i.Sub(1)))
		}
	}
}

func TestMesh_GetAggregatedLayerHash(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	numLayers := 5
	latestLyr := gLyr.Add(uint32(numLayers))
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		_, blocks := createLayerBallotsAndBlocks(t, tm.Mesh, i)
		require.NoError(t, tm.SaveContextualValidity(types.SortBlocks(blocks)[0].ID(), i, false))
	}

	prevAggHash := tm.GetAggregatedLayerHash(gLyr)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(numLayers - 1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		r.NoError(err)
		prevLyr, err := tm.GetLayer(i.Sub(1))
		r.NoError(err)
		if prevLyr.Index() == gLyr {
			r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(gLyr, gLyr, false).Times(1)
			require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
			r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
			continue
		}
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(2), i.Sub(1), false).Times(1)
		tm.mockState.EXPECT().ApplyLayer(prevLyr.Index(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i.Sub(1)))
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
		require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
		// contextual validity is still not determined for thisLyr, so aggregated hash is not calculated for this layer
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
		// but for previous layer hash should already be changed to contain only valid proposals
		expHash := types.CalcBlocksHash32(types.SortBlockIDs(prevLyr.BlocksIDs())[1:], prevAggHash.Bytes())
		assert.Equal(t, expHash, tm.GetAggregatedLayerHash(prevLyr.Index()))
		prevAggHash = expHash
	}
}

func TestMesh_GetLayer(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	id := types.GetEffectiveGenesis().Add(1)
	lyr, err := tm.GetLayer(id)
	require.NoError(t, err)
	assert.Empty(t, lyr.Blocks())
	assert.Empty(t, lyr.Ballots())

	blocks := createLayerBlocks(t, tm.Mesh, id)
	ballots := createLayerBallots(t, tm.Mesh, id)
	lyr, err = tm.GetLayer(id)
	require.NoError(t, err)
	require.Equal(t, id, lyr.Index())
	require.Len(t, lyr.Ballots(), len(ballots))
	assert.ElementsMatch(t, ballots, lyr.Ballots())
	assert.ElementsMatch(t, blocks, lyr.Blocks())
}

func TestMesh_ProcessLayerPerHareOutput(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	gPlus2 := gLyr.Add(2)
	gPlus3 := gLyr.Add(3)
	gPlus4 := gLyr.Add(4)
	gPlus5 := gLyr.Add(5)
	_, blocks1 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(1))
	_, blocks2 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(2))
	_, blocks3 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(3))
	_, blocks4 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(4))
	_, blocks5 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(5))

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := tm.GetHareConsensusOutput(gPlus1)
	require.NoError(t, err)
	assert.Equal(t, blocks1[0].ID(), hareOutput)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gLyr, gPlus1, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus1, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, blocks2[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, blocks2[0].ID(), hareOutput)
	assert.Equal(t, gPlus2, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus3).Return(gPlus1, gPlus2, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus2, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus3, blocks3[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus3)
	require.NoError(t, err)
	assert.Equal(t, blocks3[0].ID(), hareOutput)
	assert.Equal(t, gPlus3, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus4).Return(gPlus2, gPlus3, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus3, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus4)
	require.NoError(t, err)
	assert.Equal(t, blocks4[0].ID(), hareOutput)
	assert.Equal(t, gPlus4, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus5).Return(gPlus3, gPlus4, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus4, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus5)
	require.NoError(t, err)
	assert.Equal(t, blocks5[0].ID(), hareOutput)
	assert.Equal(t, gPlus5, tm.ProcessedLayer())
}

func TestMesh_ProcessLayerPerHareOutput_OutOfOrder(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	gPlus2 := gLyr.Add(2)
	gPlus3 := gLyr.Add(3)
	gPlus4 := gLyr.Add(4)
	gPlus5 := gLyr.Add(5)
	_, blocks1 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(1))
	_, blocks2 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(2))
	_, blocks3 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(3))
	_, blocks4 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(4))
	_, blocks5 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(5))

	// process order is  : gPlus1, gPlus3, gPlus5, gPlus2, gPlus4
	// processed layer is: gPlus1, gPlus1, gPlus1, gPlus3, gPlus5
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := tm.GetHareConsensusOutput(gPlus1)
	require.NoError(t, err)
	assert.Equal(t, blocks1[0].ID(), hareOutput)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus3).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus3, blocks3[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus3)
	require.NoError(t, err)
	assert.Equal(t, blocks3[0].ID(), hareOutput)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus5).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus5)
	require.NoError(t, err)
	assert.Equal(t, blocks5[0].ID(), hareOutput)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gLyr, gPlus2, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus1, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus2, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(2)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, blocks2[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, blocks2[0].ID(), hareOutput)
	assert.Equal(t, gPlus3, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus4).Return(gPlus2, gPlus4, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus3, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus4, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(2)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus4)
	require.NoError(t, err)
	assert.Equal(t, blocks4[0].ID(), hareOutput)
	assert.Equal(t, gPlus5, tm.ProcessedLayer())
}

func TestMesh_ProcessLayerPerHareOutput_emptyOutput(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	_, blocks1 := createLayerBallotsAndBlocks(t, tm.Mesh, gPlus1)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := tm.GetHareConsensusOutput(gPlus1)
	require.NoError(t, err)
	require.Equal(t, blocks1[0].ID(), hareOutput)
	require.Equal(t, gPlus1, tm.ProcessedLayer())

	gPlus2 := gLyr.Add(2)
	createLayerBallotsAndBlocks(t, tm.Mesh, gPlus2)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gLyr, gPlus1, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus1, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, types.EmptyBlockID))

	// hare output saved
	hareOutput, err = tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, types.EmptyBlockID, hareOutput)

	// but processed layer has advanced
	assert.Equal(t, gPlus2, tm.ProcessedLayer())
}

func TestMesh_PersistProcessedLayer(t *testing.T) {
	tm := createTestMesh(t)
	t.Cleanup(func() {
		tm.Close()
	})
	layerID := types.NewLayerID(7)
	assert.NoError(t, tm.persistProcessedLayer(layerID))
	rLyr, err := tm.GetProcessedLayer()
	assert.NoError(t, err)
	assert.Equal(t, layerID, rLyr)
}

func TestMesh_LatestKnownLayer(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	tm.setLatestLayer(types.NewLayerID(3))
	tm.setLatestLayer(types.NewLayerID(7))
	tm.setLatestLayer(types.NewLayerID(10))
	tm.setLatestLayer(types.NewLayerID(1))
	tm.setLatestLayer(types.NewLayerID(2))
	assert.Equal(t, types.NewLayerID(10), tm.LatestLayer(), "wrong layer")
}

func TestMesh_WakeUp(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	ballots1, blocks1 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(1))
	ballots2, blocks2 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(2))

	for _, b := range append(ballots1, ballots2...) {
		got, err := tm.GetBallot(b.ID())
		assert.NoError(t, err)
		assert.Equal(t, b, got)
	}
	for _, b := range append(blocks1, blocks2...) {
		got, err := tm.GetBlock(b.ID())
		assert.NoError(t, err)
		assert.Equal(t, b, got)
	}
	recoveredMesh := NewMesh(tm.DB, nil, tm.mockTortoise, newMockTxMemPool(), tm.mockState, logtest.New(t))
	for _, b := range append(ballots1, ballots2...) {
		got, err := recoveredMesh.GetBallot(b.ID())
		assert.NoError(t, err)
		assert.Equal(t, b, got)
	}
	for _, b := range append(blocks1, blocks2...) {
		got, err := recoveredMesh.GetBlock(b.ID())
		assert.NoError(t, err)
		assert.Equal(t, b, got)
	}
}

func TestMesh_pushLayersToState(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	layerID := types.GetEffectiveGenesis().Add(1)
	createLayerBallots(t, tm.Mesh, layerID)
	signer, origin := newSignerAndAddress(t, "origin")
	tx1 := addTxToMempool(t, tm.Mesh, signer, 2468)
	tx2 := addTxToMempool(t, tm.Mesh, signer, 2469)
	tx3 := addTxToMempool(t, tm.Mesh, signer, 2470)
	tx4 := addTxToMempool(t, tm.Mesh, signer, 2471)
	tx5 := addTxToMempool(t, tm.Mesh, signer, 2472)
	pendingTXs := map[types.TransactionID]*types.Transaction{
		tx1.ID(): tx1,
		tx2.ID(): tx2,
		tx3.ID(): tx3,
		tx4.ID(): tx4,
		tx5.ID(): tx5,
	}

	// either block1 or block2 will be applied to the state
	block1 := addBlockWithTXsToMesh(t, tm.Mesh, layerID, true, tx1, tx2)
	block2 := addBlockWithTXsToMesh(t, tm.Mesh, layerID, true, tx2, tx3, tx4)
	addBlockWithTXsToMesh(t, tm.Mesh, layerID, false, tx4, tx5)
	addBlockWithTXsToMesh(t, tm.Mesh, layerID, false, tx5)

	valids := []*types.Block{block1, block2}
	types.SortBlocks(valids)
	hareOutput := valids[0]
	for _, txID := range hareOutput.TxIDs {
		delete(pendingTXs, txID)
	}

	txns := getTxns(t, tm.DB, origin)
	require.Len(t, txns, 5)
	for i := 0; i < 5; i++ {
		require.Equal(t, 2468+i, int(txns[i].Nonce))
		require.Equal(t, 111, int(txns[i].TotalAmount))
	}

	tm.mockState.EXPECT().ApplyLayer(layerID, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ types.LayerID, txs []*types.Transaction, _ map[types.Address]uint64) ([]*types.Transaction, error) {
			assert.ElementsMatch(t, hareOutput.TxIDs, types.ToTransactionIDs(txs))
			return nil, nil
		}).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	for _, tx := range pendingTXs {
		tm.mockState.EXPECT().ValidateNonceAndBalance(tx).Return(nil).Times(1)
		tm.mockState.EXPECT().AddTxToPool(tx).Return(nil).Times(1)
	}
	require.NoError(t, tm.pushLayersToState(context.TODO(), types.GetEffectiveGenesis().Add(1), types.GetEffectiveGenesis().Add(1)))

	txns = getTxns(t, tm.DB, origin)
	require.Empty(t, txns)

	// the transactions should be updated with the correct BlockID
	mtxs, missing := tm.GetMeshTransactions(hareOutput.TxIDs)
	require.Empty(t, missing)
	for _, tx := range mtxs {
		assert.Equal(t, hareOutput.ID(), tx.BlockID)
	}
}

func TestMesh_persistLayerHash(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	// persist once
	lyr := types.NewLayerID(3)
	hash := types.RandomHash()
	require.NoError(t, tm.persistLayerHash(lyr, hash))
	assert.Equal(t, hash, tm.GetLayerHash(lyr))

	// persist twice
	newHash := types.RandomHash()
	assert.NotEqual(t, newHash, hash)
	require.NoError(t, tm.persistLayerHash(lyr, newHash))
	assert.Equal(t, newHash, tm.GetLayerHash(lyr))
}

func addTxToMempool(t *testing.T, msh *Mesh, signer *signing.EdSigner, nonce uint64) *types.Transaction {
	t.Helper()
	tx := newTx(t, signer, nonce, 111)
	msh.txPool.Put(tx.ID(), tx)
	return tx
}

func addBlockWithTXsToMesh(t *testing.T, msh *Mesh, id types.LayerID, valid bool, txs ...*types.Transaction) *types.Block {
	t.Helper()
	b := types.GenLayerBlock(id, types.ToTransactionIDs(txs))
	require.NoError(t, msh.AddBlockWithTXs(context.TODO(), b))
	require.NoError(t, msh.SaveContextualValidity(b.ID(), id, valid))
	return b
}

func addManyTXsToPool(t *testing.T, msh *Mesh, numOfTxs int) ([]types.TransactionID, []*types.Transaction) {
	txs := make([]*types.Transaction, numOfTxs)
	txIDs := make([]types.TransactionID, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		tx := addTxToMempool(t, msh, signing.NewEdSigner(), rand.Uint64())
		txs[i] = tx
		txIDs[i] = tx.ID()
	}
	return txIDs, txs
}

func TestMesh_AddTXsFromProposal(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	numTXs := 6
	txIDs, txs := addManyTXsToPool(t, tm.Mesh, numTXs)
	for _, id := range txIDs {
		tx, err := tm.txPool.Get(id)
		require.NoError(t, err)
		assert.NotNil(t, tx)
	}

	layerID := types.GetEffectiveGenesis().Add(1)
	r.NoError(tm.AddTXsFromProposal(context.TODO(), layerID, types.RandomProposalID(), txIDs))

	// these TXs should be
	// - removed from mempool
	// - added to mesh with empty block id
	// - considered as pending
	for _, id := range txIDs {
		tx, err := tm.txPool.Get(id)
		assert.Error(t, err)
		assert.Nil(t, tx)
	}

	got, missing := tm.GetMeshTransactions(txIDs)
	require.Empty(t, missing)
	for i, tx := range got {
		assert.Equal(t, layerID, tx.LayerID)
		assert.Equal(t, types.EmptyBlockID, tx.BlockID)
		assert.Equal(t, txs[i].ID(), tx.Transaction.ID()) // causing tx.Transaction to calculate ID
		assert.Equal(t, *txs[i], tx.Transaction)
	}

	for _, tx := range txs {
		txns := getTxns(t, tm.DB, tx.Origin())
		r.Len(txns, 1)
	}
}

func TestMesh_AddBlockWithTXs(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	numTXs := 6
	txIDs, txs := addManyTXsToPool(t, tm.Mesh, numTXs)
	for _, id := range txIDs {
		tx, err := tm.txPool.Get(id)
		assert.NoError(t, err)
		assert.NotNil(t, tx)
	}

	layerID := types.GetEffectiveGenesis().Add(1)
	block := types.GenLayerBlock(layerID, txIDs)
	r.NoError(tm.AddBlockWithTXs(context.TODO(), block))

	// these TXs should be
	// - removed from mempool
	// - added to mesh with empty block id
	// - considered as pending
	for _, id := range txIDs {
		tx, err := tm.txPool.Get(id)
		require.Error(t, err)
		assert.Nil(t, tx)
	}

	got, missing := tm.GetMeshTransactions(txIDs)
	require.Empty(t, missing)
	for i, tx := range got {
		assert.Equal(t, layerID, tx.LayerID)
		assert.Equal(t, block.ID(), tx.BlockID)
		assert.Equal(t, txs[i].ID(), tx.Transaction.ID()) // causing tx.Transaction to calculate ID
		assert.Equal(t, *txs[i], tx.Transaction)
	}

	for _, tx := range txs {
		txns := getTxns(t, tm.DB, tx.Origin())
		r.Len(txns, 1)
	}
}

func TestMesh_ReverifyFailed(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(10)

	for lid := genesis.Add(1); !lid.After(last); lid = lid.Add(1) {
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
			Return(maxLayer(lid.Sub(2), genesis), lid.Sub(1), false)
		tm.mockState.EXPECT().GetStateRoot()
		tm.ProcessLayer(ctx, lid)
	}

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())

	last = last.Add(1)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(last.Sub(1), last, false)

	block := types.Block{}
	block.LayerIndex = last
	block.TxIDs = []types.TransactionID{{1, 1, 1}}
	require.NoError(t, tm.AddBlock(&block))
	require.NoError(t, tm.SaveContextualValidity(block.ID(), last, true))
	// no calls to svm state, as layer will be failed earlier
	require.Error(t, tm.ProcessLayer(ctx, last))
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.missingLayer)
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())

	last = last.Add(1)
	for lid := last.Sub(1); !lid.After(last); lid = lid.Add(1) {
		require.NoError(t, tm.SaveContextualValidity(block.ID(), last, false))
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
			Return(lid.Sub(1), last.Sub(1), false)
	}
	tm.mockState.EXPECT().GetStateRoot()
	for lid := last.Sub(1); !lid.After(last); lid = lid.Add(1) {
		tm.ProcessLayer(ctx, lid)
	}

	require.Empty(t, tm.MissingLayer())
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())
}

func TestMesh_MissingTransactionsFailure(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(1)

	block := types.Block{}
	block.LayerIndex = last
	block.TxIDs = []types.TransactionID{{1, 1, 1}}
	require.NoError(t, tm.AddBlock(&block))
	require.NoError(t, tm.SaveContextualValidity(block.ID(), last, true))

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(genesis, last, false)

	tm.ProcessLayer(ctx, last)
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, genesis, tm.LatestLayerInState())
}

func TestMesh_ResetAppliedOnRevert(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(10)

	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
			Return(lid.Sub(1), lid, false)
		tm.mockState.EXPECT().GetStateRoot()
		tm.SetZeroBlockLayer(lid)
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}

	require.Equal(t, last.Sub(1), tm.ProcessedLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())

	failed := genesis.Add(2)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(failed.Sub(1), last, true)
	tm.mockState.EXPECT().Rewind(failed.Sub(1))

	block := types.Block{}
	block.LayerIndex = failed
	block.TxIDs = []types.TransactionID{{1, 1, 1}}
	require.NoError(t, tm.AddBlock(&block))
	require.NoError(t, tm.SaveContextualValidity(block.ID(), failed, true))
	tm.ProcessLayer(ctx, last)

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, failed.Sub(1), tm.LatestLayerInState())
}
