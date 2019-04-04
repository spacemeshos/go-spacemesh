package mesh

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"math/big"
	"strconv"
	"testing"
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
	layers := NewMemMesh(ConfigTst(), &MeshValidatorMock{}, s, log.New(id, "", ""))
	return layers
}

func addTransactionsToBlock(bl *Block, numOfTxs int) int64 {
	var totalRewards int64
	for i := 0; i < numOfTxs; i++ {
		gasPrice := rand.Int63n(100)
		addr := rand.Int63n(1000000)
		//log.Info("adding tx with gas price %v nonce %v", gasPrice, i)
		bl.Txs = append(bl.Txs, NewSerializableTransaction(uint64(i), address.HexToAddress("1"),
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
		bl.Txs = append(bl.Txs, NewSerializableTransaction(uint64(i), address.HexToAddress("1"),
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
	block1 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data1"))
	block1.MinerID.Key = "1"
	totalRewards += addTransactionsToBlock(block1, 15)

	block2 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data2"))
	block2.MinerID.Key = "2"
	totalRewards += addTransactionsToBlock(block2, 13)

	block3 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data3"))
	block3.MinerID.Key = "3"
	totalRewards += addTransactionsToBlock(block3, 17)

	block4 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data4"))
	block4.MinerID.Key = "4"
	totalRewards += addTransactionsToBlock(block4, 16)

	log.Info("total fees : %v", totalRewards)
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)

	params := NewTestRewardParams()

	layers.AccumulateRewards(1, params)
	totalRewardsCost := totalRewards*params.SimpleTxCost.Int64() + params.BaseReward.Int64()
	remainder := (totalRewardsCost) % 4
	var adj int64

	//total penalty of blocks with less txs than quota sometimes does not divide equally between all nodes, therefore some Lerners can be lost
	reward_penalty := (((totalRewardsCost + adj) / 4) * (params.PenaltyPercent.Int64())) / 100

	log.Info("remainder %v reward_penalty %v mod %v reward cost %v", remainder, reward_penalty, (reward_penalty)%4, totalRewardsCost)

	assert.Equal(t, totalRewards*params.SimpleTxCost.Int64()+params.BaseReward.Int64()-(reward_penalty)%4+remainder, s.Total)

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

	block1 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data1"))
	block1.MinerID.Key = "1"
	totalRewards += addTransactionsWithGas(block1, 10, 8)

	block2 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data2"))
	block2.MinerID.Key = "2"
	totalRewards += addTransactionsWithGas(block2, 10, 9)

	block3 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data3"))
	block3.MinerID.Key = "3"
	totalRewards += addTransactionsWithGas(block3, 17, 10)

	block4 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data4"))
	block4.MinerID.Key = "4"
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
	assert.Equal(t, s.Rewards[block1.MinerID.Key], s.Rewards[block2.MinerID.Key])
	assert.Equal(t, s.Rewards[block3.MinerID.Key], s.Rewards[block4.MinerID.Key])
	assert.NotEqual(t, s.Rewards[block1.MinerID.Key], s.Rewards[block3.MinerID.Key])

}

func createLayer(mesh *Mesh, id LayerID, numOfBlocks, maxTransactions int) (totalRewards int64) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := NewExistingBlock(BlockID(uuid.New().ID()), id, []byte("data1"))
		block1.MinerID.Key = strconv.Itoa(i)
		totalRewards += addTransactionsToBlock(block1, rand.Intn(maxTransactions))
		mesh.AddBlock(block1)
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
		// rewards are applied to layers in the past according to the reward maturity param
		if rewards == 0 {
			rewards += reward
			log.Info("reward %v", rewards)
		}
	}

	oldTotal := s.Total
	l4, err := layers.GetLayer(4)
	assert.NoError(t, err)
	l5, err := layers.GetLayer(5)
	assert.NoError(t, err)
	//test negative case
	layers.ValidateLayer(l4)
	assert.Equal(t, oldTotal, s.Total)

	//reward maturity is 5, when processing layer 5 rewards will be applied
	layers.ValidateLayer(l5)
	//since there can be a difference of up to x lerners where x is the number of blocks due to round up of penalties when distributed among all blocks
	totalPayout := rewards*ConfigTst().SimpleTxCost.Int64() + ConfigTst().BaseReward.Int64()
	assert.True(t, totalPayout-s.Total < int64(numofBlocks), " rewards : %v, total %v blocks %v", totalPayout, s.Total, int64(numofBlocks))
}

func TestMesh_calcRewards(t *testing.T) {
	cfg := RewardConfig{PenaltyPercent: big.NewInt(13)}
	bonus, penalty := calculateActualRewards(big.NewInt(10000), big.NewInt(10), cfg, 5)
	assert.Equal(t, int64(10000), bonus.Int64()*5+penalty.Int64()*5)
	assert.Equal(t, int64(1065), bonus.Int64())
	assert.Equal(t, int64(935), penalty.Int64())
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
