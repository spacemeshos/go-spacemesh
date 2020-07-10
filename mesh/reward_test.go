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
	Pool        []*types.Transaction
	TotalReward int64
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

func getMeshWithMapState(id string, s txProcessor) (*Mesh, *AtxDbMock) {
	atxDb := NewAtxDbMock()
	lg := log.New(id, "", "")
	mshDb := NewMemMeshDB(lg)
	mshDb.contextualValidity = &ContextualValidityMock{}
	return NewMesh(mshDb, atxDb, ConfigTst(), &MeshValidatorMock{}, &MockTxMemPool{}, s, lg), atxDb
}

func addTransactionsWithFee(t testing.TB, mesh *DB, bl *types.Block, numOfTxs int, fee int64) int64 {
	var totalFee int64
	var txs []*types.Transaction
	for i := 0; i < numOfTxs; i++ {
		// log.Info("adding tx with fee %v nonce %v", fee, i)
		tx, err := types.NewSignedTx(1, types.HexToAddress("1"), 10, 100, uint64(fee), signing.NewEdSigner())
		assert.NoError(t, err)
		bl.TxIDs = append(bl.TxIDs, tx.ID())
		totalFee += fee
		txs = append(txs, tx)
	}
	err := mesh.writeTransactions(0, txs)
	assert.NoError(t, err)
	return totalFee
}

func TestMesh_AccumulateRewards_happyFlow(t *testing.T) {
	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	layers, atxDB := getMeshWithMapState("t1", s)
	defer layers.Close()

	var totalFee int64
	block1 := types.NewExistingBlock(1, []byte(rand.String(8)))

	coinbase1 := types.HexToAddress("0xaaa")
	atx := newActivationTx(types.NodeID{Key: "1", VRFPublicKey: []byte("bbbbb")}, 0, *types.EmptyATXID, 1, 0, *types.EmptyATXID, coinbase1, 10, []types.BlockID{}, &types.NIPST{})
	atxDB.AddAtx(atx.ID(), atx)
	block1.ATXID = atx.ID()
	totalFee += addTransactionsWithFee(t, layers.DB, block1, 15, 7)

	block2 := types.NewExistingBlock(1, []byte(rand.String(8)))

	coinbase2 := types.HexToAddress("0xbbb")
	atx = newActivationTx(types.NodeID{Key: "2", VRFPublicKey: []byte("bbbbb")}, 0, *types.EmptyATXID, 1, 0, *types.EmptyATXID, coinbase2, 10, []types.BlockID{}, &types.NIPST{})
	atxDB.AddAtx(atx.ID(), atx)
	block2.ATXID = atx.ID()
	totalFee += addTransactionsWithFee(t, layers.DB, block2, 13, rand.Int63n(100))

	block3 := types.NewExistingBlock(1, []byte(rand.String(8)))

	coinbase3 := types.HexToAddress("0xccc")
	atx = newActivationTx(types.NodeID{Key: "3", VRFPublicKey: []byte("bbbbb")}, 0, *types.EmptyATXID, 1, 0, *types.EmptyATXID, coinbase3, 10, []types.BlockID{}, &types.NIPST{})
	atxDB.AddAtx(atx.ID(), atx)
	block3.ATXID = atx.ID()
	totalFee += addTransactionsWithFee(t, layers.DB, block3, 17, rand.Int63n(100))

	block4 := types.NewExistingBlock(1, []byte(rand.String(8)))

	coinbase4 := types.HexToAddress("0xddd")
	atx = newActivationTx(types.NodeID{Key: "4", VRFPublicKey: []byte("bbbbb")}, 0, *types.EmptyATXID, 1, 0, *types.EmptyATXID, coinbase4, 10, []types.BlockID{}, &types.NIPST{})
	atxDB.AddAtx(atx.ID(), atx)
	block4.ATXID = atx.ID()
	totalFee += addTransactionsWithFee(t, layers.DB, block4, 16, rand.Int63n(100))

	log.Info("total fees : %v", totalFee)
	_ = layers.AddBlock(block1)
	_ = layers.AddBlock(block2)
	_ = layers.AddBlock(block3)
	_ = layers.AddBlock(block4)

	params := NewTestRewardParams()

	l, err := layers.GetLayer(1)
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
		block1 := types.NewExistingBlock(id, []byte(rand.String(8)))
		nodeID := types.NodeID{Key: strconv.Itoa(i), VRFPublicKey: []byte("bbbbb")}
		coinbase := types.HexToAddress(nodeID.Key)
		atx := newActivationTx(nodeID, 0, *types.EmptyATXID, 1, 0, *types.EmptyATXID, coinbase, 10, []types.BlockID{}, &types.NIPST{})
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
	layers, atxDB := getMeshWithMapState("t1", s)
	defer layers.Close()

	var l3Rewards int64
	for i := 0; i < numOfLayers; i++ {
		reward, _ := createLayer(t, layers, types.LayerID(i), numOfBlocks, maxTxs, atxDB)
		// rewards are applied to layers in the past according to the reward maturity param
		if i == 3 {
			l3Rewards = reward
			log.Info("reward %v", l3Rewards)
		}

		l, err := layers.GetLayer(types.LayerID(i))
		assert.NoError(t, err)
		layers.ValidateLayer(l)
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
	// test that state does not advance when layer x +2 is received before layer x+1, and then test that all layers are pushed

	numOfLayers := 10
	numOfBlocks := 10
	maxTxs := 20

	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh, atxDB := getMeshWithMapState("t1", s)
	defer mesh.Close()

	for i := 0; i < numOfLayers; i++ {
		createLayer(t, mesh, types.LayerID(i), numOfBlocks, maxTxs, atxDB)
		l, err := mesh.GetLayer(types.LayerID(i))
		assert.NoError(t, err)
		mesh.ValidateLayer(l)
	}

	s2 := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh2, atxDB2 := getMeshWithMapState("t2", s2)

	// this should be played until numOfLayers -1 if we want to compare states
	for i := 0; i < numOfLayers-1; i++ {
		blockIds := copyLayer(t, mesh, mesh2, atxDB2, types.LayerID(i))
		mesh2.HandleValidatedLayer(types.LayerID(i), blockIds)
	}
	// test states are the same when one input is from tortoise and the other from hare
	assert.Equal(t, s.Txs, s2.Txs)

	for i := 0; i < numOfLayers; i++ {
		l, err := mesh.GetLayer(types.LayerID(i))
		assert.NoError(t, err)
		mesh2.ValidateLayer(l)
	}
	// test state is the same if receiving result from tortoise after same result from hare received
	assert.ObjectsAreEqualValues(s.Txs, s2.Txs)

	// test state is the same after late block
	layer4, err := mesh.GetLayer(4)
	assert.NoError(t, err)

	blk := layer4.Blocks()[0]
	mesh2.HandleLateBlock(blk)
	assert.Equal(t, s.Txs, s2.Txs)

	// test that state does not advance when layer x +2 is received before layer x+1, and then test that all layers are pushed
	s3 := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh3, atxDB3 := getMeshWithMapState("t3", s3)

	// this should be played until numOfLayers -1 if we want to compare states
	for i := 0; i < numOfLayers-3; i++ {
		blockIds := copyLayer(t, mesh, mesh3, atxDB3, types.LayerID(i))
		mesh3.HandleValidatedLayer(types.LayerID(i), blockIds)
	}
	s3Len := len(s3.Txs)
	blockIds := copyLayer(t, mesh, mesh3, atxDB3, types.LayerID(numOfLayers-2))
	mesh3.HandleValidatedLayer(types.LayerID(numOfLayers-2), blockIds)
	assert.Equal(t, s3Len, len(s3.Txs))

	blockIds = copyLayer(t, mesh, mesh3, atxDB3, types.LayerID(numOfLayers-3))
	mesh3.HandleValidatedLayer(types.LayerID(numOfLayers-3), blockIds)
	assert.Equal(t, s.Txs, s3.Txs)
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
		atx, err := srcMesh.GetFullAtx(b.ATXID)
		assert.NoError(t, err)
		dstAtxDb.AddAtx(atx.ID(), atx)
		err = dstMesh.AddBlockWithTxs(b, txs, []*types.ActivationTx{})
		assert.NoError(t, err)
		blockIds = append(blockIds, b.ID())
	}
	return blockIds
}

type meshValidatorBatchMock struct {
	mesh           *Mesh
	batchSize      types.LayerID
	processedLayer types.LayerID
}

func (m *meshValidatorBatchMock) ValidateLayer(lyr *types.Layer) {
	m.SetProcessedLayer(lyr.Index())
	layerID := lyr.Index()
	if layerID == 0 {
		return
	}
	if layerID%m.batchSize == 0 {
		m.mesh.pushLayersToState(layerID-m.batchSize, layerID)
		return
	}
	prevPBase := layerID - layerID%m.batchSize
	m.mesh.pushLayersToState(prevPBase, prevPBase)
}

func (m *meshValidatorBatchMock) ProcessedLayer() types.LayerID       { panic("implement me") }
func (m *meshValidatorBatchMock) SetProcessedLayer(lyr types.LayerID) { m.processedLayer = lyr }
func (m *meshValidatorBatchMock) HandleLateBlock(*types.Block)        { panic("implement me") }

func TestMesh_AccumulateRewards(t *testing.T) {
	numOfLayers := 10
	numOfBlocks := 10
	maxTxs := 20
	batchSize := 6

	s := &MockMapState{Rewards: make(map[types.Address]*big.Int)}
	mesh, atxDb := getMeshWithMapState("t1", s)
	defer mesh.Close()

	mesh.Validator = &meshValidatorBatchMock{mesh: mesh, batchSize: types.LayerID(batchSize)}

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
	reward, remainder := calculateActualRewards(1, big.NewInt(10000), big.NewInt(10))
	assert.Equal(t, int64(1000), reward.Int64())
	assert.Equal(t, int64(0), remainder.Int64())
}

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, activeSetSize uint32, view []types.BlockID,
	nipst *types.NIPST) *types.ActivationTx {

	nipstChallenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipstChallenge, coinbase, nipst, nil)
}
