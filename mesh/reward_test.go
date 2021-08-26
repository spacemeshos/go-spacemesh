package mesh

import (
	"context"
	"github.com/stretchr/testify/require"
	"math/big"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var goldenATXID = types.ATXID(types.HexToHash32("77777"))

type MockMapState struct {
	Rewards     map[types.Address]*big.Int
	Txs         []*types.Transaction
	Pool        []*types.Transaction
	TotalReward int64
}

func (s *MockMapState) GetAllAccounts() (*types.MultipleAccountsState, error) {
	panic("implement me")
}

func (s *MockMapState) ValidateAndAddTxToPool(tx *types.Transaction) error {
	s.Pool = append(s.Pool, tx)
	return nil
}

func (s MockMapState) LoadState(types.LayerID) error                       { panic("implement me") }
func (MockMapState) GetStateRoot() types.Hash32                            { return [32]byte{} }
func (MockMapState) ValidateNonceAndBalance(*types.Transaction) error      { panic("implement me") }
func (MockMapState) GetLayerApplied(types.TransactionID) *types.LayerID    { panic("implement me") }
func (MockMapState) GetLayerStateRoot(types.LayerID) (types.Hash32, error) { panic("implement me") }
func (MockMapState) GetBalance(types.Address) uint64                       { panic("implement me") }
func (MockMapState) GetNonce(types.Address) uint64                         { panic("implement me") }

func (s *MockMapState) ApplyTransactions(_ types.LayerID, txs []*types.Transaction) (int, error) {
	s.Txs = append(s.Txs, txs...)
	return 0, nil
}

func (s *MockMapState) ApplyRewards(_ types.LayerID, miners []types.Address, reward *big.Int) {
	for _, minerID := range miners {
		s.Rewards[minerID] = reward
		s.TotalReward += reward.Int64()
	}
}

func (s *MockMapState) AddressExists(types.Address) bool {
	return true
}

func ConfigTst() Config {
	return Config{
		BaseReward: big.NewInt(5000),
	}
}

func getMeshWithMapState(tb testing.TB, id string, s txProcessor) (*Mesh, *AtxDbMock) {
	atxDb := NewAtxDbMock()
	lg := logtest.New(tb)
	mshDb := NewMemMeshDB(lg)
	mshDb.contextualValidity = &ContextualValidityMock{}
	return NewMesh(mshDb, atxDb, ConfigTst(), &MeshValidatorMock{}, newMockTxMemPool(), s, lg), atxDb
}

func addTransactionsWithFee(t testing.TB, mesh *DB, bl *types.Block, numOfTxs int, fee int64) int64 {
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
	return totalFee
}

func init() {
	types.SetLayersPerEpoch(3)
}

func TestMesh_AccumulateRewards_happyFlow(t *testing.T) {
	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	layers, atxDB := getMeshWithMapState(t, "t1", s)
	defer layers.Close()

	var totalFee int64
	block1 := types.NewExistingBlock(types.NewLayerID(1), []byte(rand.String(8)), nil)

	coinbase1 := types.HexToAddress("0xaaa")
	atx := newActivationTx(types.NodeID{Key: "1", VRFPublicKey: []byte("bbbbb")}, 0, *types.EmptyATXID, types.NewLayerID(1), 0, goldenATXID, coinbase1, 10, []types.BlockID{}, &types.NIPost{})
	atxDB.AddAtx(atx.ID(), atx)
	block1.ATXID = atx.ID()
	totalFee += addTransactionsWithFee(t, layers.DB, block1, 15, 7)

	block2 := types.NewExistingBlock(types.NewLayerID(1), []byte(rand.String(8)), nil)

	coinbase2 := types.HexToAddress("0xbbb")
	atx = newActivationTx(types.NodeID{Key: "2", VRFPublicKey: []byte("bbbbb")}, 0, *types.EmptyATXID, types.NewLayerID(1), 0, goldenATXID, coinbase2, 10, []types.BlockID{}, &types.NIPost{})
	atxDB.AddAtx(atx.ID(), atx)
	block2.ATXID = atx.ID()
	totalFee += addTransactionsWithFee(t, layers.DB, block2, 13, rand.Int63n(100))

	block3 := types.NewExistingBlock(types.NewLayerID(1), []byte(rand.String(8)), nil)

	coinbase3 := types.HexToAddress("0xccc")
	atx = newActivationTx(types.NodeID{Key: "3", VRFPublicKey: []byte("bbbbb")}, 0, goldenATXID, types.NewLayerID(1), 0, goldenATXID, coinbase3, 10, []types.BlockID{}, &types.NIPost{})
	atxDB.AddAtx(atx.ID(), atx)
	block3.ATXID = atx.ID()
	totalFee += addTransactionsWithFee(t, layers.DB, block3, 17, rand.Int63n(100))

	block4 := types.NewExistingBlock(types.NewLayerID(1), []byte(rand.String(8)), nil)

	coinbase4 := types.HexToAddress("0xddd")
	atx = newActivationTx(types.NodeID{Key: "4", VRFPublicKey: []byte("bbbbb")}, 0, goldenATXID, types.NewLayerID(1), 0, goldenATXID, coinbase4, 10, []types.BlockID{}, &types.NIPost{})
	atxDB.AddAtx(atx.ID(), atx)
	block4.ATXID = atx.ID()
	totalFee += addTransactionsWithFee(t, layers.DB, block4, 16, rand.Int63n(100))

	_ = layers.AddBlock(block1)
	_ = layers.AddBlock(block2)
	_ = layers.AddBlock(block3)
	_ = layers.AddBlock(block4)

	params := NewTestRewardParams()

	l, err := layers.GetLayer(types.NewLayerID(1))
	assert.NoError(t, err)
	layers.accumulateRewards(l, params)
	totalRewardsCost := totalFee + params.BaseReward.Int64()
	remainder := totalRewardsCost % 4

	assert.Equal(t, totalRewardsCost, s.TotalReward+remainder)

}

func NewTestRewardParams() Config {
	return Config{
		BaseReward: big.NewInt(5000),
	}
}

func createLayer(t testing.TB, mesh *Mesh, id types.LayerID, numOfBlocks, maxTransactions int, atxDB *AtxDbMock) (totalRewards int64, blocks []*types.Block) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(id, []byte(rand.String(8)), nil)
		nodeID := types.NodeID{Key: strconv.Itoa(i), VRFPublicKey: []byte("bbbbb")}
		coinbase := types.HexToAddress(nodeID.Key)
		atx := newActivationTx(nodeID, 0, goldenATXID, types.NewLayerID(1), 0, goldenATXID, coinbase, 10, []types.BlockID{}, &types.NIPost{})
		atxDB.AddAtx(atx.ID(), atx)
		block1.ATXID = atx.ID()

		totalRewards += addTransactionsWithFee(t, mesh.DB, block1, rand.Intn(maxTransactions), rand.Int63n(100))
		block1.Initialize()
		err := mesh.AddBlock(block1)
		assert.NoError(t, err)
		blocks = append(blocks, block1)
	}
	return totalRewards, blocks
}

func TestMesh_integration(t *testing.T) {
	numOfLayers := 10
	numOfBlocks := 10
	maxTxs := 20

	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	layers, atxDB := getMeshWithMapState(t, "t1", s)
	defer layers.Close()

	var l3Rewards int64
	for i := 1; i <= numOfLayers; i++ {
		reward, _ := createLayer(t, layers, types.NewLayerID(uint32(i)), numOfBlocks, maxTxs, atxDB)
		// rewards are applied to layers in the past according to the reward maturity param
		if i == 3 {
			l3Rewards = reward
		}

		l, err := layers.GetLayer(types.NewLayerID(uint32(i)))
		assert.NoError(t, err)
		layers.ValidateLayer(context.TODO(), l)
	}
	// since there can be a difference of up to x lerners where x is the number of blocks due to round up of penalties when distributed among all blocks
	totalPayout := l3Rewards + ConfigTst().BaseReward.Int64()
	assert.True(t, totalPayout-s.TotalReward < int64(numOfBlocks), " rewards : %v, total %v blocks %v", totalPayout, s.TotalReward, int64(numOfBlocks))
}

func TestMesh_updateStateWithLayer(t *testing.T) {
	// test states are the same when one input is from tortoise and the other from hare
	// test state is the same if receiving result from tortoise after same result from hare received
	// test state is the same after late block
	// test panic after block from hare was not found in mesh
	// test that state does not advance when layer x+2 is received before layer x+1, and then test that all layers are pushed
	numOfLayers := 10
	numOfBlocks := 10
	maxTxs := 20

	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh, atxDB := getMeshWithMapState(t, "t1", s)
	defer mesh.Close()

	startLayer := types.GetEffectiveGenesis()

	for i := 0; i < numOfLayers; i++ {
		layerID := startLayer.Add(uint32(i))
		createLayer(t, mesh, layerID, numOfBlocks, maxTxs, atxDB)
		l, err := mesh.GetLayer(layerID)
		require.NoError(t, err)
		mesh.ValidateLayer(context.TODO(), l)
	}

	s2 := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh2, atxDB2 := getMeshWithMapState(t, "t2", s2)

	// this should be played until numOfLayers-1 if we want to compare states
	for i := 0; i < numOfLayers-1; i++ {
		layerID := startLayer.Add(uint32(i))
		blockIds := copyLayer(t, mesh, mesh2, atxDB2, layerID)
		mesh2.HandleValidatedLayer(context.TODO(), layerID, blockIds)
	}

	// test states are the same when one input is from tortoise and the other from hare
	require.NotEqual(t, s.Txs, s2.Txs)

	layerIDFinal := startLayer.Add(uint32(numOfLayers - 1))
	copyLayer(t, mesh, mesh2, atxDB2, layerIDFinal)
	l, err := mesh.GetLayer(layerIDFinal)
	require.NoError(t, err)
	mesh2.ValidateLayer(context.TODO(), l)

	// test states are the same when one input is from tortoise and the other from hare
	require.Equal(t, s.Txs, s2.Txs)

	// test state is the same if receiving result from tortoise after same result from hare received
	assert.ObjectsAreEqualValues(s.Txs, s2.Txs)

	// test state is the same after late block
	layer4, err := mesh.GetLayer(startLayer.Add(4))
	require.NoError(t, err)

	blk := layer4.Blocks()[0]
	mesh2.HandleLateBlock(context.TODO(), blk)
	require.Equal(t, s.Txs, s2.Txs)

	// test that state does not advance when layer x+2 is received before layer x+1,
	// and then test that all layers are pushed
	s3 := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh3, atxDB3 := getMeshWithMapState(t, "t3", s3)

	// this should be played until numOfLayers-1 if we want to compare states
	for i := 0; i < numOfLayers-3; i++ {
		layerID := startLayer.Add(uint32(i))
		blockIds := copyLayer(t, mesh, mesh3, atxDB3, layerID)
		mesh3.HandleValidatedLayer(context.TODO(), layerID, blockIds)
	}
	s3Len := len(s3.Txs)
	blockIds := copyLayer(t, mesh, mesh3, atxDB3, startLayer.Add(uint32(numOfLayers)-2))
	// layer arrived early, gets queued for state processing (no new txs processed)
	mesh3.HandleValidatedLayer(context.TODO(), startLayer.Add(uint32(numOfLayers)-2), blockIds)
	require.Equal(t, s3Len, len(s3.Txs))

	blockIds = copyLayer(t, mesh, mesh3, atxDB3, startLayer.Add(uint32(numOfLayers)-3))
	// this is the next layer, it's processed along with layer n-2
	mesh3.HandleValidatedLayer(context.TODO(), startLayer.Add(uint32(numOfLayers)-3), blockIds)
	require.Greater(t, len(s3.Txs), s3Len)
	s3Len = len(s3.Txs)

	// re-validate layer n-2: no change, it should already have been processed
	mesh3.HandleValidatedLayer(context.TODO(), startLayer.Add(uint32(numOfLayers)-2), blockIds)
	require.Equal(t, s3Len, len(s3.Txs))

	// validate layer n-1
	blockIds = copyLayer(t, mesh, mesh3, atxDB3, startLayer.Add(uint32(numOfLayers)-1))
	mesh3.HandleValidatedLayer(context.TODO(), startLayer.Add(uint32(numOfLayers)-1), blockIds)
	require.Greater(t, len(s3.Txs), s3Len) // expect txs from layer n-1 to have been applied

	// now everything should have been applied
	blockIds = copyLayer(t, mesh, mesh3, atxDB3, startLayer.Add(uint32(numOfLayers)-2))
	mesh3.HandleValidatedLayer(context.TODO(), startLayer.Add(uint32(numOfLayers)-2), blockIds)
	require.Equal(t, s.Txs, s3.Txs)
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

	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh, atxDb := getMeshWithMapState(t, "t1", s)
	defer mesh.Close()

	mesh.Validator = &meshValidatorBatchMock{mesh: mesh, batchSize: uint32(batchSize)}

	var firstLayerRewards int64
	for i := 0; i < numOfLayers; i++ {
		reward, _ := createLayer(t, mesh, types.NewLayerID(uint32(i)), numOfBlocks, maxTxs, atxDb)
		if i == 0 {
			firstLayerRewards = reward
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
	totalPayout := firstLayerRewards + ConfigTst().BaseReward.Int64()
	assert.True(t, totalPayout-s.TotalReward < int64(numOfBlocks),
		"diff=%v, totalPayout=%v, s.TotalReward=%v, numOfBlocks=%v",
		totalPayout-s.TotalReward-int64(numOfBlocks), totalPayout, s.TotalReward, int64(numOfBlocks))
}

func TestMesh_calcRewards(t *testing.T) {
	reward, remainder := calculateActualRewards(types.NewLayerID(1), big.NewInt(10000), big.NewInt(10))
	assert.Equal(t, int64(1000), reward.Int64())
	assert.Equal(t, int64(0), remainder.Int64())
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
