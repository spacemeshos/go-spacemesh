package mesh

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"math/big"
	"strconv"
	"testing"
)

type MockMapState struct {
	Rewards     map[types.Address]*big.Int
	Txs         []*types.Transaction
	TotalReward int64
}

func (s MockMapState) LoadState(layer types.LayerID) error {
	panic("implement me")
}

func (MockMapState) GetStateRoot() types.Hash32 {
	return [32]byte{}
}

func (MockMapState) ValidateNonceAndBalance(transaction *types.Transaction) error {
	panic("implement me")
}

func (MockMapState) GetLayerApplied(txId types.TransactionId) *types.LayerID {
	panic("implement me")
}

func (MockMapState) ValidateSignature(signed types.Signed) (types.Address, error) {
	return types.Address{}, nil
}

func (s *MockMapState) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error) {
	s.Txs = append(s.Txs, txs...)
	return 0, nil
}

func (s *MockMapState) ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int) {
	for _, minerId := range miners {
		s.Rewards[minerId] = reward
		s.TotalReward += reward.Int64()
	}
}

func (s *MockMapState) AddressExists(addr types.Address) bool {
	return true
}

func ConfigTst() Config {
	return Config{
		BaseReward: big.NewInt(5000),
	}
}

func getMeshWithMapState(id string, s TxProcessor) (*Mesh, *AtxDbMock) {
	atxDb := &AtxDbMock{
		db:     make(map[types.AtxId]*types.ActivationTx),
		nipsts: make(map[types.AtxId]*types.NIPST),
	}

	lg := log.New(id, "", "")
	mshdb := NewMemMeshDB(lg)
	mshdb.contextualValidity = &ContextualValidityMock{}
	return NewMesh(mshdb, atxDb, ConfigTst(), &MeshValidatorMock{}, &MockTxMemPool{}, &MockAtxMemPool{}, s, lg), atxDb
}

func addTransactionsWithFee(t testing.TB, mesh *MeshDB, bl *types.Block, numOfTxs int, fee int64) int64 {
	var totalFee int64
	var txs []*types.Transaction
	for i := 0; i < numOfTxs; i++ {
		//log.Info("adding tx with fee %v nonce %v", fee, i)
		tx, err := NewSignedTx(1, types.HexToAddress("1"), 10, 100, uint64(fee), signing.NewEdSigner())
		assert.NoError(t, err)
		bl.TxIds = append(bl.TxIds, tx.Id())
		totalFee += fee
		txs = append(txs, tx)
	}
	err := mesh.writeTransactions(0, txs)
	assert.NoError(t, err)
	return totalFee
}

func TestMesh_AccumulateRewards_happyFlow(t *testing.T) {
	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	layers, atxdb := getMeshWithMapState("t1", s)
	defer layers.Close()

	var totalFee int64
	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	coinbase1 := types.HexToAddress("0xaaa")
	atx := types.NewActivationTxForTests(types.NodeId{"1", []byte("bbbbb")}, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, coinbase1, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block1.ATXID = atx.Id()
	totalFee += addTransactionsWithFee(t, layers.MeshDB, block1, 15, 7)

	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	coinbase2 := types.HexToAddress("0xbbb")
	atx = types.NewActivationTxForTests(types.NodeId{"2", []byte("bbbbb")}, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, coinbase2, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block2.ATXID = atx.Id()
	totalFee += addTransactionsWithFee(t, layers.MeshDB, block2, 13, rand.Int63n(100))

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	coinbase3 := types.HexToAddress("0xccc")
	atx = types.NewActivationTxForTests(types.NodeId{"3", []byte("bbbbb")}, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, coinbase3, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block3.ATXID = atx.Id()
	totalFee += addTransactionsWithFee(t, layers.MeshDB, block3, 17, rand.Int63n(100))

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	coinbase4 := types.HexToAddress("0xddd")
	atx = types.NewActivationTxForTests(types.NodeId{"4", []byte("bbbbb")}, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, coinbase4, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block4.ATXID = atx.Id()
	totalFee += addTransactionsWithFee(t, layers.MeshDB, block4, 16, rand.Int63n(100))

	log.Info("total fees : %v", totalFee)
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)

	params := NewTestRewardParams()

	l, err := layers.GetLayer(1)
	assert.NoError(t, err)
	layers.AccumulateRewards(l, params)
	totalRewardsCost := totalFee + params.BaseReward.Int64()
	remainder := totalRewardsCost % 4

	assert.Equal(t, totalRewardsCost, s.TotalReward+remainder)

}

func NewTestRewardParams() Config {
	return Config{
		BaseReward: big.NewInt(5000),
	}
}

func createLayer(t testing.TB, mesh *Mesh, id types.LayerID, numOfBlocks, maxTransactions int, atxdb *AtxDbMock) (totalRewards int64, blocks []*types.Block) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(id, []byte(rand.RandString(8)))
		nodeid := types.NodeId{strconv.Itoa(i), []byte("bbbbb")}
		coinbase := types.HexToAddress(nodeid.Key)
		atx := types.NewActivationTxForTests(nodeid, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, coinbase, 10, []types.BlockID{}, &types.NIPST{})
		atxdb.AddAtx(atx.Id(), atx)
		block1.ATXID = atx.Id()

		totalRewards += addTransactionsWithFee(t, mesh.MeshDB, block1, rand.Intn(maxTransactions), rand.Int63n(100))
		block1.Initialize()
		err := mesh.AddBlock(block1)
		assert.NoError(t, err)
		blocks = append(blocks, block1)
	}
	return totalRewards, blocks
}

func TestMesh_integration(t *testing.T) {
	numofLayers := 10
	numofBlocks := 10
	maxTxs := 20

	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	layers, atxdb := getMeshWithMapState("t1", s)
	defer layers.Close()

	var l3Rewards int64
	for i := 0; i < numofLayers; i++ {
		reward, _ := createLayer(t, layers, types.LayerID(i), numofBlocks, maxTxs, atxdb)
		// rewards are applied to layers in the past according to the reward maturity param
		if i == 3 {
			l3Rewards = reward
			log.Info("reward %v", l3Rewards)
		}

		l, err := layers.GetLayer(types.LayerID(i))
		assert.NoError(t, err)
		layers.ValidateLayer(l)
	}
	//since there can be a difference of up to x lerners where x is the number of blocks due to round up of penalties when distributed among all blocks
	totalPayout := l3Rewards + ConfigTst().BaseReward.Int64()
	assert.True(t, totalPayout-s.TotalReward < int64(numofBlocks), " rewards : %v, total %v blocks %v", totalPayout, s.TotalReward, int64(numofBlocks))
}

func TestMesh_updateStateWithLayer(t *testing.T) {
	// test states are the same when one input is from tortoise and the other from hare
	// test state is the same if receiving result from tortoise after same result from hare received
	// test state is the same after late block
	// test panic after block from hare was not found in mesh
	// test that state does not advance when layer x +2 is received before layer x+1, and then test that all layers are pushed

	numofLayers := 10
	numofBlocks := 10
	maxTxs := 20

	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh, atxdb := getMeshWithMapState("t1", s)
	defer mesh.Close()

	for i := 0; i < numofLayers; i++ {
		createLayer(t, mesh, types.LayerID(i), numofBlocks, maxTxs, atxdb)
		l, err := mesh.GetLayer(types.LayerID(i))
		assert.NoError(t, err)
		mesh.ValidateLayer(l)
	}

	s2 := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	meshB, atxdbB := getMeshWithMapState("t2", s2)

	// this sohuld be played until numofLayers -1 if we want to compare states
	for i := 0; i < numofLayers-1; i++ {
		blockIds := copyLayer(t, mesh, meshB, atxdbB, types.LayerID(i))
		meshB.HandleValidatedLayer(types.LayerID(i), blockIds)
	}
	// test states are the same when one input is from tortoise and the other from hare
	assert.Equal(t, s.Txs, s2.Txs)

	for i := 0; i < numofLayers; i++ {
		l, err := mesh.GetLayer(types.LayerID(i))
		assert.NoError(t, err)
		meshB.ValidateLayer(l)
	}
	// test state is the same if receiving result from tortoise after same result from hare received
	assert.ObjectsAreEqualValues(s.Txs, s2.Txs)

	// test state is the same after late block
	layer4, err := mesh.GetLayer(4)
	assert.NoError(t, err)

	blk := layer4.Blocks()[0]
	meshB.HandleLateBlock(blk)
	assert.Equal(t, s.Txs, s2.Txs)

	// test that state does not advance when layer x +2 is received before layer x+1, and then test that all layers are pushed
	s3 := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	meshC, atxdbC := getMeshWithMapState("t3", s3)

	// this should be played until numofLayers -1 if we want to compare states
	for i := 0; i < numofLayers-3; i++ {
		blockIds := copyLayer(t, mesh, meshC, atxdbC, types.LayerID(i))
		meshC.HandleValidatedLayer(types.LayerID(i), blockIds)
	}
	s3Len := len(s3.Txs)
	blockIds := copyLayer(t, mesh, meshC, atxdbC, types.LayerID(numofLayers-2))
	meshC.HandleValidatedLayer(types.LayerID(numofLayers-2), blockIds)
	assert.Equal(t, s3Len, len(s3.Txs))

	blockIds = copyLayer(t, mesh, meshC, atxdbC, types.LayerID(numofLayers-3))
	meshC.HandleValidatedLayer(types.LayerID(numofLayers-3), blockIds)
	assert.Equal(t, s.Txs, s3.Txs)
}

func copyLayer(t *testing.T, srcMesh, dstMesh *Mesh, dstAtxDb *AtxDbMock, id types.LayerID) []types.BlockID {
	l, err := srcMesh.GetLayer(types.LayerID(id))
	assert.NoError(t, err)
	var blockIds []types.BlockID
	for _, b := range l.Blocks() {
		txs := srcMesh.getTxs(b.TxIds, l)
		atx, err := srcMesh.GetFullAtx(b.ATXID)
		assert.NoError(t, err)
		dstAtxDb.AddAtx(atx.Id(), atx)
		err = dstMesh.AddBlockWithTxs(b, txs, []*types.ActivationTx{})
		assert.NoError(t, err)
		blockIds = append(blockIds, b.Id())
	}
	return blockIds
}

func Test_HandleValidatedLayer_panic(t *testing.T) {
	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh, _ := getMeshWithMapState("t1", s)
	defer mesh.Close()
	block1 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block1.Initialize()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()

	mesh.HandleValidatedLayer(1, []types.BlockID{block1.Id()})
}

type meshValidatorBatchMock struct {
	batchSize types.LayerID
}

func (m *meshValidatorBatchMock) LatestComplete() types.LayerID {
	panic("implement me")
}

func (m *meshValidatorBatchMock) HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID) {
	layerId := layer.Index()
	if layerId == 0 {
		return 0, 0
	}
	if layerId%m.batchSize == 0 {
		return layerId - m.batchSize, layerId
	}
	prevPBase := layerId - layerId%m.batchSize
	return prevPBase, prevPBase
}

func (m *meshValidatorBatchMock) HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID) {
	return bl.Layer() - 1, bl.Layer()
}

func (m *meshValidatorBatchMock) PersistTortoise() error {
	return nil
}

func TestMesh_AccumulateRewards(t *testing.T) {
	numOfLayers := 10
	numOfBlocks := 10
	maxTxs := 20
	batchSize := 6

	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh, atxDb := getMeshWithMapState("t1", s)
	defer mesh.Close()

	mesh.MeshValidator = &meshValidatorBatchMock{batchSize: types.LayerID(batchSize)}

	var firstLayerRewards int64
	for i := 0; i < numOfLayers; i++ {
		reward, _ := createLayer(t, mesh, types.LayerID(i), numOfBlocks, maxTxs, atxDb)
		if i == 0 {
			firstLayerRewards = reward
			log.Info("reward %v", firstLayerRewards)
		}
	}

	oldTotal := s.TotalReward
	l4, err := mesh.GetLayer(4)
	assert.NoError(t, err)
	// Test negative case
	mesh.ValidateLayer(l4)
	assert.Equal(t, oldTotal, s.TotalReward)

	l5, err := mesh.GetLayer(5)
	assert.NoError(t, err)
	// Since batch size is 6, rewards will not be applied yet at this point
	mesh.ValidateLayer(l5)
	assert.Equal(t, oldTotal, s.TotalReward)

	l6, err := mesh.GetLayer(6)
	assert.NoError(t, err)
	// Rewards will be applied at this point
	mesh.ValidateLayer(l6)

	// When distributing rewards to blocks they are rounded down, so we have to allow up to numOfBlocks difference
	totalPayout := firstLayerRewards + ConfigTst().BaseReward.Int64()
	assert.True(t, totalPayout-s.TotalReward < int64(numOfBlocks),
		"diff=%v, totalPayout=%v, s.TotalReward=%v, numOfBlocks=%v",
		totalPayout-s.TotalReward-int64(numOfBlocks), totalPayout, s.TotalReward, int64(numOfBlocks))
}

func TestMesh_calcRewards(t *testing.T) {
	reward := calculateActualRewards(1, big.NewInt(10000), big.NewInt(10))
	assert.Equal(t, int64(1000), reward.Int64())
}
