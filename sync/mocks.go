package sync

import (
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"math/big"
	"time"
)

type poetDbMock struct{}

func (poetDbMock) GetProofMessage(proofRef []byte) ([]byte, error) { return proofRef, nil }

func (poetDbMock) HasProof([]byte) bool { return true }

func (poetDbMock) ValidateAndStore(*types.PoetProofMessage) error { return nil }

type blockEligibilityValidatorMock struct {
}

func (blockEligibilityValidatorMock) BlockSignedAndEligible(*types.Block) (bool, error) {
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

func (m *meshValidatorMock) HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID) {
	return bl.Layer(), bl.Layer() - 1

}

type mockState struct{}

func (s mockState) ValidateAndAddTxToPool(tx *types.Transaction) error {
	panic("implement me")
}

func (s mockState) LoadState(types.LayerID) error {
	panic("implement me")
}

func (s mockState) GetStateRoot() types.Hash32 {
	return [32]byte{}
}

func (mockState) ValidateNonceAndBalance(*types.Transaction) error {
	panic("implement me")
}

func (mockState) GetLayerStateRoot(id types.LayerID) (types.Hash32, error) {
	panic("implement me")
}

func (mockState) GetLayerApplied(types.TransactionID) *types.LayerID {
	panic("implement me")
}

func (mockState) ApplyTransactions(types.LayerID, []*types.Transaction) (int, error) {
	return 0, nil
}

func (mockState) ApplyRewards(types.LayerID, []types.Address, *big.Int) {}

func (mockState) GetBalance(types.Address) uint64 {
	panic("implement me")
}

func (mockState) GetNonce(types.Address) uint64 {
	panic("implement me")
}

func (mockState) AddressExists(types.Address) bool {
	return true
}

type mockIStore struct{}

func (*mockIStore) StoreNodeIdentity(types.NodeID) error {
	return nil
}

func (*mockIStore) GetIdentity(string) (types.NodeID, error) {
	return types.NodeID{Key: "some string ", VRFPublicKey: []byte("bytes")}, nil
}

type validatorMock struct{}

func (*validatorMock) Validate(signing.PublicKey, *types.NIPST, types.Hash32) error {
	return nil
}

func (*validatorMock) VerifyPost(signing.PublicKey, *types.PostProof, uint64) error {
	return nil
}

type mockTxMemPool struct{}

func (mockTxMemPool) Get(types.TransactionID) (*types.Transaction, error) {
	return &types.Transaction{}, nil
}

func (mockTxMemPool) GetAllItems() []*types.Transaction {
	return nil
}

func (mockTxMemPool) Put(types.TransactionID, *types.Transaction) {}
func (mockTxMemPool) Invalidate(types.TransactionID)              {}

type mockAtxMemPool struct{}

func (mockAtxMemPool) Get(types.ATXID) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (mockAtxMemPool) GetAllItems() []types.ActivationTx {
	return nil
}

func (mockAtxMemPool) Put(*types.ActivationTx) {}
func (mockAtxMemPool) Invalidate(types.ATXID)  {}

type mockClock struct {
	ch         map[timesync.LayerTimer]int
	ids        map[int]timesync.LayerTimer
	countSub   int
	countUnsub int
	Interval   duration.Duration
	Layer      types.LayerID
}

func (m *mockClock) LayerToTime(types.LayerID) time.Time {
	return time.Now().Add(1000 * time.Hour) // hack so this wont take affect in the mock
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
// for use in testing
func NewSyncWithMocks(atxdbStore *database.LDBDatabase, mshdb *mesh.DB, txpool *state.TxMempool, atxpool *activation.AtxMemDB, swarm service.Service, poetDb *activation.PoetDb, conf Configuration, expectedLayers types.LayerID) *Syncer {
	lg := log.New("sync_test", "", "")
	atxdb := activation.NewDB(atxdbStore, &mockIStore{}, mshdb, conf.LayersPerEpoch, &validatorMock{}, lg.WithOptions(log.Nop))
	var msh *mesh.Mesh
	if mshdb.PersistentData() {
		lg.Info("persistent data found ")
		msh = mesh.NewRecoveredMesh(mshdb, atxdb, configTst(), &meshValidatorMock{}, txpool, &mockState{}, lg)
	} else {
		lg.Info("no persistent data found ")
		msh = mesh.NewMesh(mshdb, atxdb, configTst(), &meshValidatorMock{}, txpool, &mockState{}, lg)
	}

	_ = msh.AddBlock(mesh.GenesisBlock())
	clock := mockClock{Layer: expectedLayers + 1}
	lg.Info("current layer %v", clock.GetCurrentLayer())
	return NewSync(swarm, msh, txpool, atxdb, blockEligibilityValidatorMock{}, poetDb, conf, &clock, lg)
}
