package mesh

import (
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"math/big"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

type MockMapState struct {
	Rewards map[string]*big.Int
	Total   int64
}

func (MockMapState) ApplyTransactions(layer LayerID, txs Transactions) (uint32, error) {
	return 0, nil
}

func (s *MockMapState) ApplyRewards(layer LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {
	for minerId := range miners {
		if _, has := underQuota[minerId]; !has {
			s.Rewards[minerId] = bonusReward
		} else {
			s.Rewards[minerId] = diminishedReward
		}
		s.Total += s.Rewards[minerId].Int64()
	}

}

func ConfigTst() RewardConfig {
	return RewardConfig{
		big.NewInt(10),
		big.NewInt(5000),
		big.NewInt(15),
		15,
		5,
	}
}

func getMeshWithMapState(id string, s StateUpdater) *Mesh {

	//time := time.Now()
	bdb := database.NewMemDatabase()
	ldb := database.NewMemDatabase()
	cdb := database.NewMemDatabase()
	layers := NewMesh(ldb, bdb, cdb, ConfigTst(), &MeshValidatorMock{}, s, log.New(id, "", ""))
	return layers
}

func addTransactions(bl *Block, numOfTxs int) int64 {
	var totalRewards int64
	for i := 0; i < numOfTxs; i++ {
		gasPrice := rand.Int63n(100)
		addr := rand.Int63n(10000)
		//log.Info("adding tx with gas price %v nonce %v", gasPrice, i)
		bl.Txs = append(bl.Txs, *NewSerializableTransaction(uint64(i), address.HexToAddress("1"),
			address.HexToAddress(strconv.FormatUint(uint64(addr), 10)),
			big.NewInt(10),
			big.NewInt(gasPrice),
			100))
		totalRewards += gasPrice
	}
	return totalRewards
}

func addTransactionsWithGas(bl *Block, numOfTxs int, gasPrice int64) int64 {
	var totalRewards int64
	for i := 0; i < numOfTxs; i++ {

		addr := rand.Int63n(10000)
		//log.Info("adding tx with gas price %v nonce %v", gasPrice, i)
		bl.Txs = append(bl.Txs, *NewSerializableTransaction(uint64(i), address.HexToAddress("1"),
			address.HexToAddress(strconv.FormatUint(uint64(addr), 10)),
			big.NewInt(10),
			big.NewInt(gasPrice),
			100))
		totalRewards += gasPrice
	}
	return totalRewards
}

func TestMesh_AccumulateRewards_happyFlow(t *testing.T) {
	s := &MockMapState{Rewards: make(map[string]*big.Int)}
	layers := getMeshWithMapState("t1", s)
	defer layers.Close()

	var totalRewards int64

	block1 := NewBlock(true, []byte("data1"), time.Now(), 1)
	block1.MinerID = "1"
	totalRewards += addTransactions(block1, 15)

	block2 := NewBlock(true, []byte("data2"), time.Now(), 1)
	block2.MinerID = "2"
	totalRewards += addTransactions(block2, 13)

	block3 := NewBlock(true, []byte("data3"), time.Now(), 1)
	block3.MinerID = "3"
	totalRewards += addTransactions(block3, 17)

	block4 := NewBlock(true, []byte("data3"), time.Now(), 1)
	block4.MinerID = "4"
	totalRewards += addTransactions(block4, 16)

	log.Info("total fees : %v", totalRewards)
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)

	params := NewTestRewardParams()

	layers.AccumulateRewards(1, params)
	remainder := (totalRewards * params.SimpleTxCost.Int64()) % 4

	assert.Equal(t, s.Total, totalRewards*params.SimpleTxCost.Int64()+params.BaseReward.Int64()+remainder)

}

func NewTestRewardParams() RewardConfig {
	return RewardConfig{
		big.NewInt(10),
		big.NewInt(5000),
		big.NewInt(20),
		15,
		10,
	}
}

func TestMesh_AccumulateRewards_underQuota(t *testing.T) {
	s := &MockMapState{Rewards: make(map[string]*big.Int)}
	layers := getMeshWithMapState("t1", s)
	defer layers.Close()

	var totalRewards int64

	block1 := NewBlock(true, []byte("data1"), time.Now(), 1)
	block1.MinerID = "1"
	totalRewards += addTransactionsWithGas(block1, 10, 8)

	block2 := NewBlock(true, []byte("data2"), time.Now(), 1)
	block2.MinerID = "2"
	totalRewards += addTransactionsWithGas(block2, 10, 9)

	block3 := NewBlock(true, []byte("data3"), time.Now(), 1)
	block3.MinerID = "3"
	totalRewards += addTransactionsWithGas(block3, 17, 10)

	block4 := NewBlock(true, []byte("data3"), time.Now(), 1)
	block4.MinerID = "4"
	totalRewards += addTransactionsWithGas(block4, 16, 11)

	log.Info("total fees : %v", totalRewards)
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)

	params := NewTestRewardParams()

	layers.AccumulateRewards(1, params)
	remainder := (totalRewards * params.SimpleTxCost.Int64()) % 4

	assert.Equal(t, s.Total, totalRewards*params.SimpleTxCost.Int64()+params.BaseReward.Int64()+remainder)
	assert.Equal(t, s.Rewards[block1.MinerID], s.Rewards[block2.MinerID])
	assert.Equal(t, s.Rewards[block3.MinerID], s.Rewards[block4.MinerID])
	assert.NotEqual(t, s.Rewards[block1.MinerID], s.Rewards[block3.MinerID])

}

func createLayer(mesh *Mesh, id LayerID, numOfBlocks, maxTransactions int) (totalRewards int64) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := NewBlock(true, []byte("data1"), time.Now(), id)
		block1.MinerID = strconv.Itoa(i)
		totalRewards += addTransactions(block1, rand.Intn(maxTransactions))
		mesh.addBlock(block1)
	}
	return totalRewards
}

func TestMesh_integration(t *testing.T) {
	numofLayers := 10
	numofBlocks := 10
	maxTxs := 20

	s := &MockMapState{Rewards: make(map[string]*big.Int)}
	layers := getMeshWithMapState("t1", s)
	defer layers.Close()

	var rewards int64
	for i := 0; i < numofLayers; i++ {
		reward := createLayer(layers, LayerID(i), numofBlocks, maxTxs)
		if rewards == 0 {
			rewards += reward
		}
	}

	oldTotal := s.Total
	l4, err := layers.getLayer(4)
	assert.NoError(t, err)
	l5, err := layers.getLayer(5)
	assert.NoError(t, err)
	//test negative case
	layers.ValidateLayer(l4)
	assert.Equal(t, oldTotal, s.Total)

	layers.ValidateLayer(l5)
	assert.Equal(t, rewards*ConfigTst().SimpleTxCost.Int64()+ConfigTst().BaseReward.Int64(), s.Total)
}

func TestMesh_MergeDoubles(t *testing.T) {
	s := &MockMapState{Rewards: make(map[string]*big.Int)}
	layers := getMeshWithMapState("t1", s)
	defer layers.Close()
	dst := address.HexToAddress("2")
	transactions := []*Transaction{
		{
			AccountNonce: 1,
			Origin:       address.HexToAddress("1"),
			Recipient:    &dst,
			Amount:       big.NewInt(10),
			GasLimit:     100,
			Price:        big.NewInt(1),
			Payload:      nil,
		},
		{
			AccountNonce: 1,
			Origin:       address.HexToAddress("1"),
			Recipient:    &dst,
			Amount:       big.NewInt(10),
			GasLimit:     100,
			Price:        big.NewInt(1),
			Payload:      nil,
		},
		{
			AccountNonce: 1,
			Origin:       address.HexToAddress("1"),
			Recipient:    &dst,
			Amount:       big.NewInt(10),
			GasLimit:     100,
			Price:        big.NewInt(1),
			Payload:      nil,
		},
	}

	txs := MergeDoubles(transactions)

	assert.Equal(t, 1, len(txs))
}
