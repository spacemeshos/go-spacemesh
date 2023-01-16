package hare

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
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
	started chan struct{}
	t       chan TerminationOutput
	id      types.LayerID
	set     *Set
}

func (mcp *mockConsensusProcess) Start() error {
	close(mcp.started)
	mcp.t <- mockReport{mcp.id, mcp.set, true, false}
	return nil
}

func (mcp *mockConsensusProcess) ID() types.LayerID {
	return mcp.id
}

func (mcp *mockConsensusProcess) SetInbox(chan *Msg) {
}

var _ Consensus = (*mockConsensusProcess)(nil)

func newMockConsensusProcess(_ config.Config, instanceID types.LayerID, s *Set, _ Rolacle, _ Signer, _ pubsub.Publisher, outputChan chan TerminationOutput, started chan struct{}) *mockConsensusProcess {
	mcp := new(mockConsensusProcess)
	mcp.started = started
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
	p := genLayerProposal(lyrID, nil)
	p.Ballot.RefBallot = types.EmptyBallotID
	p.Ballot.EpochData = &types.EpochData{
		Beacon: beacon,
	}
	signer, _ := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	p.TxIDs = []types.TransactionID{}
	p.Initialize()
	return p
}

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(4)
	instanceID0 = types.GetEffectiveGenesis()
	instanceID1 = instanceID0.Add(1)
	instanceID2 = instanceID0.Add(2)
	instanceID3 = instanceID0.Add(3)
	instanceID4 = instanceID0.Add(4)
	instanceID5 = instanceID0.Add(5)
	instanceID6 = instanceID0.Add(6)

	res := m.Run()
	os.Exit(res)
}

func TestHare_New(t *testing.T) {
	ctrl := gomock.NewController(t)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := logtest.New(t).WithName(t.Name())
	cfg := config.Config{N: 10, F: 5, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 1000, Hdist: 20}
	h := New(datastore.NewCachedDB(sql.InMemory(), logtest.New(t)), cfg, noopPubSub(t), signer, types.NodeID{}, make(chan LayerOutput, 1),
		smocks.NewMockSyncStateProvider(ctrl), smocks.NewMockBeaconGetter(ctrl),
		eligibility.New(logger), mocks.NewMocklayerPatrol(ctrl), mocks.NewMockstateQuerier(ctrl), newMockClock(), logger)
	assert.NotNil(t, h)
}

func TestHare_Start(t *testing.T) {
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	assert.NoError(t, h.Start(context.Background()))
	h.Close()
}

func TestHare_collectOutputAndGetResult(t *testing.T) {
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())

	lyrID := types.NewLayerID(10)
	res, err := h.getResult(lyrID)
	assert.Equal(t, errNoResult, err)
	assert.Nil(t, res)

	pids := []types.ProposalID{types.RandomProposalID(), types.RandomProposalID(), types.RandomProposalID()}
	set := NewSetFromValues(pids...)
	require.NoError(t, h.collectOutput(context.Background(), mockReport{lyrID, set, true, false}))
	lo := <-h.blockGenCh
	require.Equal(t, lyrID, lo.Layer)
	require.ElementsMatch(t, pids, lo.Proposals)

	res, err = h.getResult(lyrID)
	assert.NoError(t, err)
	assert.ElementsMatch(t, pids, res)

	res, err = h.getResult(lyrID.Add(1))
	assert.Equal(t, errNoResult, err)
	assert.Empty(t, res)
}

func TestHare_collectOutputGetResult_TerminateTooLate(t *testing.T) {
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())

	lyrID := types.NewLayerID(10)
	res, err := h.getResult(lyrID)
	assert.Equal(t, errNoResult, err)
	assert.Nil(t, res)

	h.layerLock.Lock()
	h.lastLayer = lyrID.Add(h.config.Hdist + 1)
	h.layerLock.Unlock()

	pids := []types.ProposalID{types.RandomProposalID(), types.RandomProposalID(), types.RandomProposalID()}
	set := NewSetFromValues(pids...)
	err = h.collectOutput(context.Background(), mockReport{lyrID, set, true, false})
	assert.Equal(t, ErrTooLate, err)
	lo := <-h.blockGenCh
	require.Equal(t, lyrID, lo.Layer)
	require.ElementsMatch(t, pids, lo.Proposals)

	res, err = h.getResult(lyrID)
	assert.Equal(t, err, errTooOld)
	assert.Empty(t, res)
}

func TestHare_OutputCollectionLoop(t *testing.T) {
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	require.NoError(t, h.Start(context.Background()))

	lyrID := types.NewLayerID(8)
	mo := mockReport{lyrID, NewEmptySet(0), true, false}
	_, err := h.broker.Register(context.Background(), mo.ID())
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	h.outputChan <- mo
	time.Sleep(1 * time.Second)

	h.broker.mu.RLock()
	require.Nil(t, h.broker.outbox[mo.ID().Uint32()])
	h.broker.mu.RUnlock()

	lo := <-h.blockGenCh
	require.Equal(t, lyrID, lo.Layer)
	require.Empty(t, lo.Proposals)
}

func TestHare_malfeasanceLoop(t *testing.T) {
	mpubsub := pubsubmocks.NewMockPublishSubsciber(gomock.NewController(t))
	mpubsub.EXPECT().Register(pubsub.HareProtocol, gomock.Any())
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), mpubsub, t.Name())

	lid := types.NewLayerID(11)
	round := uint32(3)

	gossip := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: lid,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
					Messages: [2]types.HareProofMsg{
						{
							InnerMsg: types.HareMetadata{
								Layer:   lid,
								Round:   round,
								MsgHash: types.RandomHash(),
							},
							Signature: types.RandomBytes(64),
						},
						{
							InnerMsg: types.HareMetadata{
								Layer:   lid,
								Round:   round,
								MsgHash: types.RandomHash(),
							},
							Signature: types.RandomBytes(64),
						},
					},
				},
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:  lid,
			Round:  round,
			PubKey: types.RandomBytes(32),
			Eligibility: types.HareEligibility{
				Proof: []byte("eligible"),
				Count: 3,
			},
		},
	}
	h.malCh <- gossip
	data, err := codec.Encode(&gossip)
	require.NoError(t, err)
	done := make(chan struct{})
	mpubsub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, data).DoAndReturn(
		func(_ context.Context, _ string, _ []byte) error {
			close(done)
			return nil
		})
	require.NoError(t, h.Start(context.Background()))
	defer h.Close()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 100*time.Millisecond)
	nodeID := types.BytesToNodeID(gossip.Eligibility.PubKey)
	proof, err := identities.GetMalfeasanceProof(h.cdb, nodeID)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.Equal(t, gossip.MalfeasanceProof, *proof)
}

func TestHare_onTick(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1
	cfg.Hdist = 1
	clock := newMockClock()
	h := createTestHare(t, sql.InMemory(), cfg, clock, noopPubSub(t), t.Name())

	h.networkDelta = 0
	createdChan := make(chan struct{}, 1)
	startedChan := make(chan struct{}, 1)
	var nmcp *mockConsensusProcess
	h.factory = func(ctx context.Context, cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput, _ chan types.MalfeasanceGossip) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan, startedChan)
		close(createdChan)
		return nmcp
	}

	require.NoError(t, h.Start(context.Background()))

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	pList := []*types.Proposal{
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
	}
	for _, p := range pList {
		require.NoError(t, ballots.Add(h.cdb, &p.Ballot))
		require.NoError(t, proposals.Add(h.cdb, p))
	}

	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	h.beacons = mockBeacons
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(beacon, nil).Times(1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		<-createdChan
		<-startedChan
		wg.Done()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	out := <-h.blockGenCh
	require.Equal(t, lyrID, out.Layer)
	require.ElementsMatch(t, types.ToProposalIDs(pList), out.Proposals)

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
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1
	cfg.Hdist = 1
	clock := newMockClock()
	h := createTestHare(t, sql.InMemory(), cfg, clock, noopPubSub(t), t.Name())

	h.networkDelta = 0
	createdChan := make(chan struct{}, 1)
	startedChan := make(chan struct{}, 1)
	var nmcp *mockConsensusProcess
	h.factory = func(ctx context.Context, cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput, _ chan types.MalfeasanceGossip) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan, startedChan)
		close(createdChan)
		return nmcp
	}

	require.NoError(t, h.Start(context.Background()))
	defer h.Close()

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	pList := []*types.Proposal{
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
	}
	refBallot := &randomProposal(lyrID.Sub(1), beacon).Ballot
	require.NoError(t, ballots.Add(h.cdb, refBallot))
	pList[1].EpochData = nil
	pList[1].RefBallot = refBallot.ID()
	for _, p := range pList {
		require.NoError(t, ballots.Add(h.cdb, &p.Ballot))
		require.NoError(t, proposals.Add(h.cdb, p))
	}

	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	h.beacons = mockBeacons

	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(beacon, nil).Times(1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		<-createdChan
		<-startedChan
		wg.Done()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	out := <-h.blockGenCh
	require.Equal(t, lyrID, out.Layer)
	require.ElementsMatch(t, types.ToProposalIDs(pList), out.Proposals)
}

func TestHare_onTick_SomeBadBallots(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1
	cfg.Hdist = 1
	clock := newMockClock()
	h := createTestHare(t, sql.InMemory(), cfg, clock, noopPubSub(t), t.Name())

	h.networkDelta = 0
	createdChan := make(chan struct{}, 1)
	startedChan := make(chan struct{}, 1)
	var nmcp *mockConsensusProcess
	h.factory = func(ctx context.Context, cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput, _ chan types.MalfeasanceGossip) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan, startedChan)
		close(createdChan)
		return nmcp
	}

	require.NoError(t, h.Start(context.Background()))
	defer h.Close()

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	epochBeacon := types.RandomBeacon()
	pList := []*types.Proposal{
		randomProposal(lyrID, epochBeacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, epochBeacon),
	}
	for _, p := range pList {
		require.NoError(t, ballots.Add(h.cdb, &p.Ballot))
		require.NoError(t, proposals.Add(h.cdb, p))
	}
	goodProposals := []*types.Proposal{pList[0], pList[2]}

	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	h.beacons = mockBeacons
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(epochBeacon, nil).Times(1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		<-createdChan
		<-startedChan
		wg.Done()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	out := <-h.blockGenCh
	require.Equal(t, lyrID, out.Layer)
	require.ElementsMatch(t, types.ToProposalIDs(goodProposals), out.Proposals)
}

func TestHare_onTick_NoGoodBallots(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = 1
	cfg.Hdist = 1
	clock := newMockClock()
	h := createTestHare(t, sql.InMemory(), cfg, clock, noopPubSub(t), t.Name())

	h.networkDelta = 0
	createdChan := make(chan struct{}, 1)
	startedChan := make(chan struct{}, 1)
	var nmcp *mockConsensusProcess
	h.factory = func(ctx context.Context, cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, outputChan chan TerminationOutput, _ chan types.MalfeasanceGossip) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, signing, p2p, outputChan, startedChan)
		close(createdChan)
		return nmcp
	}

	require.NoError(t, h.Start(context.Background()))
	defer h.Close()

	lyrID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	epochBeacon := types.RandomBeacon()
	pList := []*types.Proposal{
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
	}
	for _, p := range pList {
		require.NoError(t, ballots.Add(h.cdb, &p.Ballot))
		require.NoError(t, proposals.Add(h.cdb, p))
	}

	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	h.beacons = mockBeacons
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(epochBeacon, nil).Times(1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.advanceLayer()
		<-createdChan
		<-startedChan
		wg.Done()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	out := <-h.blockGenCh
	require.Equal(t, lyrID, out.Layer)
	require.Empty(t, out.Proposals)
}

func TestHare_onTick_NoBeacon(t *testing.T) {
	lyr := types.NewLayerID(199)

	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	h.beacons = mockBeacons
	mockBeacons.EXPECT().GetBeacon(lyr.GetEpoch()).Return(types.EmptyBeacon, errors.New("whatever")).Times(1)
	h.broker.Start(context.Background())
	defer h.broker.Close()

	started, err := h.onTick(context.Background(), lyr)
	assert.NoError(t, err)
	assert.False(t, started)
}

func TestHare_onTick_NotSynced(t *testing.T) {
	lyr := types.NewLayerID(199)

	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyr).Return(true, nil).AnyTimes()
	mockSyncS := smocks.NewMockSyncStateProvider(gomock.NewController(t))
	h.broker.nodeSyncState = mockSyncS
	h.broker.Start(context.Background())
	defer h.broker.Close()

	mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(false)
	started, err := h.onTick(context.Background(), lyr)
	assert.NoError(t, err)
	assert.False(t, started)

	mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true)
	mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(false)
	started, err = h.onTick(context.Background(), lyr)
	assert.NoError(t, err)
	assert.False(t, started)
}

func TestHare_outputBuffer(t *testing.T) {
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.config.Hdist; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		require.NoError(t, h.collectOutput(context.Background(), mockReport{lyr, NewEmptySet(0), true, false}))
		_, ok := h.outputs[lyr]
		require.True(t, ok)
		require.EqualValues(t, i, len(h.outputs))
	}

	require.EqualValues(t, h.config.Hdist, len(h.outputs))

	// add another output
	lyr = lyr.Add(1)
	h.setLastLayer(lyr)
	require.NoError(t, h.collectOutput(context.Background(), mockReport{lyr, NewEmptySet(0), true, false}))
	_, ok := h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.config.Hdist, len(h.outputs))

	// make sure the oldest layer is no longer there
	_, ok = h.outputs[lyr.Sub(h.config.Hdist)]
	assert.False(t, ok)
}

func TestHare_IsTooLate(t *testing.T) {
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.config.Hdist*2; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		_ = h.collectOutput(context.Background(), mockReport{lyr, NewEmptySet(0), true, false})
		_, ok := h.outputs[lyr]
		assert.True(t, ok)
		if i < h.config.Hdist {
			assert.EqualValues(t, i, len(h.outputs))
		} else {
			assert.EqualValues(t, h.config.Hdist, len(h.outputs))
		}
	}

	for i := uint32(1); i <= h.config.Hdist; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		require.True(t, h.outOfBufferRange(types.GetEffectiveGenesis().Add(1)))
	}
	assert.False(t, h.outOfBufferRange(lyr.Add(1)))
}

func TestHare_oldestInBuffer(t *testing.T) {
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.config.Hdist; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		require.NoError(t, h.collectOutput(context.Background(), mockReport{lyr, NewEmptySet(0), true, false}))
		_, ok := h.outputs[lyr]
		require.True(t, ok)
		require.EqualValues(t, i, len(h.outputs))
	}

	require.Equal(t, types.GetEffectiveGenesis().Add(1), h.oldestResultInBuffer())

	lyr = lyr.Add(1)
	h.setLastLayer(lyr)
	require.NoError(t, h.collectOutput(context.Background(), mockReport{lyr, NewEmptySet(0), true, false}))
	_, ok := h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.config.Hdist, len(h.outputs))

	require.Equal(t, types.GetEffectiveGenesis().Add(2), h.oldestResultInBuffer())

	lyr = lyr.Add(2)
	h.setLastLayer(lyr)
	require.NoError(t, h.collectOutput(context.Background(), mockReport{lyr, NewEmptySet(0), true, false}))
	_, ok = h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.config.Hdist, len(h.outputs))

	require.Equal(t, types.GetEffectiveGenesis().Add(3), h.oldestResultInBuffer())
}

// make sure that Hare writes a weak coin value for a layer to the mesh after the CP completes,
// regardless of whether it succeeds or fails.
func TestHare_WeakCoin(t *testing.T) {
	h := createTestHare(t, sql.InMemory(), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	layerID := types.NewLayerID(10)
	h.setLastLayer(layerID)

	require.NoError(t, h.Start(context.Background()))
	defer h.Close()
	waitForMsg := func() error {
		tmr := time.NewTimer(time.Second)
		select {
		case <-tmr.C:
			return errors.New("timeout")
		case <-h.blockGenCh:
			return nil
		}
	}

	pList := []*types.Proposal{
		randomProposal(layerID, types.EmptyBeacon),
		randomProposal(layerID, types.EmptyBeacon),
		randomProposal(layerID, types.EmptyBeacon),
	}
	for _, p := range pList {
		require.NoError(t, ballots.Add(h.cdb, &p.Ballot))
		require.NoError(t, proposals.Add(h.cdb, p))
	}
	set := NewSetFromValues(types.ToProposalIDs(pList)...)

	// complete + coin flip true
	h.outputChan <- mockReport{layerID, set, true, true}
	require.NoError(t, waitForMsg())
	wc, err := layers.GetWeakCoin(h.cdb, layerID)
	require.NoError(t, err)
	require.True(t, wc)

	// incomplete + coin flip true
	h.outputChan <- mockReport{layerID, set, false, true}
	require.Error(t, waitForMsg())
	wc, err = layers.GetWeakCoin(h.cdb, layerID)
	require.NoError(t, err)
	require.True(t, wc)

	// complete + coin flip false
	h.outputChan <- mockReport{layerID, set, true, false}
	require.NoError(t, waitForMsg())
	wc, err = layers.GetWeakCoin(h.cdb, layerID)
	require.NoError(t, err)
	require.False(t, wc)

	// incomplete + coin flip false
	h.outputChan <- mockReport{layerID, set, false, true}
	require.Error(t, waitForMsg())

	wc, err = layers.GetWeakCoin(h.cdb, layerID)
	require.NoError(t, err)
	require.True(t, wc)
}
