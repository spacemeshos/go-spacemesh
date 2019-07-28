package mesh

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"math/big"
	"strconv"
	"testing"
)

type MockMapState struct {
	Rewards map[address.Address]*big.Int
	Total   int64
}

func (MockMapState) ValidateSignature(signed types.Signed) (address.Address, error) {
	return address.Address{}, nil
}

func (MockMapState) ApplyTransactions(layer types.LayerID, txs Transactions) (uint32, error) {
	return 0, nil
}

func (s *MockMapState) ApplyRewards(layer types.LayerID, miners []address.Address, underQuota map[address.Address]int, bonusReward, diminishedReward *big.Int) {
	for _, minerId := range miners {
		if _, has := underQuota[minerId]; !has {
			s.Rewards[minerId] = bonusReward
		} else {
			s.Rewards[minerId] = diminishedReward
		}
		s.Total += s.Rewards[minerId].Int64()
	}

}

func (s *MockMapState) ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (address.Address, error) {
	return address.Address{}, nil
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

func getMeshWithMapState(id string, s TxProcessor) (*Mesh, *AtxDbMock) {
	atxDb := &AtxDbMock{
		db:     make(map[types.AtxId]*types.ActivationTx),
		nipsts: make(map[types.AtxId]*types.NIPST),
	}

	lg := log.New(id, "", "")
	mshdb := NewMemMeshDB(lg)

	return NewMesh(mshdb, atxDb, ConfigTst(), &MeshValidatorMock{}, &MockTxMemPool{}, &MockAtxMemPool{}, s, lg), atxDb
}

func addTransactionsWithGas(mesh *MeshDB, bl *types.Block, numOfTxs int, gasPrice int64) int64 {
	var totalRewards int64
	var txs []*types.AddressableSignedTransaction
	for i := 0; i < numOfTxs; i++ {
		addr := rand.Int63n(10000)
		//log.Info("adding tx with gas price %v nonce %v", gasPrice, i)
		tx := types.NewAddressableTx(uint64(i), address.HexToAddress("1"),
			address.HexToAddress(strconv.FormatUint(uint64(addr), 10)),
			10,
			100,
			uint64(gasPrice))
		bl.TxIds = append(bl.TxIds, types.GetTransactionId(tx.SerializableSignedTransaction))
		totalRewards += gasPrice
		txs = append(txs, tx)
	}
	mesh.writeTransactions(txs)
	return totalRewards
}

func TestMesh_AccumulateRewards_happyFlow(t *testing.T) {
	s := &MockMapState{Rewards: make(map[address.Address]*big.Int)}
	layers, atxdb := getMeshWithMapState("t1", s)
	defer layers.Close()

	var totalRewards int64
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))

	coinbase1 := address.HexToAddress("0xaaa")
	atx := types.NewActivationTx(types.NodeId{"1", []byte("bbbbb")}, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block1.ATXID = atx.Id()
	totalRewards += addTransactionsWithGas(layers.MeshDB, block1, 15, 7)

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data2"))

	coinbase2 := address.HexToAddress("0xbbb")
	atx = types.NewActivationTx(types.NodeId{"2", []byte("bbbbb")}, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block2.ATXID = atx.Id()
	totalRewards += addTransactionsWithGas(layers.MeshDB, block2, 13, rand.Int63n(100))

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data3"))

	coinbase3 := address.HexToAddress("0xccc")
	atx = types.NewActivationTx(types.NodeId{"3", []byte("bbbbb")}, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx.Id(), atx)
	block3.ATXID = atx.Id()
	totalRewards += addTransactionsWithGas(layers.MeshDB, block3, 17, rand.Int63n(100))

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data4"))

	coinbase4 := address.HexToAddress("0xddd")
	atx = types.NewActivationTx(types.NodeId{"4", []byte("bbbbb")}, coinbase4, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
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
	s := &MockMapState{Rewards: make(map[address.Address]*big.Int)}
	layers, atxdb := getMeshWithMapState("t1", s)
	defer layers.Close()

	var totalRewards int64

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))
	coinbase1 := address.HexToAddress("0xaaa")
	totalRewards += addTransactionsWithGas(layers.MeshDB, block1, 10, 8)
	atx1 := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx1.Id(), atx1)
	block1.ATXID = atx1.Id()

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data2"))
	coinbase2 := address.HexToAddress("0xbbb")
	totalRewards += addTransactionsWithGas(layers.MeshDB, block2, 10, 9)
	atx2 := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx2.Id(), atx2)
	block2.ATXID = atx2.Id()

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data3"))
	coinbase3 := address.HexToAddress("0xccc")
	totalRewards += addTransactionsWithGas(layers.MeshDB, block3, 17, 10)
	atx3 := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx3.Id(), atx3)
	block3.ATXID = atx3.Id()

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data4"))
	coinbase4 := address.HexToAddress("0xddd")
	totalRewards += addTransactionsWithGas(layers.MeshDB, block4, 16, 11)
	atx4 := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("bbbbb")}, coinbase4, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
	atxdb.AddAtx(atx4.Id(), atx4)
	block4.ATXID = atx4.Id()

	log.Info("total fees : %v", totalRewards)
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)

	params := NewTestRewardParams()

	layers.AccumulateRewards(1, params)
	remainder := (totalRewards * params.SimpleTxCost.Int64()) % 4

	assert.Equal(t, s.Total, totalRewards*params.SimpleTxCost.Int64()+params.BaseReward.Int64()+remainder)
	assert.Equal(t, s.Rewards[atx1.Coinbase], s.Rewards[atx2.Coinbase])
	assert.Equal(t, s.Rewards[atx3.Coinbase], s.Rewards[atx4.Coinbase])
	assert.NotEqual(t, s.Rewards[atx1.Coinbase], s.Rewards[atx3.Coinbase])

}

func createLayer(mesh *Mesh, id types.LayerID, numOfBlocks, maxTransactions int, atxdb *AtxDbMock) (totalRewards int64) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), id, []byte("data1"))
		nodeid := types.NodeId{strconv.Itoa(i), []byte("bbbbb")}
		coinbase := address.HexToAddress(nodeid.Key)
		atx := types.NewActivationTx(nodeid, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 10, []types.BlockID{}, &types.NIPST{})
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

	s := &MockMapState{Rewards: make(map[address.Address]*big.Int)}
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
	s := &MockMapState{Rewards: make(map[address.Address]*big.Int)}
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
			GasPrice:     big.NewInt(1),
			Payload:      nil,
		},
		{
			AccountNonce: 1,
			Origin:       address.HexToAddress("1"),
			Recipient:    &dst,
			Amount:       big.NewInt(10),
			GasLimit:     100,
			GasPrice:     big.NewInt(1),
			Payload:      nil,
		},
		{
			AccountNonce: 1,
			Origin:       address.HexToAddress("1"),
			Recipient:    &dst,
			Amount:       big.NewInt(10),
			GasLimit:     100,
			GasPrice:     big.NewInt(1),
			Payload:      nil,
		},
	}

	txs := MergeDoubles(transactions)

	assert.Equal(t, 1, len(txs))
}
