package mesh

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"math/big"
	"strconv"
	"testing"
)

type MockMapState struct {
	Rewards map[string]*big.Int
	Total   int64
}

func (MockMapState) ApplyTransactions(layer types.LayerID, txs Transactions) (uint32, error) {
	return 0, nil
}

func (s *MockMapState) ApplyRewards(layer types.LayerID, miners []string, underQuota map[string]int, bonusReward, diminishedReward *big.Int) {
	for _, minerId := range miners {
		if _, has := underQuota[minerId]; !has {
			s.Rewards[minerId] = bonusReward
		} else {
			s.Rewards[minerId] = diminishedReward
		}
		s.Total += s.Rewards[minerId].Int64()
	}

}

func ConfigTst() Config {
	return Config{
		big.NewInt(10),
		big.NewInt(5000),
		big.NewInt(15),
		15,
		5,
	}
}

func getMeshWithMapState(id string, s StateUpdater) (*Mesh, *AtxDbMock) {
	atxDb := &AtxDbMock{
		db:     make(map[types.AtxId]*types.ActivationTx),
		nipsts: make(map[types.AtxId]*nipst.NIPST),
	}

	lg := log.New(id, "", "")
	mshdb := NewMemMeshDB(lg)

	return NewMesh(mshdb, atxDb, ConfigTst(), &MeshValidatorMock{}, &MemPoolMock{}, &MemPoolMock{}, s, lg), atxDb
}

func addTransactionsWithGas(mesh *MeshDB, bl *types.Block, numOfTxs int, gasPrice int64) int64 {
	var totalRewards int64
	var txs []*types.SerializableTransaction
	for i := 0; i < numOfTxs; i++ {
		addr := rand.Int63n(10000)
		//log.Info("adding tx with gas price %v nonce %v", gasPrice, i)
		tx := types.NewSerializableTransaction(uint64(i), address.HexToAddress("1"),
			address.HexToAddress(strconv.FormatUint(uint64(addr), 10)),
			big.NewInt(10),
			big.NewInt(gasPrice),
			100)
		bl.TxIds = append(bl.TxIds, types.GetTransactionId(tx))
		totalRewards += gasPrice
		txs = append(txs, tx)
	}
	mesh.writeTransactions(txs)
	return totalRewards
}

func TestMesh_AccumulateRewards_happyFlow(t *testing.T) {
	s := &MockMapState{Rewards: make(map[string]*big.Int)}
	layers, atxdb := getMeshWithMapState("t1", s)
	defer layers.Close()

	var totalRewards int64
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))
	block1.MinerID.Key = "1"
	atx := types.NewActivationTx(block1.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
	atxdb.AddAtx(atx.Id(), atx)
	block1.ATXID = atx.Id()

	totalRewards += addTransactionsWithGas(layers.MeshDB, block1, 15, rand.Int63n(100))

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data2"))
	block2.MinerID.Key = "2"
	atx = types.NewActivationTx(block2.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
	atxdb.AddAtx(atx.Id(), atx)
	block2.ATXID = atx.Id()
	totalRewards += addTransactionsWithGas(layers.MeshDB, block2, 13, rand.Int63n(100))

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data3"))
	block3.MinerID.Key = "3"
	atx = types.NewActivationTx(block3.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
	atxdb.AddAtx(atx.Id(), atx)
	block3.ATXID = atx.Id()
	totalRewards += addTransactionsWithGas(layers.MeshDB, block3, 17, rand.Int63n(100))

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data4"))
	block4.MinerID.Key = "4"
	atx = types.NewActivationTx(block4.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
	atxdb.AddAtx(atx.Id(), atx)
	block4.ATXID = atx.Id()
	totalRewards += addTransactionsWithGas(layers.MeshDB, block4, 16, rand.Int63n(100))

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

func NewTestRewardParams() Config {
	return Config{
		big.NewInt(10),
		big.NewInt(5000),
		big.NewInt(20),
		15,
		10,
	}
}

func TestMesh_AccumulateRewards_underQuota(t *testing.T) {
	s := &MockMapState{Rewards: make(map[string]*big.Int)}
	layers, atxdb := getMeshWithMapState("t1", s)
	defer layers.Close()

	var totalRewards int64

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))
	block1.MinerID.Key = "1"
	totalRewards += addTransactionsWithGas(layers.MeshDB, block1, 10, 8)
	atx := types.NewActivationTx(block1.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
	atxdb.AddAtx(atx.Id(), atx)
	block1.ATXID = atx.Id()

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data2"))
	block2.MinerID.Key = "2"
	totalRewards += addTransactionsWithGas(layers.MeshDB, block2, 10, 9)
	atx = types.NewActivationTx(block2.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
	atxdb.AddAtx(atx.Id(), atx)
	block2.ATXID = atx.Id()

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data3"))
	block3.MinerID.Key = "3"
	totalRewards += addTransactionsWithGas(layers.MeshDB, block3, 17, 10)
	atx = types.NewActivationTx(block3.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
	atxdb.AddAtx(atx.Id(), atx)
	block3.ATXID = atx.Id()

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data4"))
	block4.MinerID.Key = "4"
	totalRewards += addTransactionsWithGas(layers.MeshDB, block4, 16, 11)
	atx = types.NewActivationTx(block4.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
	atxdb.AddAtx(atx.Id(), atx)
	block4.ATXID = atx.Id()

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

func createLayer(mesh *Mesh, id types.LayerID, numOfBlocks, maxTransactions int, atxdb *AtxDbMock) (totalRewards int64) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), id, []byte("data1"))
		block1.MinerID.Key = strconv.Itoa(i)
		atx := types.NewActivationTx(block1.MinerID, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &nipst.NIPST{}, true)
		atxdb.AddAtx(atx.Id(), atx)
		block1.ATXID = atx.Id()
		totalRewards += addTransactionsWithGas(mesh.MeshDB, block1, rand.Intn(maxTransactions), rand.Int63n(100))
		mesh.AddBlock(block1)
	}
	return totalRewards
}

func TestMesh_integration(t *testing.T) {
	numofLayers := 10
	numofBlocks := 10
	maxTxs := 20

	s := &MockMapState{Rewards: make(map[string]*big.Int)}
	layers, atxdb := getMeshWithMapState("t1", s)
	defer layers.Close()

	var rewards int64
	for i := 0; i < numofLayers; i++ {
		reward := createLayer(layers, types.LayerID(i), numofBlocks, maxTxs, atxdb)
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
	cfg := Config{PenaltyPercent: big.NewInt(13)}
	bonus, penalty := calculateActualRewards(big.NewInt(10000), big.NewInt(10), cfg, 5)
	assert.Equal(t, int64(10000), bonus.Int64()*5+penalty.Int64()*5)
	assert.Equal(t, int64(1065), bonus.Int64())
	assert.Equal(t, int64(935), penalty.Int64())
}

func TestMesh_MergeDoubles(t *testing.T) {
	s := &MockMapState{Rewards: make(map[string]*big.Int)}
	layers, _ := getMeshWithMapState("t1", s)
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
