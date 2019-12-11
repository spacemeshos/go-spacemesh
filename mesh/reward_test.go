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
	TotalReward int64
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

func (MockMapState) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error) {
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
	atx := types.NewActivationTx(types.NodeId{"1", []byte("bbbbb")}, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block1.ATXID = atx.Id()
	totalFee += addTransactionsWithFee(t, layers.MeshDB, block1, 15, 7)

	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	coinbase2 := types.HexToAddress("0xbbb")
	atx = types.NewActivationTx(types.NodeId{"2", []byte("bbbbb")}, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block2.ATXID = atx.Id()
	totalFee += addTransactionsWithFee(t, layers.MeshDB, block2, 13, rand.Int63n(100))

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	coinbase3 := types.HexToAddress("0xccc")
	atx = types.NewActivationTx(types.NodeId{"3", []byte("bbbbb")}, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block3.ATXID = atx.Id()
	totalFee += addTransactionsWithFee(t, layers.MeshDB, block3, 17, rand.Int63n(100))

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	coinbase4 := types.HexToAddress("0xddd")
	atx = types.NewActivationTx(types.NodeId{"4", []byte("bbbbb")}, coinbase4, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block4.ATXID = atx.Id()
	totalFee += addTransactionsWithFee(t, layers.MeshDB, block4, 16, rand.Int63n(100))

	log.Info("total fees : %v", totalFee)
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)

	params := NewTestRewardParams()

	layers.AccumulateRewards(1, params)
	totalRewardsCost := totalFee + params.BaseReward.Int64()
	remainder := totalRewardsCost % 4

	assert.Equal(t, totalRewardsCost, s.TotalReward+remainder)

}

func NewTestRewardParams() Config {
	return Config{
		BaseReward: big.NewInt(5000),
	}
}

func createLayer(t testing.TB, mesh *Mesh, id types.LayerID, numOfBlocks, maxTransactions int, atxdb *AtxDbMock) (totalRewards int64) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(id, []byte(rand.RandString(8)))
		nodeid := types.NodeId{strconv.Itoa(i), []byte("bbbbb")}
		coinbase := types.HexToAddress(nodeid.Key)
		atx := types.NewActivationTx(nodeid, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
		atxdb.AddAtx(atx.Id(), atx)
		block1.ATXID = atx.Id()

		totalRewards += addTransactionsWithFee(t, mesh.MeshDB, block1, rand.Intn(maxTransactions), rand.Int63n(100))
		block1.CalcAndSetId()
		err := mesh.AddBlock(block1)
		assert.NoError(t, err)
	}
	return totalRewards
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
		reward := createLayer(t, layers, types.LayerID(i), numofBlocks, maxTxs, atxdb)
		// rewards are applied to layers in the past according to the reward maturity param
		if i == 3 {
			l3Rewards = reward
			log.Info("reward %v", l3Rewards)
		}
	}

	l4, err := layers.GetLayer(4)
	assert.NoError(t, err)
	layers.ValidateLayer(l4)
	//since there can be a difference of up to x lerners where x is the number of blocks due to round up of penalties when distributed among all blocks
	totalPayout := l3Rewards + ConfigTst().BaseReward.Int64()
	assert.True(t, totalPayout-s.TotalReward < int64(numofBlocks), " rewards : %v, total %v blocks %v", totalPayout, s.TotalReward, int64(numofBlocks))
}

type meshValidatorBatchMock struct {
	batchSize types.LayerID
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

func (m *meshValidatorBatchMock) HandleLateBlock(bl *types.Block) {}

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
		reward := createLayer(t, mesh, types.LayerID(i), numOfBlocks, maxTxs, atxDb)
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
	reward := calculateActualRewards(big.NewInt(10000), big.NewInt(10))
	assert.Equal(t, int64(1000), reward.Int64())
}
