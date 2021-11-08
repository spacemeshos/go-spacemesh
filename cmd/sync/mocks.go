package main

import (
	"context"
	"time"

	"github.com/golang/protobuf/ptypes/duration"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mempool"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

type blockEligibilityValidatorMock struct{}

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

func (m *meshValidatorMock) LatestComplete() types.LayerID { return m.vl }
func (m *meshValidatorMock) Persist(context.Context) error { return nil }
func (m *meshValidatorMock) HandleIncomingLayer(_ context.Context, lyr types.LayerID) (types.LayerID, types.LayerID, bool) {
	m.countValidate++
	m.calls++
	m.vl = lyr
	if m.validatedLayers == nil {
		m.validatedLayers = make(map[types.LayerID]struct{})
	}
	m.validatedLayers[lyr] = struct{}{}
	time.Sleep(m.delay)
	return lyr, lyr.Sub(1), false
}

func (m *meshValidatorMock) HandleLateBlocks(_ context.Context, blocks []*types.Block) (types.LayerID, types.LayerID) {
	return blocks[0].Layer(), blocks[0].Layer().Sub(1)
}

type mockState struct{}

func (s mockState) ValidateAndAddTxToPool(_ *types.Transaction) error     { panic("implement me") }
func (s mockState) LoadState(types.LayerID) error                         { return nil }
func (s mockState) GetStateRoot() types.Hash32                            { return [32]byte{} }
func (mockState) ValidateNonceAndBalance(*types.Transaction) error        { panic("implement me") }
func (mockState) GetLayerStateRoot(_ types.LayerID) (types.Hash32, error) { panic("implement me") }
func (mockState) GetLayerApplied(types.TransactionID) *types.LayerID      { panic("implement me") }
func (s *mockState) ApplyLayer(l types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
	return make([]*types.Transaction, 0), nil
}
func (mockState) GetBalance(types.Address) uint64                       { panic("implement me") }
func (mockState) GetNonce(types.Address) uint64                         { panic("implement me") }
func (mockState) AddressExists(types.Address) bool                      { return true }
func (mockState) GetAllAccounts() (*types.MultipleAccountsState, error) { panic("implement me") }

type mockIStore struct{}

func (*mockIStore) StoreNodeIdentity(types.NodeID) error { return nil }
func (*mockIStore) GetIdentity(string) (types.NodeID, error) {
	return types.NodeID{Key: "some string ", VRFPublicKey: []byte("bytes")}, nil
}

type validatorMock struct{}

func (*validatorMock) Validate(signing.PublicKey, *types.NIPost, types.Hash32, uint) error {
	return nil
}
func (*validatorMock) ValidatePost([]byte, *types.Post, *types.PostMetadata, uint) error { return nil }

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
func (m *mockClock) GetCurrentLayer() types.LayerID { return m.Layer }
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
		BaseReward: 5000,
	}
}

type mockTxProcessor struct{}

func (m mockTxProcessor) HandleSyncTransaction(_ []byte) error { return nil }

type mockBlockFetcher struct{}

func (m *mockBlockFetcher) FetchBlock(context.Context, types.BlockID) error  { return nil }
func (m *mockBlockFetcher) GetBlocks(context.Context, []types.BlockID) error { return nil }

type allDbs struct {
	atxdb       *activation.DB
	atxdbStore  *database.LDBDatabase
	poetDb      *activation.PoetDb
	poetStorage database.Database
	mshdb       *mesh.DB
}

func createMeshWithMock(dbs *allDbs, txpool *mempool.TxMempool, lg log.Log) *mesh.Mesh {
	var msh *mesh.Mesh
	if dbs.mshdb.PersistentData() {
		lg.Info("persistent data found")
		msh = mesh.NewRecoveredMesh(context.TODO(), dbs.mshdb, dbs.atxdb, configTst(), &mockBlockFetcher{}, &meshValidatorMock{}, txpool, &mockState{}, lg)
	} else {
		lg.Info("no persistent data found")
		msh = mesh.NewMesh(dbs.mshdb, dbs.atxdb, configTst(), &mockBlockFetcher{}, &meshValidatorMock{}, txpool, &mockState{}, lg)
	}
	return msh
}

func createSyncer(conf syncer.Configuration, msh *mesh.Mesh, layerFetch *layerfetcher.Logic, expectedLayers types.LayerID, lg log.Log) *syncer.Syncer {
	clock := mockClock{Layer: expectedLayers.Add(1)}
	lg.Info("current layer %v", clock.GetCurrentLayer())
	return syncer.NewSyncer(context.TODO(), conf, &clock, msh, layerFetch, layerpatrol.New(), lg)
}
