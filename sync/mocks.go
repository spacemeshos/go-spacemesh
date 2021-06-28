package sync

import (
	"context"
	"math/big"
	"time"

	"fmt"
	"github.com/golang/protobuf/ptypes/duration"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

type poetDbMock struct {
	Data map[types.Hash32][]byte
}

func (poetDbMock) GetProofMessage(proofRef []byte) ([]byte, error) { return proofRef, nil }

func (poetDbMock) HasProof([]byte) bool { return true }

func (p *poetDbMock) ValidateAndStore(m *types.PoetProofMessage) error {
	b, e := m.Ref()
	if e != nil {
		return e
	}
	val, e := types.InterfaceToBytes(m.PoetProof)

	p.Data[types.BytesToHash(b)] = val
	return nil
}

func (p *poetDbMock) Get(b []byte) ([]byte, error) {
	b, has := p.Data[types.BytesToHash(b)]
	if !has {
		return nil, fmt.Errorf("not found in mock db")
	}
	return b, nil
}

func (p *poetDbMock) Put(key, val []byte) error {
	p.Data[types.BytesToHash(key)] = val
	return nil
}

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
	return m.vl
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
	return nil
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

func (mockState) GetAllAccounts() (*types.MultipleAccountsState, error) {
	panic("implement me")
}

type mockIStore struct{}

func (*mockIStore) StoreNodeIdentity(types.NodeID) error {
	return nil
}

func (*mockIStore) GetIdentity(string) (types.NodeID, error) {
	return types.NodeID{Key: "some string ", VRFPublicKey: []byte("bytes")}, nil
}

type validatorMock struct{}

func (*validatorMock) Validate(signing.PublicKey, *types.NIPST, uint64, types.Hash32) error {
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

type mockTxProcessor struct {
}

func (m mockTxProcessor) HandleTxSyncData(data []byte) error {
	return nil
}

// NewSyncWithMocks returns a syncer instance that is backed by mocks of other modules
// for use in testing
func NewSyncWithMocks(atxdbStore *database.LDBDatabase, mshdb *mesh.DB, txpool *state.TxMempool, atxpool *activation.AtxMemDB, swarm service.Service, poetDb *activation.PoetDb, conf Configuration, goldenATXID types.ATXID, expectedLayers types.LayerID, poetStorage database.Database) *Syncer {
	lg := log.NewDefault("sync_test")
	lg.Info("new sync tester")
	atxdb := activation.NewDB(atxdbStore, &mockIStore{}, mshdb, conf.LayersPerEpoch, goldenATXID, &validatorMock{}, lg.WithOptions(log.Nop))
	var msh *mesh.Mesh
	if mshdb.PersistentData() {
		lg.Info("persistent data found")
		msh = mesh.NewRecoveredMesh(mshdb, atxdb, configTst(), &meshValidatorMock{}, txpool, &mockState{}, lg)
	} else {
		lg.Info("no persistent data found")
		msh = mesh.NewMesh(mshdb, atxdb, configTst(), &meshValidatorMock{}, txpool, &mockState{}, lg)
	}

	clock := mockClock{Layer: expectedLayers + 1}
	lg.Info("current layer %v", clock.GetCurrentLayer())

	blockHandler := blocks.NewBlockHandler(blocks.Config{Depth: 10}, msh, blockEligibilityValidatorMock{}, lg)

	fCfg := fetch.DefaultConfig()
	fetcher := fetch.NewFetch(context.TODO(), fCfg, swarm, lg)

	lCfg := layerfetcher.Config{RequestTimeout: 20}
	layerFetch := layerfetcher.NewLogic(context.TODO(), lCfg, blockHandler, atxdb, poetDb, atxdb, mockTxProcessor{}, swarm, fetcher, msh, lg)
	layerFetch.AddDBs(mshdb.Blocks(), atxdbStore, mshdb.Transactions(), poetStorage, mshdb.InputVector())
	layerFetch.Start()
	return NewSync(context.TODO(), swarm, msh, txpool, atxdb, blockEligibilityValidatorMock{}, poetDb, conf, &clock, layerFetch, lg)
}
