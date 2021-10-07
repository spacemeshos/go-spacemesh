package mesh

import (
	"context"
	"math/big"
	"strconv"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var goldenATXID = types.ATXID(types.HexToHash32("77777"))

type MockMapSVM struct {
	Rewards     map[types.Address]*big.Int
	Txs         []*types.Transaction
	Pool        []*types.Transaction
	TotalReward *big.Int
}

func (s *MockMapSVM) GetAllAccounts() (*types.MultipleAccountsState, error) {
	panic("implement me")
}

func (s *MockMapSVM) ValidateAndAddTxToPool(tx *types.Transaction) error {
	s.Pool = append(s.Pool, tx)
	return nil
}

func (s MockMapSVM) LoadState(types.LayerID) error                       { panic("implement me") }
func (MockMapSVM) GetStateRoot() types.Hash32                            { return [32]byte{} }
func (MockMapSVM) ValidateNonceAndBalance(*types.Transaction) error      { panic("implement me") }
func (MockMapSVM) GetLayerApplied(types.TransactionID) *types.LayerID    { panic("implement me") }
func (MockMapSVM) GetLayerStateRoot(types.LayerID) (types.Hash32, error) { panic("implement me") }
func (MockMapSVM) GetBalance(types.Address) uint64                       { panic("implement me") }
func (MockMapSVM) GetNonce(types.Address) uint64                         { panic("implement me") }

func (s *MockMapSVM) ApplyTransactions(_ types.LayerID, txs []*types.Transaction) ([]*types.Transaction, error) {
	s.Txs = append(s.Txs, txs...)
	return []*types.Transaction{}, nil
}

func (s *MockMapSVM) ApplyLayer(l types.LayerID, transactions []*types.Transaction, rewards []types.AmountAndAddress) ([]*types.Transaction, error) {
	s.ApplyTransactions(l, transactions)
	for _, reward := range rewards {
		s.Rewards[reward.Address] = reward.Amount
		s.TotalReward.Add(s.TotalReward, reward.Amount)
	}
	return []*types.Transaction{}, nil
}

func (s *MockMapSVM) AddressExists(types.Address) bool {
	return true
}

func ConfigTst() Config {
	return Config{
		BaseReward: big.NewInt(5000),
	}
}

func getMeshWithMapState(tb testing.TB, id string, s svm) (*Mesh, *AtxDbMock) {
	atxDb := NewAtxDbMock()
	lg := logtest.New(tb)
	mshDb := NewMemMeshDB(lg)
	mshDb.contextualValidity = &ContextualValidityMock{}
	return NewMesh(mshDb, atxDb, ConfigTst(), &MeshValidatorMock{}, newMockTxMemPool(), s, lg), atxDb
}

func addTransactionsWithFee(t testing.TB, mesh *DB, bl *types.Block, numOfTxs int, fee int64) *big.Int {
	var totalFee int64
	var txs []*types.Transaction
	for i := 0; i < numOfTxs; i++ {
		tx, err := types.NewSignedTx(1, types.HexToAddress("1"), 10, 100, uint64(fee), signing.NewEdSigner())
		assert.NoError(t, err)
		bl.TxIDs = append(bl.TxIDs, tx.ID())
		totalFee += fee
		txs = append(txs, tx)
	}
	blk := &types.Block{}
	blk.LayerIndex = types.NewLayerID(0)
	err := mesh.writeTransactions(blk, txs...)
	assert.NoError(t, err)
	return big.NewInt(totalFee)
}

func init() {
	types.SetLayersPerEpoch(3)
}

type TestData struct {
}

func TestMesh_AccumulateRewards_happyFlow(t *testing.T) {
	s := emptyMapSVM()
	layers, atxDB := s.getMeshWithMapStateThenClose(t, "t1")

	type NewBlockData struct {
		coinbase string
		numTxs   int
		fee      int64
	}

	blockData := []NewBlockData{
		{"0xaaa", 15, 7},
		{"0xbbb", 13, rand.Int63n(100)},
		{"0xccc", 17, rand.Int63n(100)},
		{"0xddd", 16, rand.Int63n(100)},
	}

	totalFee := big.NewInt(0)
	blocks := []*types.Block{}

	for i, data := range blockData {
		key := strconv.Itoa(i)
		coinbase := types.HexToAddress(data.coinbase)
		nodeID := types.NodeID{Key: key, VRFPublicKey: []byte("bbbbb")}

		block := types.NewExistingBlock(types.NewLayerID(1), []byte(rand.String(8)), nil)
		atx := newActivationTx(nodeID, 0, *types.EmptyATXID, types.NewLayerID(1), 0, goldenATXID, coinbase, 10, []types.BlockID{}, &types.NIPost{})
		atxDB.AddAtx(atx.ID(), atx)
		block.ATXID = atx.ID()

		blocks = append(blocks, block)
		totalFee.Add(totalFee, addTransactionsWithFee(t, layers.DB, block, data.numTxs, data.fee))
	}

	for _, block := range blocks {
		_ = layers.AddBlock(block)
	}

	params := NewTestRewardParams()

	l, err := layers.GetLayer(types.NewLayerID(1))
	assert.NoError(t, err)
	validBlockTxs := layers.extractUniqueOrderedTransactions(l)
	rewards := layers.calculateRewards(l, validBlockTxs, params)
	layers.writeRewards(rewards)
	totalRewardsCost := big.NewInt(0)
	totalRewardsCost.Add(totalFee, params.BaseReward)
	remainder := big.NewInt(0)
	remainder.Mod(totalRewardsCost, big.NewInt(4))

	assert.Equal(t, totalRewardsCost, s.TotalReward.Add(s.TotalReward, remainder))

}

func NewTestRewardParams() Config {
	return Config{
		BaseReward: big.NewInt(5000),
	}
}

func createBlock(t testing.TB, mesh *Mesh, lyrID types.LayerID, nodeID types.NodeID, maxTransactions int, atxDB *AtxDbMock) (*types.Block, *big.Int) {
	blk := types.NewExistingBlock(lyrID, []byte(rand.String(8)), nil)
	coinbase := types.HexToAddress(nodeID.Key)
	atx := newActivationTx(nodeID, 0, goldenATXID, types.NewLayerID(1), 0, goldenATXID, coinbase, 10, []types.BlockID{}, &types.NIPost{})
	atxDB.AddAtx(atx.ID(), atx)
	blk.ATXID = atx.ID()
	reward := addTransactionsWithFee(t, mesh.DB, blk, rand.Intn(maxTransactions), rand.Int63n(100))
	blk.Initialize()
	err := mesh.AddBlock(blk)
	assert.NoError(t, err)
	return blk, reward
}

func createLayer(t testing.TB, mesh *Mesh, lyrID types.LayerID, numOfBlocks, maxTransactions int, atxDB *AtxDbMock) (totalRewards *big.Int, blocks []*types.Block) {
	totalRewards = big.NewInt(0)
	for i := 0; i < numOfBlocks; i++ {
		nodeID := types.NodeID{Key: strconv.Itoa(i), VRFPublicKey: []byte("bbbbb")}
		blk, reward := createBlock(t, mesh, lyrID, nodeID, maxTransactions, atxDB)
		blocks = append(blocks, blk)
		totalRewards.Add(totalRewards, reward)
	}
	return totalRewards, blocks
}

func TestMesh_integration(t *testing.T) {
	numOfLayers := 10
	numOfBlocks := 10
	maxTxs := 20

	s := emptyMapSVM()
	layers, atxDB := getMeshWithMapState(t, "t1", s)
	defer layers.Close()

	l3Rewards := big.NewInt(0)
	for i := 1; i <= numOfLayers; i++ {
		reward, _ := createLayer(t, layers, types.NewLayerID(uint32(i)), numOfBlocks, maxTxs, atxDB)
		// rewards are applied to layers in the past according to the reward maturity param
		if i == 3 {
			l3Rewards.Set(reward)
		}

		l, err := layers.GetLayer(types.NewLayerID(uint32(i)))
		assert.NoError(t, err)
		layers.ValidateLayer(context.TODO(), l)
	}
	// since there can be a difference of up to x lerners where x is the number of blocks due to round up of penalties when distributed among all blocks
	totalPayout := big.NewInt(0)
	totalPayout.Add(l3Rewards, ConfigTst().BaseReward)

	diff := big.NewInt(0)
	diff.Sub(totalPayout, s.TotalReward)
	assert.True(t, diff.Int64() < int64(numOfBlocks), " rewards : %v, total %v blocks %v", totalPayout, s.TotalReward, int64(numOfBlocks))
}

func createMeshFromSyncing(t *testing.T, finalLyr types.LayerID, msh *Mesh, atxDB *AtxDbMock) {
	numOfBlocks := 10
	maxTxs := 20
	gLyr := types.GetEffectiveGenesis()
	for i := types.NewLayerID(1); !i.After(finalLyr); i = i.Add(1) {
		if i.After(gLyr) {
			createLayer(t, msh, i, numOfBlocks, maxTxs, atxDB)
		}
		lyr, err := msh.GetLayer(i)
		require.NoError(t, err)
		msh.ValidateLayer(context.TODO(), lyr)
	}
}

func createMeshFromHareOutput(t *testing.T, finalLyr types.LayerID, msh *Mesh, atxDB *AtxDbMock) {
	numOfBlocks := 10
	maxTxs := 20
	gLyr := types.GetEffectiveGenesis()
	for i := types.NewLayerID(1); !i.After(finalLyr); i = i.Add(1) {
		if i.After(gLyr) {
			createLayer(t, msh, i, numOfBlocks, maxTxs, atxDB)
		}
		lyr, err := msh.GetLayer(i)
		require.NoError(t, err)
		msh.HandleValidatedLayer(context.TODO(), i, lyr.BlocksIDs())
	}
}

func emptyMapSVM() *MockMapSVM {
	return &MockMapSVM{
		Rewards:     make(map[types.Address]*big.Int),
		TotalReward: big.NewInt(0),
	}
}

func (s *MockMapSVM) getMeshWithMapStateThenClose(t *testing.T, id string) (*Mesh, *AtxDbMock) {
	msh, atxDB := getMeshWithMapState(t, id, s)
	t.Cleanup(func() {
		msh.Close()
	})
	return msh, atxDB
}

// test states are the same when one input is data polled from peers and the other from hare's output
func TestMesh_updateStateWithLayer_SyncingAndHareReachSameState(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	// s1 is the state where a node advance its state via syncing with peers
	s1 := emptyMapSVM()
	msh1, atxDB := s1.getMeshWithMapStateThenClose(t, "t1")
	createMeshFromSyncing(t, finalLyr, msh1, atxDB)

	// s2 is the state where the node advances its state via hare output
	s2 := emptyMapSVM()
	msh2, atxDB2 := s2.getMeshWithMapStateThenClose(t, "t2")

	// use hare output to advance state to finalLyr
	for i := gLyr.Add(1); !i.After(finalLyr); i = i.Add(1) {
		blockIds := copyLayer(t, msh1, msh2, atxDB2, i)
		msh2.HandleValidatedLayer(context.TODO(), i, blockIds)
	}

	// s1 (sync from peers) and s2 (advance via hare output) should have the same state
	require.Equal(t, s1.Txs, s2.Txs)
	require.Greater(t, len(s1.Txs), 0)
}

// test state is the same after same result received from hare
func TestMesh_updateStateWithLayer_SameInputFromHare(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	// s is the state where a node advance its state via syncing with peers
	s := emptyMapSVM()
	msh, atxDB := s.getMeshWithMapStateThenClose(t, "t1")
	createMeshFromSyncing(t, finalLyr, msh, atxDB)
	oldTxs := make([]*types.Transaction, len(s.Txs))
	copy(oldTxs, s.Txs)
	require.Greater(t, len(oldTxs), 0)

	// then hare outputs the same result
	lyr, err := msh.GetLayer(finalLyr)
	require.NoError(t, err)
	msh.HandleValidatedLayer(context.TODO(), finalLyr, lyr.BlocksIDs())

	// s2 state should be unchanged
	require.Equal(t, oldTxs, s.Txs)
}

// test state is the same after same result received from syncing with peers
func TestMesh_updateStateWithLayer_SameInputFromSyncing(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	// s is the state where a node advance its state via syncing with peers
	s := emptyMapSVM()
	msh, atxDB := s.getMeshWithMapStateThenClose(t, "t1")
	t.Cleanup(func() {
		msh.Close()
	})
	createMeshFromHareOutput(t, finalLyr, msh, atxDB)
	oldTxs := make([]*types.Transaction, len(s.Txs))
	copy(oldTxs, s.Txs)
	require.Greater(t, len(oldTxs), 0)

	// sync the last layer from peers
	lyr, err := msh.GetLayer(finalLyr)
	require.NoError(t, err)
	msh.ValidateLayer(context.TODO(), lyr)

	// s2 state should be unchanged
	require.Equal(t, oldTxs, s.Txs)
}

func TestMesh_updateStateWithLayer_LateBlock(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	// s is the state where a node advance its state via syncing with peers
	s := emptyMapSVM()
	msh, atxDB := s.getMeshWithMapStateThenClose(t, "t1")
	createMeshFromSyncing(t, finalLyr, msh, atxDB)
	oldTxs := make([]*types.Transaction, len(s.Txs))
	copy(oldTxs, s.Txs)
	require.Greater(t, len(oldTxs), 0)

	oldLyr, err := msh.GetLayer(finalLyr.Sub(4))
	require.NoError(t, err)

	blk := oldLyr.Blocks()[0]
	msh.HandleLateBlock(context.TODO(), blk)
	// a seen late block should not change the state
	require.Equal(t, oldTxs, s.Txs)

	// a not-before-seen late block should not change the state either
	nodeID := types.NodeID{Key: strconv.Itoa(999), VRFPublicKey: []byte("ccccc")}
	blk, _ = createBlock(t, msh, oldLyr.Index(), nodeID, 200, atxDB)
	msh.HandleLateBlock(context.TODO(), blk)
	// a late block we haven't seen should not the state
	require.Equal(t, oldTxs, s.Txs)
}

func TestMesh_updateStateWithLayer_AdvanceInOrder(t *testing.T) {
	gLyr := types.GetEffectiveGenesis()
	finalLyr := gLyr.Add(10)

	// s1 is the state where a node advance its state via syncing with peers
	s1 := emptyMapSVM()
	msh1, atxDB1 := s1.getMeshWithMapStateThenClose(t, "t1")
	createMeshFromSyncing(t, finalLyr, msh1, atxDB1)

	// s2 is the state where the node advances its state via hare output
	s2 := emptyMapSVM()
	msh2, atxDB2 := s2.getMeshWithMapStateThenClose(t, "t2")

	// use hare output to advance state to finalLyr-2
	for i := gLyr.Add(1); i.Before(finalLyr.Sub(1)); i = i.Add(1) {
		blockIds := copyLayer(t, msh1, msh2, atxDB2, i)
		msh2.HandleValidatedLayer(context.TODO(), i, blockIds)
	}
	// s1 is at finalLyr, s2 is at finalLyr-2
	require.NotEqual(t, s1.Txs, s2.Txs)

	finalMinus2Txs := make([]*types.Transaction, len(s2.Txs))
	copy(finalMinus2Txs, s2.Txs)
	require.Greater(t, len(finalMinus2Txs), 0)

	finalMinus1BlockIds := copyLayer(t, msh1, msh2, atxDB2, finalLyr.Sub(1))
	//copyLayer(t, msh1, msh2, atxDB2, finalLyr.Sub(1))
	finalBlockIds := copyLayer(t, msh1, msh2, atxDB2, finalLyr)

	// now advance s2 to finalLyr
	msh2.HandleValidatedLayer(context.TODO(), finalLyr, finalBlockIds)
	// s2 should be unchanged because finalLyr-1 has not been processed
	require.Equal(t, finalMinus2Txs, s2.Txs)

	// advancing s2 to finalLyr-1 should bring s2 to finalLyr
	msh2.HandleValidatedLayer(context.TODO(), finalLyr.Sub(1), finalMinus1BlockIds)
	// s2 should be the same as s1 (at finalLyr)
	require.Equal(t, s1.Txs, s2.Txs)
}

func copyLayer(t *testing.T, srcMesh, dstMesh *Mesh, dstAtxDb *AtxDbMock, id types.LayerID) []types.BlockID {
	l, err := srcMesh.GetLayer(id)
	assert.NoError(t, err)
	var blockIds []types.BlockID
	for _, b := range l.Blocks() {
		if b.ID() == GenesisBlock().ID() {
			continue
		}
		txs := srcMesh.getTxs(b.TxIDs, l.Index())
		for _, tx := range txs {
			dstMesh.txPool.Put(tx.ID(), tx)
		}
		atx, err := srcMesh.GetFullAtx(b.ATXID)
		assert.NoError(t, err)
		dstAtxDb.AddAtx(atx.ID(), atx)
		err = dstMesh.AddBlockWithTxs(context.TODO(), b)
		assert.NoError(t, err)
		blockIds = append(blockIds, b.ID())
	}
	return blockIds
}

type meshValidatorBatchMock struct {
	mesh           *Mesh
	batchSize      uint32
	processedLayer types.LayerID
	layerHash      types.Hash32
}

func (m *meshValidatorBatchMock) ValidateLayer(_ context.Context, lyr *types.Layer) {
	m.mesh.setProcessedLayer(lyr)
	layerID := lyr.Index()
	if layerID.Uint32() == 0 {
		return
	}
	if layerID.Uint32()%m.batchSize == 0 {
		m.mesh.pushLayersToState(context.TODO(), layerID.Sub(m.batchSize), layerID)
		return
	}
	prevPBase := layerID.Sub(layerID.Uint32() % m.batchSize)
	m.mesh.pushLayersToState(context.TODO(), prevPBase, prevPBase)
}

func TestMesh_AccumulateRewards(t *testing.T) {
	types.SetLayersPerEpoch(1)
	defer types.SetLayersPerEpoch(3)
	numOfLayers := 10
	numOfBlocks := 10
	maxTxs := 20
	batchSize := 6

	s := emptyMapSVM()
	mesh, atxDb := getMeshWithMapState(t, "t1", s)
	defer mesh.Close()

	mesh.Validator = &meshValidatorBatchMock{mesh: mesh, batchSize: uint32(batchSize)}

	firstLayerRewards := big.NewInt(0)
	for i := 0; i < numOfLayers; i++ {
		reward, _ := createLayer(t, mesh, types.NewLayerID(uint32(i)), numOfBlocks, maxTxs, atxDb)
		if i == 0 {
			firstLayerRewards.Set(reward)
		}
	}

	oldTotal := s.TotalReward
	l4, err := mesh.GetLayer(types.NewLayerID(4))
	assert.NoError(t, err)
	// Test negative case
	mesh.ValidateLayer(context.TODO(), l4)
	assert.Equal(t, oldTotal, s.TotalReward)

	l5, err := mesh.GetLayer(types.NewLayerID(5))
	assert.NoError(t, err)
	// Since batch size is 6, rewards will not be applied yet at this point
	mesh.ValidateLayer(context.TODO(), l5)
	assert.Equal(t, oldTotal, s.TotalReward)

	l6, err := mesh.GetLayer(types.NewLayerID(6))
	assert.NoError(t, err)
	// Rewards will be applied at this point
	mesh.ValidateLayer(context.TODO(), l6)

	// When distributing rewards to blocks they are rounded down, so we have to allow up to numOfBlocks difference
	totalPayout := big.NewInt(0)
	totalPayout.Add(firstLayerRewards, ConfigTst().BaseReward)
	diff := big.NewInt(0)
	diff.Sub(totalPayout, s.TotalReward)
	assert.True(t, diff.Int64() < int64(numOfBlocks),
		"diff=%v, totalPayout=%v, s.TotalReward=%v, numOfBlocks=%v",
		diff.Int64()-int64(numOfBlocks), totalPayout, s.TotalReward, int64(numOfBlocks))
}

func TestMesh_calcRewards(t *testing.T) {
	reward, remainder := calculateActualRewards(types.NewLayerID(1), big.NewInt(10000), big.NewInt(10))
	assert.Equal(t, big.NewInt(1000), reward)
	assert.Equal(t, big.NewInt(0), remainder)
}

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, activeSetSize uint32, view []types.BlockID,
	nipost *types.NIPost) *types.ActivationTx {

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
