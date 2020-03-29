package sync

import (
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"math/big"
	"time"
)

type poetDbMock struct{}

func (poetDbMock) GetProofMessage(proofRef []byte) ([]byte, error) { return proofRef, nil }

func (poetDbMock) HasProof(proofRef []byte) bool { return true }

func (poetDbMock) ValidateAndStore(proofMessage *types.PoetProofMessage) error { return nil }

func (*poetDbMock) SubscribeToProofRef(poetID []byte, roundID string) chan []byte {
	ch := make(chan []byte)
	go func() {
		ch <- []byte("hello there")
	}()
	return ch
}

func (*poetDbMock) GetMembershipMap(poetRoot []byte) (map[types.Hash32]bool, error) {
	hash := types.BytesToHash([]byte("anton"))
	return map[types.Hash32]bool{hash: true}, nil
}

type blockEligibilityValidatorMock struct {
}

func (blockEligibilityValidatorMock) BlockSignedAndEligible(block *types.Block) (bool, error) {
	return true, nil
}

type syntacticValidatorMock struct {
}

func (syntacticValidatorMock) SyntacticallyValid(block *types.BlockHeader) (bool, error) {
	return true, nil
}

type meshValidatorMock struct {
	delay           time.Duration
	calls           int
	layers          *mesh.Mesh
	vl              types.LayerID // the validated layer
	countValidated  int
	countValidate   int
	validatedLayers map[types.LayerID]struct{}
}

func (m *meshValidatorMock) LatestComplete() types.LayerID {
	panic("implement me")
}

func (m *meshValidatorMock) Persist() error {
	return nil
}

func (m *meshValidatorMock) HandleIncomingLayer(lyr *types.Layer) (types.LayerID, types.LayerID) {
	m.countValidate++
	m.calls++
	m.vl = lyr.Index()
	if m.validatedLayers == nil {
		m.validatedLayers = make(map[types.LayerID]struct{})
	}
	m.validatedLayers[lyr.Index()] = struct{}{}
	time.Sleep(m.delay)
	return lyr.Index(), lyr.Index() - 1
}

func (m *meshValidatorMock) GetGoodPatternBlocks(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	panic("implement me")
}

func (m *meshValidatorMock) HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID) {
	return bl.Layer(), bl.Layer() - 1

}

func (m *meshValidatorMock) RegisterLayerCallback(func(id types.LayerID)) {}
func (m *meshValidatorMock) ContextualValidity(id types.BlockID) bool     { return true }

type mockState struct{}

func (s mockState) LoadState(layer types.LayerID) error {
	panic("implement me")
}

func (s mockState) GetStateRoot() types.Hash32 {
	return [32]byte{}
}

func (mockState) ValidateNonceAndBalance(transaction *types.Transaction) error {
	panic("implement me")
}

func (mockState) GetLayerApplied(txID types.TransactionId) *types.LayerID {
	panic("implement me")
}

func (mockState) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error) {
	return 0, nil
}

func (mockState) ValidateSignature(signed types.Signed) (types.Address, error) {
	return types.Address{}, nil
}

func (mockState) ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int) {
}

func (mockState) AddressExists(addr types.Address) bool {
	return true
}

type mockIStore struct {
}

func (*mockIStore) StoreNodeIdentity(id types.NodeId) error {
	return nil
}

func (*mockIStore) GetIdentity(id string) (types.NodeId, error) {
	return types.NodeId{Key: "some string ", VRFPublicKey: []byte("bytes")}, nil
}

type validatorMock struct{}

func (*validatorMock) Validate(id signing.PublicKey, nipst *types.NIPST, expectedChallenge types.Hash32) error {
	return nil
}

func (*validatorMock) VerifyPost(id signing.PublicKey, proof *types.PostProof, space uint64) error {
	return nil
}

type mockTxMemPool struct{}

func (mockTxMemPool) Get(id types.TransactionId) (*types.Transaction, error) {
	return &types.Transaction{}, nil
}
func (mockTxMemPool) GetAllItems() []*types.Transaction {
	return nil
}
func (mockTxMemPool) Put(id types.TransactionId, item *types.Transaction) {

}
func (mockTxMemPool) Invalidate(id types.TransactionId) {

}

type mockAtxMemPool struct{}

func (mockAtxMemPool) Get(id types.AtxId) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (mockAtxMemPool) GetAllItems() []types.ActivationTx {
	return nil
}

func (mockAtxMemPool) Put(atx *types.ActivationTx) {

}

func (mockAtxMemPool) Invalidate(id types.AtxId) {

}

type mockClock struct {
	ch         map[timesync.LayerTimer]int
	ids        map[int]timesync.LayerTimer
	countSub   int
	countUnsub int
	Interval   duration.Duration
	Layer      types.LayerID
}

func (m *mockClock) LayerToTime(types.LayerID) time.Time {
	return time.Now().Add(1000 * time.Hour) //hack so this wont take affect in the mock
}

func (m *mockClock) Tick() {
	l := m.GetCurrentLayer()
	log.Info("tick %v", l)
	for _, c := range m.ids {
		c <- l
	}
}

func (m *mockClock) GetCurrentLayer() types.LayerID {
	return m.Layer
}

func (m *mockClock) Subscribe() timesync.LayerTimer {
	m.countSub++

	if m.ch == nil {
		m.ch = make(map[timesync.LayerTimer]int)
		m.ids = make(map[int]timesync.LayerTimer)
	}
	newCh := make(chan types.LayerID, 1)
	m.ch[newCh] = len(m.ch)
	m.ids[len(m.ch)] = newCh

	return newCh
}

func (m *mockClock) Unsubscribe(timer timesync.LayerTimer) {
	m.countUnsub++
	delete(m.ids, m.ch[timer])
	delete(m.ch, timer)
}

func configTst() mesh.Config {
	return mesh.Config{
		BaseReward: big.NewInt(5000),
	}
}

type mockBlockBuilder struct {
	txs []*types.Transaction
}

func (m *mockBlockBuilder) ValidateAndAddTxToPool(tx *types.Transaction) error {
	m.txs = append(m.txs, tx)
	return nil
}

// NewSyncWithMocks returns a syncer instance that is backed by mocks of other modules
//for use in testing
func NewSyncWithMocks(atxdbStore *database.LDBDatabase, mshdb *mesh.DB, txpool *miner.TxMempool, atxpool *miner.AtxMemPool, swarm service.Service, poetDb *activation.PoetDb, conf Configuration, expectedLayers types.LayerID) *Syncer {
	lg := log.New("sync_test", "", "")
	atxdb := activation.NewActivationDb(atxdbStore, &mockIStore{}, mshdb, uint16(conf.LayersPerEpoch), &validatorMock{}, lg.WithOptions(log.Nop))
	var msh *mesh.Mesh
	if mshdb.PersistentData() {
		lg.Info("persistent data found ")
		msh = mesh.NewRecoveredMesh(mshdb, atxdb, configTst(), &meshValidatorMock{}, txpool, atxpool, &mockState{}, lg)
	} else {
		lg.Info("no persistent data found ")
		msh = mesh.NewMesh(mshdb, atxdb, configTst(), &meshValidatorMock{}, txpool, atxpool, &mockState{}, lg)
	}

	msh.SetBlockBuilder(&mockBlockBuilder{})

	defer msh.Close()
	msh.AddBlock(mesh.GenesisBlock)
	clock := mockClock{Layer: expectedLayers + 1}
	lg.Info("current layer %v", clock.GetCurrentLayer())
	return NewSync(swarm, msh, txpool, atxpool, blockEligibilityValidatorMock{}, poetDb, conf, &clock, lg)
}
