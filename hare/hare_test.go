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

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
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

var _ Consensus = (*mockConsensusProcess)(nil)

func newMockConsensusProcess(_ config.Config, instanceID types.LayerID, s *Set, _ Rolacle, _ Signer, _ pubsub.Publisher, outputChan chan TerminationOutput) *mockConsensusProcess {
	mcp := new(mockConsensusProcess)
	mcp.Closer = util.NewCloser()
	mcp.id = instanceID
	mcp.t = outputChan
	mcp.set = s
	return mcp
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

func randomProposal(lyrID types.LayerID, beacon types.Beacon) *types.Proposal {
	p := types.GenLayerProposal(lyrID, nil)
	p.Ballot.RefBallot = types.EmptyBallotID
	p.Ballot.EpochData = &types.EpochData{
		Beacon: beacon,
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
	p.Signature = signer.Sign(p.Bytes())
	p.Initialize()
	return p
}

func TestHare_New(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := logtest.New(t).WithName(t.Name())
	h := New(cfg, "", noopPubSub(t), signing.NewEdSigner(), types.NodeID{}, mocks.NewMockblockGenerator(ctrl), smocks.NewMockSyncStateProvider(ctrl),
		mocks.NewMockmeshProvider(ctrl), mocks.NewMockproposalProvider(ctrl), smocks.NewMockBeaconGetter(ctrl), smocks.NewMockProposalFetcher(ctrl),
		eligibility.New(logger), mocks.NewMocklayerPatrol(ctrl), 10, mocks.NewMockstateQuerier(ctrl), newMockClock(), logger)
	assert.NotNil(t, h)
}

func TestHare_Start(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())
	assert.NoError(t, h.Start(context.TODO()))
	h.Close()
}

func TestHare_collectOutputAndGetResult(t *testing.T) {
	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())

	lyrID := types.NewLayerID(10)
	res, err := h.getResult(lyrID)
	assert.Equal(t, errNoResult, err)
	assert.Nil(t, res)

	proposals := []*types.Proposal{
		types.GenLayerProposal(lyrID, nil),
		types.GenLayerProposal(lyrID, nil),
		types.GenLayerProposal(lyrID, nil),
	}
	proposalIDs := types.ToProposalIDs(proposals)
	set := NewSetFromValues(proposalIDs...)
	block := types.GenLayerBlock(lyrID, nil)

	h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any(), p2p.AnyPeer).DoAndReturn(
		func(ctx context.Context, pids []types.ProposalID, _ p2p.Peer) error {
			assert.ElementsMatch(t, proposalIDs, pids)
			return nil
		}).Times(1)
	h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
		func(pids []types.ProposalID) ([]*types.Proposal, error) {
			assert.ElementsMatch(t, proposalIDs, pids)
			return proposals, nil
		}).Times(1)
	h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), lyrID, proposals).Return(block, nil).Times(1)
	h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyrID, block.ID()).Times(1)
	require.NoError(t, h.collectOutput(context.TODO(), mockReport{lyrID, set, true, false}))

	res, err = h.getResult(lyrID)
	assert.NoError(t, err)
	assert.ElementsMatch(t, proposalIDs, res)

	res, err = h.getResult(lyrID.Add(1))
	assert.Equal(t, errNoResult, err)
	assert.Empty(t, res)
}

func TestHare_collectOutputGetResult_TerminateTooLate(t *testing.T) {
	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())

	lyrID := types.NewLayerID(10)
	res, err := h.getResult(lyrID)
	assert.Equal(t, errNoResult, err)
	assert.Nil(t, res)

	h.layerLock.Lock()
	h.lastLayer = lyrID.Add(h.bufferSize + 1)
	h.layerLock.Unlock()

	proposals := []*types.Proposal{
		types.GenLayerProposal(lyrID, nil),
		types.GenLayerProposal(lyrID, nil),
		types.GenLayerProposal(lyrID, nil),
	}
	proposalIDs := types.ToProposalIDs(proposals)
	set := NewSetFromValues(proposalIDs...)
	block := types.GenLayerBlock(lyrID, nil)

	h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any(), p2p.AnyPeer).DoAndReturn(
		func(ctx context.Context, pids []types.ProposalID, _ p2p.Peer) error {
			assert.ElementsMatch(t, proposalIDs, pids)
			return nil
		}).Times(1)
	h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
		func(pids []types.ProposalID) ([]*types.Proposal, error) {
			assert.ElementsMatch(t, proposalIDs, pids)
			return proposals, nil
		}).Times(1)
	h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), lyrID, proposals).Return(block, nil).Times(1)
	h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyrID, block.ID()).Times(1)
	err = h.collectOutput(context.TODO(), mockReport{lyrID, set, true, false})
	assert.Equal(t, ErrTooLate, err)

	res, err = h.getResult(lyrID)
	assert.Equal(t, err, errTooOld)
	assert.Empty(t, res)
}

func TestHare_OutputCollectionLoop(t *testing.T) {
	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())
	require.NoError(t, h.Start(context.TODO()))

	lyrID := types.NewLayerID(8)
	mo := mockReport{lyrID, NewEmptySet(0), true, false}
	_, err := h.broker.Register(context.TODO(), mo.ID())
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyrID, types.EmptyBlockID).Times(1)
	h.outputChan <- mo
	time.Sleep(1 * time.Second)

	h.broker.mu.RLock()
	assert.Nil(t, h.broker.outbox[mo.ID().Uint32()])
	h.broker.mu.RUnlock()
}

func TestHare_onTick(t *testing.T) {
	types.SetLayersPerEpoch(4)
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1
	clock := newMockClock()
	h := createTestHare(t, cfg, clock, "test", noopPubSub(t), t.Name())

	h.networkDelta = 0
	h.bufferSize = 1
	createdChan := make(chan struct{})
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput, certsChan chan CertificationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}

	require.NoError(t, h.Start(context.TODO()))

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	proposals := []*types.Proposal{
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
	}
	block := types.GenLayerBlock(lyrID, nil)

	mockBeacons := smocks.NewMockBeaconGetter(h.ctrl)
	h.beacons = mockBeacons
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(beacon, nil).Times(1)
	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	h.mockProposalDB.EXPECT().LayerProposals(lyrID).Return(proposals, nil).Times(1)
	h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any(), p2p.AnyPeer).DoAndReturn(
		func(ctx context.Context, pids []types.ProposalID, _ p2p.Peer) error {
			assert.ElementsMatch(t, types.ToProposalIDs(proposals), pids)
			return nil
		}).Times(1)
	h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
		func(pids []types.ProposalID) ([]*types.Proposal, error) {
			assert.ElementsMatch(t, types.ToProposalIDs(proposals), pids)
			return proposals, nil
		}).Times(1)
	h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), lyrID, proposals).Return(block, nil).Times(1)
	h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyrID, block.ID()).Times(1)

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
	res1, err := h.getResult(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, types.SortProposalIDs(types.ToProposalIDs(proposals)), types.SortProposalIDs(res1))

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
	res2, err := h.getResult(lyrID)
	assert.Equal(t, errNoResult, err)
	assert.Empty(t, res2)
}

func TestHare_onTick_BeaconFromRefBallot(t *testing.T) {
	types.SetLayersPerEpoch(4)
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1
	clock := newMockClock()
	h := createTestHare(t, cfg, clock, "test", noopPubSub(t), t.Name())

	h.networkDelta = 0
	h.bufferSize = 1
	createdChan := make(chan struct{})
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput, certsChan chan CertificationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}

	require.NoError(t, h.Start(context.TODO()))
	defer h.Close()

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	proposals := []*types.Proposal{
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
	}
	block := types.GenLayerBlock(lyrID, nil)

	mockBeacons := smocks.NewMockBeaconGetter(h.ctrl)
	h.beacons = mockBeacons
	refBallot := &randomProposal(lyrID.Sub(1), beacon).Ballot
	proposals[1].EpochData = nil
	proposals[1].RefBallot = refBallot.ID()

	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(beacon, nil).Times(1)
	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	h.mockProposalDB.EXPECT().LayerProposals(lyrID).Return(proposals, nil).Times(1)
	h.mockMeshDB.EXPECT().GetBallot(refBallot.ID()).Return(refBallot, nil).Times(1)
	h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any(), p2p.AnyPeer).DoAndReturn(
		func(ctx context.Context, pids []types.ProposalID, _ p2p.Peer) error {
			assert.ElementsMatch(t, types.ToProposalIDs(proposals), pids)
			return nil
		}).Times(1)
	h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
		func(pids []types.ProposalID) ([]*types.Proposal, error) {
			assert.ElementsMatch(t, types.ToProposalIDs(proposals), pids)
			return proposals, nil
		}).Times(1)
	h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), lyrID, proposals).Return(block, nil).Times(1)
	h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyrID, block.ID()).Times(1)

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
	res, err := h.getResult(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, types.SortProposalIDs(types.ToProposalIDs(proposals)), types.SortProposalIDs(res))
}

func TestHare_onTick_SomeBadBallots(t *testing.T) {
	types.SetLayersPerEpoch(4)
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1
	clock := newMockClock()
	h := createTestHare(t, cfg, clock, "test", noopPubSub(t), t.Name())

	h.networkDelta = 0
	h.bufferSize = 1
	createdChan := make(chan struct{})
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput, certsChan chan CertificationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}

	require.NoError(t, h.Start(context.TODO()))
	defer h.Close()

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	epochBeacon := types.RandomBeacon()
	proposals := []*types.Proposal{
		randomProposal(lyrID, epochBeacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, epochBeacon),
	}
	goodProposals := []*types.Proposal{proposals[0], proposals[2]}
	block := types.GenLayerBlock(lyrID, nil)

	mockBeacons := smocks.NewMockBeaconGetter(h.ctrl)
	h.beacons = mockBeacons
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	h.mockProposalDB.EXPECT().LayerProposals(lyrID).Return(proposals, nil).Times(1)
	h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any(), p2p.AnyPeer).DoAndReturn(
		func(ctx context.Context, pids []types.ProposalID, _ p2p.Peer) error {
			assert.ElementsMatch(t, types.ToProposalIDs(goodProposals), pids)
			return nil
		}).Times(1)
	h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
		func(pids []types.ProposalID) ([]*types.Proposal, error) {
			assert.ElementsMatch(t, types.ToProposalIDs(goodProposals), pids)
			return goodProposals, nil
		}).Times(1)
	h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), lyrID, goodProposals).Return(block, nil).Times(1)
	h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyrID, block.ID()).Times(1)

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
	res, err := h.getResult(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, types.SortProposalIDs(types.ToProposalIDs(goodProposals)), types.SortProposalIDs(res))
}

func TestHare_onTick_NoGoodBallots(t *testing.T) {
	types.SetLayersPerEpoch(4)
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1
	clock := newMockClock()
	h := createTestHare(t, cfg, clock, "test", noopPubSub(t), t.Name())

	h.networkDelta = 0
	h.bufferSize = 1
	createdChan := make(chan struct{})
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput, certsChan chan CertificationOutput) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan)
		createdChan <- struct{}{}
		return nmcp
	}

	require.NoError(t, h.Start(context.TODO()))
	defer h.Close()

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	epochBeacon := types.RandomBeacon()
	proposals := []*types.Proposal{
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
	}

	mockBeacons := smocks.NewMockBeaconGetter(h.ctrl)
	h.beacons = mockBeacons
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), lyrID, false).Times(1)
	h.mockProposalDB.EXPECT().LayerProposals(lyrID).Return(proposals, nil).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyrID, types.EmptyBlockID).Return(nil).Times(1)

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
	res, err := h.getResult(lyrID)
	assert.NoError(t, err)
	assert.Empty(t, res)
}

func TestHare_onTick_NoBeacon(t *testing.T) {
	types.SetLayersPerEpoch(4)
	lyr := types.NewLayerID(199)

	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())
	mockBeacons := smocks.NewMockBeaconGetter(h.ctrl)
	h.beacons = mockBeacons
	mockBeacons.EXPECT().GetBeacon(lyr.GetEpoch()).Return(types.EmptyBeacon, errors.New("whatever")).Times(1)
	require.NoError(t, h.broker.Start(context.TODO()))

	started, err := h.onTick(context.TODO(), lyr)
	assert.NoError(t, err)
	assert.False(t, started)
}

func TestHare_onTick_NotSynced(t *testing.T) {
	types.SetLayersPerEpoch(4)
	lyr := types.NewLayerID(199)

	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyr).Return(true, nil).AnyTimes()
	mockSyncS := smocks.NewMockSyncStateProvider(h.ctrl)
	h.broker.nodeSyncState = mockSyncS
	require.NoError(t, h.broker.Start(context.TODO()))

	mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(false).Times(1)
	started, err := h.onTick(context.TODO(), lyr)
	assert.NoError(t, err)
	assert.False(t, started)
}

func TestHare_outputBuffer(t *testing.T) {
	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.bufferSize; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyr, types.EmptyBlockID).Return(nil).Times(1)
		require.NoError(t, h.collectOutput(context.TODO(), mockReport{lyr, NewEmptySet(0), true, false}))
		_, ok := h.outputs[lyr]
		require.True(t, ok)
		require.EqualValues(t, i, len(h.outputs))
	}

	require.EqualValues(t, h.bufferSize, len(h.outputs))

	// add another output
	lyr = lyr.Add(1)
	h.setLastLayer(lyr)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyr, types.EmptyBlockID).Return(nil).Times(1)
	require.NoError(t, h.collectOutput(context.TODO(), mockReport{lyr, NewEmptySet(0), true, false}))
	_, ok := h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.bufferSize, len(h.outputs))

	// make sure the oldest layer is no longer there
	_, ok = h.outputs[lyr.Sub(h.bufferSize)]
	assert.False(t, ok)
}

func TestHare_IsTooLate(t *testing.T) {
	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.bufferSize*2; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyr, types.EmptyBlockID).Return(nil).Times(1)
		_ = h.collectOutput(context.TODO(), mockReport{lyr, NewEmptySet(0), true, false})
		_, ok := h.outputs[lyr]
		assert.True(t, ok)
		if i < h.bufferSize {
			assert.EqualValues(t, i, len(h.outputs))
		} else {
			assert.EqualValues(t, h.bufferSize, len(h.outputs))
		}
	}

	for i := uint32(1); i <= h.bufferSize; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		require.True(t, h.outOfBufferRange(types.GetEffectiveGenesis().Add(1)))
	}
	assert.False(t, h.outOfBufferRange(lyr.Add(1)))
}

func TestHare_oldestInBuffer(t *testing.T) {
	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.bufferSize; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyr, types.EmptyBlockID).Return(nil).Times(1)
		require.NoError(t, h.collectOutput(context.TODO(), mockReport{lyr, NewEmptySet(0), true, false}))
		_, ok := h.outputs[lyr]
		require.True(t, ok)
		require.EqualValues(t, i, len(h.outputs))
	}

	require.Equal(t, types.GetEffectiveGenesis().Add(1), h.oldestResultInBuffer())

	lyr = lyr.Add(1)
	h.setLastLayer(lyr)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyr, types.EmptyBlockID).Return(nil).Times(1)
	require.NoError(t, h.collectOutput(context.TODO(), mockReport{lyr, NewEmptySet(0), true, false}))
	_, ok := h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.bufferSize, len(h.outputs))

	require.Equal(t, types.GetEffectiveGenesis().Add(2), h.oldestResultInBuffer())

	lyr = lyr.Add(2)
	h.setLastLayer(lyr)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyr, types.EmptyBlockID).Return(nil).Times(1)
	require.NoError(t, h.collectOutput(context.TODO(), mockReport{lyr, NewEmptySet(0), true, false}))
	_, ok = h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.bufferSize, len(h.outputs))

	require.Equal(t, types.GetEffectiveGenesis().Add(3), h.oldestResultInBuffer())
}

// make sure that Hare writes a weak coin value for a layer to the mesh after the CP completes,
// regardless of whether it succeeds or fails.
func TestHare_WeakCoin(t *testing.T) {
	h := createTestHare(t, config.DefaultConfig(), newMockClock(), "test", noopPubSub(t), t.Name())
	layerID := types.NewLayerID(10)
	h.setLastLayer(layerID)

	done := make(chan struct{})
	require.NoError(t, h.Start(context.TODO()))
	defer h.Close()
	waitForMsg := func() {
		tmr := time.NewTimer(time.Second)
		select {
		case <-tmr.C:
			require.Fail(t, "timed out waiting for message")
		case <-done:
		}
	}

	proposals := []*types.Proposal{
		randomProposal(layerID, types.EmptyBeacon),
		randomProposal(layerID, types.EmptyBeacon),
		randomProposal(layerID, types.EmptyBeacon),
	}
	set := NewSetFromValues(types.ToProposalIDs(proposals)...)
	block := types.GenLayerBlock(layerID, nil)

	// complete + coin flip true
	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), layerID, true).Times(1)
	h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any(), p2p.AnyPeer).DoAndReturn(
		func(ctx context.Context, pids []types.ProposalID, _ p2p.Peer) error {
			assert.ElementsMatch(t, types.ToProposalIDs(proposals), pids)
			return nil
		}).Times(1)
	h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
		func(pids []types.ProposalID) ([]*types.Proposal, error) {
			assert.ElementsMatch(t, types.ToProposalIDs(proposals), pids)
			return proposals, nil
		}).Times(1)
	h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), layerID, proposals).Return(block, nil).Times(1)
	h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID()).DoAndReturn(
		func(context.Context, types.LayerID, types.BlockID) error {
			done <- struct{}{}
			return nil
		}).Times(1)
	h.outputChan <- mockReport{layerID, set, true, true}
	waitForMsg()

	// incomplete + coin flip true
	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), layerID, true).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, types.EmptyBlockID).DoAndReturn(
		func(context.Context, types.LayerID, types.BlockID) error {
			done <- struct{}{}
			return nil
		}).Times(1)
	h.outputChan <- mockReport{layerID, set, false, true}
	waitForMsg()

	// complete + coin flip false
	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), layerID, false).Times(1)
	h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any(), p2p.AnyPeer).DoAndReturn(
		func(ctx context.Context, pids []types.ProposalID, _ p2p.Peer) error {
			assert.ElementsMatch(t, types.ToProposalIDs(proposals), pids)
			return nil
		}).Times(1)
	h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
		func(pids []types.ProposalID) ([]*types.Proposal, error) {
			assert.ElementsMatch(t, types.ToProposalIDs(proposals), pids)
			return proposals, nil
		}).Times(1)
	h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), layerID, proposals).Return(block, nil).Times(1)
	h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID()).DoAndReturn(
		func(context.Context, types.LayerID, types.BlockID) error {
			done <- struct{}{}
			return nil
		}).Times(1)
	h.outputChan <- mockReport{layerID, set, true, false}
	waitForMsg()

	// incomplete + coin flip false
	h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), layerID, true).Times(1)
	h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, types.EmptyBlockID).DoAndReturn(
		func(context.Context, types.LayerID, types.BlockID) error {
			done <- struct{}{}
			return nil
		}).Times(1)
	h.outputChan <- mockReport{layerID, set, false, true}
	waitForMsg()
}
