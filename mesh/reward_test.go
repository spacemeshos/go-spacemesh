package mesh

import (
	"context"
	"strconv"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var goldenATXID = types.ATXID(types.HexToHash32("77777"))

func ConfigTst() Config {
	return Config{
		BaseReward: 5000,
	}
}

func addTransactionsWithFee(t testing.TB, mesh *DB, numOfTxs int, fee int64) (int64, []types.TransactionID) {
	var totalFee int64
	txs := make([]*types.Transaction, 0, numOfTxs)
	txIDs := make([]types.TransactionID, 0, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		tx, err := types.NewSignedTx(1, types.HexToAddress("1"), 10, 100, uint64(fee), signing.NewEdSigner())
		assert.NoError(t, err)
		totalFee += fee
		txs = append(txs, tx)
		txIDs = append(txIDs, tx.ID())
	}
	p := &types.Proposal{}
	p.LayerIndex = types.NewLayerID(0)
	err := mesh.writeTransactions(p, txs...)
	assert.NoError(t, err)
	return totalFee, txIDs
}

func init() {
	types.SetLayersPerEpoch(3)
}

func TestMesh_AccumulateRewards_happyFlow(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	totalFee := int64(0)
	blocksData := []struct {
		numOfTxs int
		fee      int64
		addr     string
	}{
		{15, 7, "0xaaa"},
		{13, rand.Int63n(100), "0xbbb"},
		{17, rand.Int63n(100), "0xccc"},
		{16, rand.Int63n(100), "0xddd"},
	}

	lyr := types.GetEffectiveGenesis().Add(1)
	for i, data := range blocksData {
		coinbase1 := types.HexToAddress(data.addr)
		atx := newActivationTx(types.NodeID{Key: strconv.Itoa(i + 1), VRFPublicKey: []byte("bbbbb")}, 0, *types.EmptyATXID, lyr, 0, goldenATXID, coinbase1, &types.NIPost{})
		tm.mockATXDB.AddAtx(atx.ID(), atx)
		fee, txIDs := addTransactionsWithFee(t, tm.DB, data.numOfTxs, data.fee)
		totalFee += fee
		p := &types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: types.Ballot{
					InnerBallot: types.InnerBallot{
						AtxID:      atx.ID(),
						LayerIndex: lyr,
					},
				},
				TxIDs: txIDs,
			},
		}
		signer := signing.NewEdSigner()
		p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
		p.Signature = signer.Sign(p.Bytes())
		require.NoError(t, p.Initialize())
		require.NoError(t, tm.AddProposal(p))
	}

	params := NewTestRewardParams()

	l, err := tm.GetLayer(lyr)
	assert.NoError(t, err)

	var totalReward uint64
	tm.mockState.EXPECT().ApplyLayer(l.Index(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(layer types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
			for _, reward := range rewards {
				totalReward += reward
			}
			return []*types.Transaction{}, nil
		}).Times(1)

	tm.updateStateWithLayer(context.TODO(), l)
	totalRewardsCost := totalFee + int64(params.BaseReward)
	remainder := totalRewardsCost % 4

	assert.Equal(t, totalRewardsCost, int64(totalReward)+remainder)
}

func NewTestRewardParams() Config {
	return Config{
		BaseReward: 5000,
	}
}

func createBlock(t testing.TB, mesh *Mesh, lyrID types.LayerID, nodeID types.NodeID, maxTransactions int, atxDB *AtxDbMock) (*types.Block, int64) {
	coinbase := types.HexToAddress(nodeID.Key)
	atx := newActivationTx(nodeID, 0, goldenATXID, types.NewLayerID(1), 0, goldenATXID, coinbase, &types.NIPost{})
	atxDB.AddAtx(atx.ID(), atx)
	reward, txIDs := addTransactionsWithFee(t, mesh.DB, rand.Intn(maxTransactions), rand.Int63n(100))
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					AtxID:      atx.ID(),
					LayerIndex: lyrID,
				},
			},
			TxIDs: txIDs,
		},
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	require.NoError(t, mesh.SaveContextualValidity(types.BlockID(p.ID()), lyrID, true))
	require.NoError(t, mesh.AddProposal(p))
	return (*types.Block)(p), reward
}

func createLayer(t testing.TB, mesh *Mesh, lyrID types.LayerID, numOfBlocks, maxTransactions int, atxDB *AtxDbMock) (totalRewards int64, blocks []*types.Block) {
	for i := 0; i < numOfBlocks; i++ {
		nodeID := types.NodeID{Key: strconv.Itoa(i), VRFPublicKey: []byte("bbbbb")}
		blk, reward := createBlock(t, mesh, lyrID, nodeID, maxTransactions, atxDB)
		blocks = append(blocks, blk)
		totalRewards += reward
	}
	return totalRewards, blocks
}

func TestMesh_integration(t *testing.T) {
	numOfLayers := 10

	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	var l3Rewards int64
	var totalReward uint64
	gLyr := types.GetEffectiveGenesis()
	for i := gLyr.Add(1); !i.After(gLyr.Add(uint32(numOfLayers))); i = i.Add(1) {
		reward, _ := createLayer(t, tm.Mesh, i, numOfBlocks, maxTxs, tm.mockATXDB)
		// rewards are applied to layers in the past according to the reward maturity param
		if i == gLyr.Add(3) {
			l3Rewards = reward
		}

		l, err := tm.GetLayer(i)
		assert.NoError(t, err)

		oldVerified := i.Sub(2)
		if oldVerified.Before(gLyr) {
			oldVerified = gLyr
		}
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(oldVerified, i.Sub(1), false).Times(1)
		if i.Sub(1).After(gLyr) {
			tm.mockState.EXPECT().ApplyLayer(i.Sub(1), gomock.Any(), gomock.Any()).DoAndReturn(
				func(layer types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
					for _, reward := range rewards {
						totalReward += reward
					}
					return []*types.Transaction{}, nil
				}).Times(1)
			tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
			tm.mockState.EXPECT().ValidateAndAddTxToPool(gomock.Any()).Return(nil).AnyTimes()
		}
		tm.ValidateLayer(context.TODO(), l)
	}
	// since there can be a difference of up to x lerners where x is the number of proposals due to round up of penalties when distributed among all proposals
	totalPayout := l3Rewards + int64(ConfigTst().BaseReward)
	assert.True(t, totalPayout-int64(totalReward) < int64(numOfBlocks), " rewards : %v, total %v proposals %v", totalPayout, totalReward, int64(numOfBlocks))
}

func createMeshFromSyncing(t *testing.T, finalLyr types.LayerID, tm *testMesh, atxDB *AtxDbMock) ([]*types.Transaction, map[types.Address]uint64) {
	gLyr := types.GetEffectiveGenesis()
	var allTXs []*types.Transaction
	allRewards := make(map[types.Address]uint64)
	for i := gLyr.Add(1); !i.After(finalLyr); i = i.Add(1) {
		_, blocks := createLayer(t, tm.Mesh, i, numOfBlocks, maxTxs, atxDB)
		oldVerified := i.Sub(2)
		if oldVerified.Before(gLyr) {
			oldVerified = gLyr
		}
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(oldVerified, i.Sub(1), false).Times(1)
		if i.Sub(1).After(gLyr) {
			tm.mockState.EXPECT().ApplyLayer(i.Sub(1), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
					allTXs = append(allTXs, txs...)
					for addr, reward := range rewards {
						allRewards[addr] += reward
					}
					return nil, nil
				}).Times(1)
			tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
			tm.mockState.EXPECT().ValidateAndAddTxToPool(gomock.Any()).Return(nil).AnyTimes()
		}
		tm.ValidateLayer(context.TODO(), types.NewExistingLayer(i, blocks))
	}
	return allTXs, allRewards
}

func createMeshFromHareOutput(t *testing.T, finalLyr types.LayerID, tm *testMesh, atxDB *AtxDbMock) ([]*types.Transaction, map[types.Address]uint64) {
	gLyr := types.GetEffectiveGenesis()
	var allTXs []*types.Transaction
	allRewards := make(map[types.Address]uint64)
	for i := gLyr.Add(1); !i.After(finalLyr); i = i.Add(1) {
		_, blocks := createLayer(t, tm.Mesh, i, numOfBlocks, maxTxs, atxDB)
		blockIDs := types.BlockIDs(blocks)
		oldVerified := i.Sub(2)
		if oldVerified.Before(gLyr) {
			oldVerified = gLyr
		}
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(oldVerified, i.Sub(1), false).Times(1)
		tm.mockFetch.EXPECT().GetBlocks(gomock.Any(), blockIDs).Times(1)
		if i.Sub(1).After(gLyr) {
			tm.mockState.EXPECT().ApplyLayer(i.Sub(1), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
					allTXs = append(allTXs, txs...)
					for addr, reward := range rewards {
						allRewards[addr] += reward
					}
					return nil, nil
				}).Times(1)
			tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
			tm.mockState.EXPECT().ValidateAndAddTxToPool(gomock.Any()).Return(nil).AnyTimes()
		}
		tm.HandleValidatedLayer(context.TODO(), i, blockIDs)
	}
	return allTXs, allRewards
}

// test states are the same when one input is data polled from peers and the other from hare's output.
func TestMesh_updateStateWithLayer_SyncingAndHareReachSameState(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	tm1 := createTestMesh(t)
	defer tm1.ctrl.Finish()
	defer tm1.Close()
	txs1, rewards1 := createMeshFromSyncing(t, finalLyr, tm1, tm1.mockATXDB)

	tm2 := createTestMesh(t)
	defer tm2.ctrl.Finish()
	defer tm2.Close()

	var txs2 []*types.Transaction
	rewards2 := make(map[types.Address]uint64)
	// use hare output to advance state to finalLyr
	for i := gLyr.Add(1); !i.After(finalLyr); i = i.Add(1) {
		blockIDs := copyLayer(t, tm1.Mesh, tm2.Mesh, tm2.mockATXDB, i)
		oldVerified := i.Sub(2)
		if oldVerified.Before(gLyr) {
			oldVerified = gLyr
		}
		tm2.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(oldVerified, i.Sub(1), false).Times(1)
		tm2.mockFetch.EXPECT().GetBlocks(gomock.Any(), blockIDs).Times(1)
		if i.Sub(1).After(gLyr) {
			tm2.mockState.EXPECT().ApplyLayer(i.Sub(1), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
					txs2 = append(txs2, txs...)
					for addr, reward := range rewards {
						rewards2[addr] += reward
					}
					return nil, nil
				})
			tm2.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
			tm2.mockState.EXPECT().ValidateAndAddTxToPool(gomock.Any()).Return(nil).AnyTimes()
		}
		tm2.HandleValidatedLayer(context.TODO(), i, blockIDs)
	}

	// two meshes should have the same state
	assert.Equal(t, txs1, txs2)
	assert.Equal(t, rewards1, rewards2)
	verified := finalLyr.Sub(1)
	assert.NotEqual(t, types.EmptyLayerHash, tm1.GetAggregatedLayerHash(verified))
	assert.Equal(t, tm1.GetAggregatedLayerHash(verified), tm2.GetAggregatedLayerHash(verified))
}

// test state is the same after same result received from hare.
func TestMesh_updateStateWithLayer_SameInputFromHare(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()
	createMeshFromSyncing(t, finalLyr, tm, tm.mockATXDB)

	aggHash := tm.GetAggregatedLayerHash(finalLyr.Sub(1))
	require.NotEqual(t, types.EmptyLayerHash, aggHash)

	// then hare outputs the same result
	lyr, err := tm.GetLayer(finalLyr)
	require.NoError(t, err)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), finalLyr).Return(finalLyr.Sub(1), finalLyr.Sub(1), false).Times(1)
	tm.mockFetch.EXPECT().GetBlocks(gomock.Any(), lyr.BlocksIDs()).Times(1)
	tm.HandleValidatedLayer(context.TODO(), finalLyr, lyr.BlocksIDs())

	// aggregated hash should be unchanged
	require.Equal(t, aggHash, tm.GetAggregatedLayerHash(finalLyr.Sub(1)))
}

// test state is the same after same result received from syncing with peers.
func TestMesh_updateStateWithLayer_SameInputFromSyncing(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()
	createMeshFromSyncing(t, finalLyr, tm, tm.mockATXDB)

	aggHash := tm.GetAggregatedLayerHash(finalLyr.Sub(1))
	require.NotEqual(t, types.EmptyLayerHash, aggHash)

	// sync the last layer from peers
	lyr, err := tm.GetLayer(finalLyr)
	require.NoError(t, err)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), finalLyr).Return(finalLyr.Sub(1), finalLyr.Sub(1), false).Times(1)
	tm.ValidateLayer(context.TODO(), lyr)

	// aggregated hash should be unchanged
	require.Equal(t, aggHash, tm.GetAggregatedLayerHash(finalLyr.Sub(1)))
}

func TestMesh_updateStateWithLayer_AdvanceInOrder(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	tm1 := createTestMesh(t)
	defer tm1.ctrl.Finish()
	defer tm1.Close()
	txs1, rewards1 := createMeshFromSyncing(t, finalLyr, tm1, tm1.mockATXDB)

	tm2 := createTestMesh(t)
	defer tm2.ctrl.Finish()
	defer tm2.Close()

	// use hare output to advance state to finalLyr-2
	var (
		newVerified types.LayerID
		txs2        []*types.Transaction
		rewards2    = make(map[types.Address]uint64)
	)
	tm2.mockState.EXPECT().ValidateAndAddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	for i := gLyr.Add(1); i.Before(finalLyr.Sub(1)); i = i.Add(1) {
		blockIDs := copyLayer(t, tm1.Mesh, tm2.Mesh, tm2.mockATXDB, i)
		newVerified = i.Sub(1)
		oldVerified := i.Sub(2)
		if oldVerified.Before(gLyr) {
			oldVerified = gLyr
		}
		tm2.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(oldVerified, newVerified, false).Times(1)
		tm2.mockFetch.EXPECT().GetBlocks(gomock.Any(), blockIDs).Times(1)
		if newVerified.After(gLyr) {
			tm2.mockState.EXPECT().ApplyLayer(newVerified, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
					txs2 = append(txs2, txs...)
					for addr, reward := range rewards {
						rewards2[addr] += reward
					}
					return nil, nil
				}).Times(1)
			tm2.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
		}
		tm2.HandleValidatedLayer(context.TODO(), i, blockIDs)
	}
	// tm1 is at finalLyr, tm2 is at finalLyr-2
	require.Equal(t, finalLyr, tm1.ProcessedLayer())
	require.Equal(t, finalLyr.Sub(2), tm2.ProcessedLayer())
	require.Equal(t, tm1.GetLayerHash(finalLyr.Sub(2)), tm2.GetLayerHash(finalLyr.Sub(2)))

	finalMinus1BlockIds := copyLayer(t, tm1.Mesh, tm2.Mesh, tm2.mockATXDB, finalLyr.Sub(1))
	finalBlockIds := copyLayer(t, tm1.Mesh, tm2.Mesh, tm2.mockATXDB, finalLyr)

	// now advance s2 to finalLyr
	require.Equal(t, finalLyr.Sub(3), newVerified)
	tm2.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), finalLyr).Return(newVerified, newVerified, false).Times(1)
	tm2.mockFetch.EXPECT().GetBlocks(gomock.Any(), finalBlockIds).Times(1)
	tm2.HandleValidatedLayer(context.TODO(), finalLyr, finalBlockIds)
	require.Equal(t, finalLyr.Sub(2), tm2.ProcessedLayer())

	// advancing tm2 to finalLyr-1 should bring tm2 to finalLyr
	tm2.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), finalLyr.Sub(1)).Return(newVerified, finalLyr.Sub(1), false).Times(1)
	tm2.mockFetch.EXPECT().GetBlocks(gomock.Any(), finalMinus1BlockIds).Times(1)
	for i := newVerified.Add(1); i.Before(finalLyr); i = i.Add(1) {
		tm2.mockState.EXPECT().ApplyLayer(i, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
				txs2 = append(txs2, txs...)
				for addr, reward := range rewards {
					rewards2[addr] += reward
				}
				return nil, nil
			}).Times(1)
		tm2.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	}
	tm2.HandleValidatedLayer(context.TODO(), finalLyr.Sub(1), finalMinus1BlockIds)
	assert.Equal(t, finalLyr, tm2.ProcessedLayer())

	// both mesh should be at the same state
	assert.Equal(t, tm1.GetLayerHash(finalLyr.Sub(1)), tm2.GetLayerHash(finalLyr.Sub(1)))
	assert.Equal(t, txs1, txs2)
	assert.Equal(t, rewards1, rewards2)
}

func copyLayer(t *testing.T, srcMesh, dstMesh *Mesh, dstAtxDb *AtxDbMock, id types.LayerID) []types.BlockID {
	l, err := srcMesh.GetLayer(id)
	assert.NoError(t, err)
	var blockIds []types.BlockID
	for _, b := range l.Blocks() {
		if b.ID() == types.GenesisBlockID {
			continue
		}
		txs, missing := srcMesh.GetTransactions(b.TxIDs)
		require.Empty(t, missing)
		for _, tx := range txs {
			dstMesh.txPool.Put(tx.ID(), tx)
		}
		atx, err := srcMesh.GetFullAtx(b.AtxID)
		assert.NoError(t, err)
		dstAtxDb.AddAtx(atx.ID(), atx)
		assert.NoError(t, dstMesh.AddProposalWithTxs(context.TODO(), (*types.Proposal)(b)))
		assert.NoError(t, dstMesh.SaveContextualValidity(b.ID(), id, true))
		blockIds = append(blockIds, b.ID())
	}
	return blockIds
}

func TestMesh_calculateActualRewards(t *testing.T) {
	reward, remainder := calculateActualRewards(types.NewLayerID(1), 10000, 10)
	assert.Equal(t, uint64(1000), reward)
	assert.Equal(t, uint64(0), remainder)
}

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, nipost *types.NIPost) *types.ActivationTx {
	nipostChallenge := types.NIPostChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipostChallenge, coinbase, nipost, 0, nil)
}
