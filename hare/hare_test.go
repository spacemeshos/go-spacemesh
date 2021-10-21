package hare

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks"
	bMocks "github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/lp2p"
	"github.com/spacemeshos/go-spacemesh/lp2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/lp2p/pubsub/mocks"
	signing2 "github.com/spacemeshos/go-spacemesh/signing"
)

type mockReport struct {
	id       types.LayerID
	set      *Set
	c        bool
	coinflip bool
}

func (m mockReport) ID() types.LayerID {
	return m.id
}

func (m mockReport) Set() *Set {
	return m.set
}

func (m mockReport) Completed() bool {
	return m.c
}

func (m mockReport) Coinflip() bool {
	return m.coinflip
}

type mockConsensusProcess struct {
	util.Closer
	t    chan TerminationOutput
	id   types.LayerID
	term chan struct{}
	set  *Set
}

func (mcp *mockConsensusProcess) Start(context.Context) error {
	if mcp.term != nil {
		<-mcp.term
	}
	mcp.Close()
	mcp.t <- mockReport{mcp.id, mcp.set, true, false}
	return nil
}

func (mcp *mockConsensusProcess) ID() types.LayerID {
	return mcp.id
}

func (mcp *mockConsensusProcess) SetInbox(chan *Msg) {
}

type mockIDProvider struct {
	err error
}

func (mip *mockIDProvider) GetIdentity(edID string) (types.NodeID, error) {
	return types.NodeID{Key: edID, VRFPublicKey: []byte{}}, mip.err
}

func noopPubSub(tb testing.TB) pubsub.PublishSubsciber {
	tb.Helper()
	ctrl := gomock.NewController(tb)
	publisher := pubsubmocks.NewMockPublishSubsciber(ctrl)
	publisher.EXPECT().
		Publish(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()
	publisher.EXPECT().
		Register(gomock.Any(), gomock.Any()).
		AnyTimes()
	return publisher
}

func newMockConsensusProcess(_ config.Config, instanceID types.LayerID, s *Set, _ Rolacle, _ Signer, _ pubsub.Publisher, outputChan chan TerminationOutput) *mockConsensusProcess {
	mcp := new(mockConsensusProcess)
	mcp.Closer = util.NewCloser()
	mcp.id = instanceID
	mcp.t = outputChan
	mcp.set = s
	return mcp
}

func randomBytes(t *testing.T, size int) []byte {
	data, err := crypto.GetRandomBytes(size)
	require.NoError(t, err)
	return data
}

func randomBlock(t *testing.T, lyrID types.LayerID, beacon []byte) *types.Block {
	block := types.NewExistingBlock(lyrID, randomBytes(t, 4), nil)
	block.TortoiseBeacon = beacon
	return block
}

func createHare(t *testing.T, id lp2p.Peer, ps pubsub.PublishSubsciber, msh meshProvider, beacons blocks.BeaconGetter, clock *mockClock, logger log.Log) *Hare {
	ctrl := gomock.NewController(t)
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(gomock.Any()).AnyTimes()
	return New(cfg, id, ps, signing2.NewEdSigner(), types.NodeID{}, (&mockSyncer{true}).IsSynced, msh, beacons, eligibility.New(logger), patrol, 10, &mockIDProvider{}, NewMockStateQuerier(), clock, logger)
}

var _ Consensus = (*mockConsensusProcess)(nil)

func TestHare_New(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := logtest.New(t).WithName(t.Name())
	h := New(cfg, "", noopPubSub(t), signing2.NewEdSigner(), types.NodeID{}, (&mockSyncer{true}).IsSynced,
		mocks.NewMockmeshProvider(ctrl), bMocks.NewMockBeaconGetter(ctrl), eligibility.New(logger), mocks.NewMocklayerPatrol(ctrl), 10,
		&mockIDProvider{}, NewMockStateQuerier(), newMockClock(), logger)
	assert.NotNil(t, h)
}

func TestHare_Start(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	h := createHare(t, "", noopPubSub(t), mockMesh, mockBeacons, newMockClock(), logtest.New(t).WithName(t.Name()))

	assert.NoError(t, h.Start(context.TODO()))
	t.Cleanup(func() {
		h.Close()
	})
}

func TestHare_collectOutputAndGetResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	h := createHare(t, "", noopPubSub(t), mockMesh, mockBeacons, newMockClock(), logtest.New(t).WithName(t.Name()))

	res, err := h.GetResult(types.NewLayerID(0))
	assert.Equal(t, errNoResult, err)
	assert.Nil(t, res)

	lyrID := types.NewLayerID(10)
	set := NewSetFromValues(value1)

	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), lyrID, gomock.Any()).Times(1)
	require.NoError(t, h.collectOutput(context.TODO(), mockReport{lyrID, set, true, false}))

	res, err = h.GetResult(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, value1.Bytes(), res[0].Bytes())

	res, err = h.GetResult(lyrID.Add(1))
	assert.Equal(t, errNoResult, err)
	assert.Empty(t, res)
}

func TestHare_collectOutputGetResult_TerminateTooLate(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	h := createHare(t, "", noopPubSub(t), mockMesh, mockBeacons, newMockClock(), logtest.New(t).WithName(t.Name()))

	lyrID := types.NewLayerID(10)
	res, err := h.GetResult(lyrID)
	assert.Equal(t, errNoResult, err)
	assert.Nil(t, res)

	h.layerLock.Lock()
	h.lastLayer = lyrID.Add(h.bufferSize + 1)
	h.layerLock.Unlock()
	set := NewSetFromValues(value1)

	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), lyrID, gomock.Any()).Times(1)
	err = h.collectOutput(context.TODO(), mockReport{lyrID, set, true, false})
	assert.Equal(t, ErrTooLate, err)

	res, err = h.GetResult(lyrID)
	assert.Equal(t, err, errTooOld)
	assert.Empty(t, res)
}

func TestHare_OutputCollectionLoop(t *testing.T) {
	types.SetLayersPerEpoch(4)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	h := createHare(t, "", noopPubSub(t), mockMesh, mockBeacons, newMockClock(), logtest.New(t).WithName(t.Name()))
	require.NoError(t, h.Start(context.TODO()))

	lyrID := types.NewLayerID(8)
	mo := mockReport{lyrID, NewEmptySet(0), true, false}
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), lyrID, gomock.Any()).Times(1)
	_, err := h.broker.Register(context.TODO(), mo.ID())
	require.NoError(t, err)
	time.Sleep(1 * time.Second)
	h.outputChan <- mo
	time.Sleep(1 * time.Second)
	assert.Nil(t, h.broker.outbox[mo.ID().Uint32()])
}

func TestHare_onTick(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cfg := config.DefaultConfig()
	types.SetLayersPerEpoch(4)

	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1

	clock := newMockClock()

	oracle := newMockHashOracle(numOfClients)
	signing := signing2.NewEdSigner()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(types.GetEffectiveGenesis().Add(1)).Times(1)
	h := New(cfg, "", noopPubSub(t), signing, types.NodeID{}, (&mockSyncer{true}).IsSynced, mockMesh, mockBeacons, oracle, patrol, 10, &mockIDProvider{}, NewMockStateQuerier(), clock, logtest.New(t).WithName("Hare"))
	h.networkDelta = 0
	h.bufferSize = 1

	createdChan := make(chan struct{})
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}

	require.NoError(t, h.Start(context.TODO()))

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := randomBytes(t, 32)
	blockSet := []*types.Block{
		randomBlock(t, lyrID, beacon),
		randomBlock(t, lyrID, beacon),
		randomBlock(t, lyrID, beacon),
	}
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(beacon, nil).Times(1)
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	mockMesh.EXPECT().LayerBlocks(lyrID).Return(blockSet, nil).Times(1)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), lyrID, gomock.Any()).Times(1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		<-createdChan
		<-nmcp.CloseChannel()
		wg.Done()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	res1, err := h.GetResult(types.GetEffectiveGenesis().Add(1))
	assert.NoError(t, err)
	assert.Equal(t, types.SortBlockIDs(types.BlockIDs(blockSet)), types.SortBlockIDs(res1))

	lyrID = lyrID.Add(1)
	// consensus process is closed, should not process any tick
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		h.Close()
		wg.Done()
	}()

	// collect output one more time
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	res2, err := h.GetResult(lyrID)
	assert.Equal(t, errNoResult, err)
	assert.Empty(t, res2)
}

func TestHare_onTick_BeaconFromRefBlocks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cfg := config.DefaultConfig()
	types.SetLayersPerEpoch(4)

	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1

	clock := newMockClock()
	clock.advanceLayer() // we want to start at genesis + 2 instead of 1
	lyrID := types.GetEffectiveGenesis().Add(2)

	oracle := newMockHashOracle(numOfClients)
	signing := signing2.NewEdSigner()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)

	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(lyrID).Times(1)
	h := New(cfg, "", noopPubSub(t), signing, types.NodeID{}, (&mockSyncer{true}).IsSynced, mockMesh, mockBeacons, oracle, patrol, 10, &mockIDProvider{}, NewMockStateQuerier(), clock, logtest.New(t).WithName("Hare"))
	h.networkDelta = 0
	h.bufferSize = 1

	createdChan := make(chan struct{})
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}

	require.NoError(t, h.Start(context.TODO()))
	t.Cleanup(func() {
		h.Close()
	})

	epochBeacon := randomBytes(t, 32)
	blockSet := []*types.Block{
		randomBlock(t, lyrID, epochBeacon),
		randomBlock(t, lyrID, nil),
		randomBlock(t, lyrID, epochBeacon),
	}
	refBlock := randomBlock(t, lyrID.Sub(1), epochBeacon)
	bID := refBlock.ID()
	blockSet[1].RefBlock = &bID
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	mockMesh.EXPECT().LayerBlocks(lyrID).Return(blockSet, nil).Times(1)
	mockMesh.EXPECT().GetBlock(bID).Return(refBlock, nil).Times(1)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), lyrID, gomock.Any()).Times(1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		<-createdChan
		<-nmcp.CloseChannel()
		wg.Done()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	res, err := h.GetResult(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, types.SortBlockIDs(types.BlockIDs(blockSet)), types.SortBlockIDs(res))
}

func TestHare_onTick_SomeBadBlocks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cfg := config.DefaultConfig()
	types.SetLayersPerEpoch(4)

	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1

	clock := newMockClock()

	oracle := newMockHashOracle(numOfClients)
	signing := signing2.NewEdSigner()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(types.GetEffectiveGenesis().Add(1)).Times(1)
	h := New(cfg, "", noopPubSub(t), signing, types.NodeID{}, (&mockSyncer{true}).IsSynced, mockMesh, mockBeacons, oracle, patrol, 10, &mockIDProvider{}, NewMockStateQuerier(), clock, logtest.New(t).WithName("Hare"))
	h.networkDelta = 0
	h.bufferSize = 1

	createdChan := make(chan struct{})
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}

	require.NoError(t, h.Start(context.TODO()))
	t.Cleanup(func() {
		h.Close()
	})

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := randomBytes(t, 32)
	epochBeacon := randomBytes(t, 32)
	blockSet := []*types.Block{
		randomBlock(t, lyrID, epochBeacon),
		randomBlock(t, lyrID, beacon),
		randomBlock(t, lyrID, epochBeacon),
	}
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	mockMesh.EXPECT().LayerBlocks(lyrID).Return(blockSet, nil).Times(1)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), lyrID, gomock.Any()).Times(1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		<-createdChan
		<-nmcp.CloseChannel()
		wg.Done()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	res, err := h.GetResult(lyrID)
	assert.NoError(t, err)
	goodBlocks := []*types.Block{blockSet[0], blockSet[2]}
	assert.Equal(t, types.SortBlockIDs(types.BlockIDs(goodBlocks)), types.SortBlockIDs(res))
}

func TestHare_onTick_NoGoodBlocks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cfg := config.DefaultConfig()
	types.SetLayersPerEpoch(4)

	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1

	clock := newMockClock()

	oracle := newMockHashOracle(numOfClients)
	signing := signing2.NewEdSigner()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(types.GetEffectiveGenesis().Add(1)).Times(1)
	h := New(cfg, "", noopPubSub(t), signing, types.NodeID{}, (&mockSyncer{true}).IsSynced, mockMesh, mockBeacons, oracle,
		patrol, 10, &mockIDProvider{}, NewMockStateQuerier(), clock, logtest.New(t).WithName("Hare"))
	h.networkDelta = 0
	h.bufferSize = 1

	createdChan := make(chan struct{})
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}

	require.NoError(t, h.Start(context.TODO()))
	t.Cleanup(func() {
		h.Close()
	})

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := randomBytes(t, 32)
	epochBeacon := randomBytes(t, 32)
	blockSet := []*types.Block{
		randomBlock(t, lyrID, beacon),
		randomBlock(t, lyrID, beacon),
		randomBlock(t, lyrID, beacon),
	}
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	mockMesh.EXPECT().LayerBlocks(lyrID).Return(blockSet, nil).Times(1)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), lyrID, gomock.Any()).Times(1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		<-createdChan
		<-nmcp.CloseChannel()
		wg.Done()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	res, err := h.GetResult(lyrID)
	assert.NoError(t, err)
	assert.Empty(t, res)
}

func TestHare_onTick_NoBeacon(t *testing.T) {
	types.SetLayersPerEpoch(4)
	lyr := types.NewLayerID(199)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	moRolacle := mocks.NewMockRolacle(ctrl)
	moRolacle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyr).Return(true, nil).MaxTimes(1)

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(lyr.GetEpoch()).Return(nil, errors.New("whatever")).Times(1)

	patrol := mocks.NewMocklayerPatrol(ctrl)
	h := New(cfg, "", noopPubSub(t), nil, types.NodeID{}, (&mockSyncer{false}).IsSynced, mockMesh, mockBeacons, moRolacle,
		patrol, 10, &mockIDProvider{}, NewMockStateQuerier(), newMockClock(), logtest.New(t).WithName("Hare"))
	h.networkDelta = 0
	require.NoError(t, h.broker.Start(context.TODO()))

	started, err := h.onTick(context.TODO(), lyr)
	assert.NoError(t, err)
	assert.False(t, started)
}

func TestHare_onTick_NotSynced(t *testing.T) {
	types.SetLayersPerEpoch(4)
	lyr := types.NewLayerID(199)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	moRolacle := mocks.NewMockRolacle(ctrl)
	moRolacle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyr).Return(true, nil).MaxTimes(1)

	mp := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(lyr.GetEpoch()).Return(randomBytes(t, 32), nil).Times(1)

	patrol := mocks.NewMocklayerPatrol(ctrl)
	h := New(cfg, "", noopPubSub(t), nil, types.NodeID{}, (&mockSyncer{false}).IsSynced, mp, mockBeacons, moRolacle,
		patrol, 10, &mockIDProvider{}, NewMockStateQuerier(), newMockClock(), logtest.New(t).WithName("Hare"))
	h.networkDelta = 0
	require.NoError(t, h.broker.Start(context.TODO()))

	started, err := h.onTick(context.TODO(), lyr)
	assert.NoError(t, err)
	assert.False(t, started)
}

func TestHare_outputBuffer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	h := createHare(t, "", noopPubSub(t), mockMesh, mockBeacons, newMockClock(), logtest.New(t).WithName(t.Name()))
	lasti := types.LayerID{}

	for i := lasti; i.Before(types.NewLayerID(h.bufferSize)); i = i.Add(1) {
		h.lastLayer = i
		mockid := i
		set := NewSetFromValues(value1)
		_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
		_, ok := h.outputs[mockid]
		require.True(t, ok)
		require.EqualValues(t, i.Add(1).Uint32(), len(h.outputs))
		lasti = i
	}

	require.EqualValues(t, h.bufferSize, len(h.outputs))

	// add another output
	mockid := lasti.Add(1)
	set := NewSetFromValues(value1)
	require.NoError(t, h.collectOutput(context.TODO(), mockReport{mockid, set, true, false}))
	_, ok := h.outputs[mockid]
	require.True(t, ok)
	require.EqualValues(t, h.bufferSize, len(h.outputs))
}

func TestHare_IsTooLate(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	h := createHare(t, "", noopPubSub(t), mockMesh, mockBeacons, newMockClock(), logtest.New(t).WithName(t.Name()))

	for i := (types.LayerID{}); i.Before(types.NewLayerID(h.bufferSize * 2)); i = i.Add(1) {
		mockid := i
		set := NewSetFromValues(value1)
		h.lastLayer = i
		_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
		_, ok := h.outputs[mockid]
		require.True(t, ok)
		exp := i.Add(1).Uint32()
		if exp > h.bufferSize {
			exp = h.bufferSize
		}

		require.EqualValues(t, exp, len(h.outputs))
	}

	require.True(t, h.outOfBufferRange(types.NewLayerID(1)))
}

func TestHare_oldestInBuffer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	h := createHare(t, "", noopPubSub(t), mockMesh, mockBeacons, newMockClock(), logtest.New(t).WithName(t.Name()))
	lasti := types.LayerID{}

	for i := lasti; i.Before(types.NewLayerID(h.bufferSize)); i = i.Add(1) {
		mockid := i
		set := NewSetFromValues(value1)
		h.lastLayer = i
		_ = h.collectOutput(context.TODO(), mockReport{mockid, set, true, false})
		_, ok := h.outputs[mockid]
		require.True(t, ok)
		exp := i.Add(1).Uint32()
		if exp > h.bufferSize {
			exp = h.bufferSize
		}

		require.EqualValues(t, exp, len(h.outputs))
		lasti = i
	}

	lyr := h.oldestResultInBuffer()
	require.Equal(t, types.NewLayerID(0), lyr)

	mockid := lasti.Add(1)
	set := NewSetFromValues(value1)
	h.lastLayer = lasti.Add(1)
	require.NoError(t, h.collectOutput(context.TODO(), mockReport{mockid, set, true, false}))
	_, ok := h.outputs[mockid]
	require.True(t, ok)
	require.EqualValues(t, h.bufferSize, len(h.outputs))

	lyr = h.oldestResultInBuffer()
	require.Equal(t, types.NewLayerID(1), lyr)

	mockid = lasti.Add(2)
	set = NewSetFromValues(value1)
	h.lastLayer = lasti.Add(2)
	require.NoError(t, h.collectOutput(context.TODO(), mockReport{mockid, set, true, false}))
	_, ok = h.outputs[mockid]
	require.True(t, ok)
	require.EqualValues(t, h.bufferSize, len(h.outputs))

	lyr = h.oldestResultInBuffer()
	require.Equal(t, types.NewLayerID(2), lyr)
}

// make sure that Hare writes a weak coin value for a layer to the mesh after the CP completes,
// regardless of whether it succeeds or fails.
func TestHare_WeakCoin(t *testing.T) {
	r := require.New(t)

	layerID := types.NewLayerID(10)

	done := make(chan struct{})
	oracle := newMockHashOracle(numOfClients)
	signing := signing2.NewEdSigner()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMesh := mocks.NewMockmeshProvider(ctrl)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	patrol := mocks.NewMocklayerPatrol(ctrl)
	h := New(cfg, "", noopPubSub(t), signing, types.NodeID{}, (&mockSyncer{true}).IsSynced, mockMesh, mockBeacons, oracle,
		patrol, 10, &mockIDProvider{}, NewMockStateQuerier(), newMockClock(), logtest.New(t).WithName("Hare"))
	defer h.Close()
	h.lastLayer = layerID
	set := NewSetFromValues(value1)

	require.NoError(t, h.Start(context.TODO()))
	waitForMsg := func() {
		tmr := time.NewTimer(time.Second)
		select {
		case <-tmr.C:
			r.Fail("timed out waiting for message")
		case <-done:
		}
	}

	// complete + coin flip true
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), layerID, true).Times(1)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), layerID, gomock.Any()).Do(
		func(context.Context, types.LayerID, []types.BlockID) {
			done <- struct{}{}
		}).Times(1)
	h.outputChan <- mockReport{layerID, set, true, true}
	waitForMsg()

	// incomplete + coin flip true
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), layerID, true).Times(1)
	mockMesh.EXPECT().InvalidateLayer(gomock.Any(), layerID).Do(
		func(context.Context, types.LayerID) {
			done <- struct{}{}
		}).Times(1)
	h.outputChan <- mockReport{layerID, set, false, true}
	waitForMsg()

	// complete + coin flip false
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), layerID, false).Times(1)
	mockMesh.EXPECT().HandleValidatedLayer(gomock.Any(), layerID, gomock.Any()).Do(
		func(context.Context, types.LayerID, []types.BlockID) {
			done <- struct{}{}
		}).Times(1)
	h.outputChan <- mockReport{layerID, set, true, false}
	waitForMsg()

	// incomplete + coin flip false
	mockMesh.EXPECT().RecordCoinflip(gomock.Any(), layerID, false).Times(1)
	mockMesh.EXPECT().InvalidateLayer(gomock.Any(), layerID).Do(
		func(context.Context, types.LayerID) {
			done <- struct{}{}
		}).Times(1)
	h.outputChan <- mockReport{layerID, set, false, false}
	waitForMsg()
}
