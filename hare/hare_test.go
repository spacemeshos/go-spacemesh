package hare

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

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
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type mockConsensusProcess struct {
	started chan struct{}
	t       chan report
	w       chan wcReport
	id      types.LayerID
	set     *Set
}

func (mcp *mockConsensusProcess) Start() {
	close(mcp.started)
	mcp.t <- report{id: mcp.id, set: mcp.set, completed: true}
	mcp.w <- wcReport{id: mcp.id, coinflip: false}
}

func (mcp *mockConsensusProcess) Stop() {}

func (mcp *mockConsensusProcess) ID() types.LayerID {
	return mcp.id
}

func (mcp *mockConsensusProcess) SetInbox(_ any) {
}

var _ Consensus = (*mockConsensusProcess)(nil)

func newMockConsensusProcess(_ config.Config, instanceID types.LayerID, s *Set, _ Rolacle, _ *signing.EdSigner, _ pubsub.Publisher, outputChan chan report, wcChan chan wcReport, started chan struct{}) *mockConsensusProcess {
	mcp := new(mockConsensusProcess)
	mcp.started = started
	mcp.id = instanceID
	mcp.t = outputChan
	mcp.w = wcChan
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

func newMockMesh(tb testing.TB) *mocks.Mockmesh {
	tb.Helper()
	return mocks.NewMockmesh(gomock.NewController(tb))
}

func randomProposal(lyrID types.LayerID, beacon types.Beacon) *types.Proposal {
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					Layer: lyrID,
					AtxID: types.RandomATXID(),
					EpochData: &types.EpochData{
						ActiveSetHash: types.RandomHash(),
						Beacon:        beacon,
					},
				},
				ActiveSet: types.RandomActiveSet(10),
			},
		},
	}
	signer, _ := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Signature = signer.Sign(signing.BALLOT, p.SignedBytes())
	p.TxIDs = []types.TransactionID{}
	p.SmesherID = signer.NodeID()
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

	edVerifier, err := signing.NewEdVerifier()
	require.NoError(t, err)

	logger := logtest.New(t).WithName(t.Name())
	cfg := config.Config{N: 10, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 1000, Hdist: 20}
	h := New(
		datastore.NewCachedDB(sql.InMemory(), logtest.New(t)),
		cfg,
		noopPubSub(t),
		signer,
		edVerifier,
		signer.NodeID(),
		make(chan LayerOutput, 1),
		smocks.NewMockSyncStateProvider(ctrl), smocks.NewMockBeaconGetter(ctrl),
		eligibility.New(logger),
		mocks.NewMocklayerPatrol(ctrl),
		mocks.NewMockstateQuerier(ctrl),
		newMockClock(),
		mocks.NewMockweakCoin(ctrl),
		logger,
		withMesh(mocks.NewMockmesh(ctrl)),
	)
	require.NotNil(t, h)
}

func TestHare_Start(t *testing.T) {
	h := createTestHare(t, newMockMesh(t), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	require.NoError(t, h.Start(context.Background()))
	h.Close()
}

func TestHare_collectOutputAndGetResult(t *testing.T) {
	h := createTestHare(t, newMockMesh(t), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())

	lyrID := types.LayerID(10)
	res, err := h.getResult(lyrID)
	require.Equal(t, errNoResult, err)
	require.Nil(t, res)

	pids := []types.ProposalID{types.RandomProposalID(), types.RandomProposalID(), types.RandomProposalID()}
	set := NewSetFromValues(pids...)
	require.NoError(t, h.collectOutput(context.Background(), report{id: lyrID, set: set, completed: true}))
	lo := <-h.blockGenCh
	require.Equal(t, lyrID, lo.Layer)
	require.ElementsMatch(t, pids, lo.Proposals)

	res, err = h.getResult(lyrID)
	require.NoError(t, err)
	require.ElementsMatch(t, pids, res)

	res, err = h.getResult(lyrID.Add(1))
	require.Equal(t, errNoResult, err)
	require.Empty(t, res)
}

func TestHare_collectOutputGetResult_TerminateTooLate(t *testing.T) {
	h := createTestHare(t, newMockMesh(t), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())

	lyrID := types.LayerID(10)
	res, err := h.getResult(lyrID)
	require.Equal(t, errNoResult, err)
	require.Nil(t, res)

	h.setLastLayer(lyrID.Add(h.config.Hdist + 1))

	pids := []types.ProposalID{types.RandomProposalID(), types.RandomProposalID(), types.RandomProposalID()}
	set := NewSetFromValues(pids...)
	err = h.collectOutput(context.Background(), report{id: lyrID, set: set, completed: true})
	require.Equal(t, ErrTooLate, err)
	lo := <-h.blockGenCh
	require.Equal(t, lyrID, lo.Layer)
	require.ElementsMatch(t, pids, lo.Proposals)

	res, err = h.getResult(lyrID)
	require.Equal(t, err, errTooOld)
	require.Empty(t, res)
}

func TestHare_OutputCollectionLoop(t *testing.T) {
	mockMesh := newMockMesh(t)
	h := createTestHare(t, mockMesh, config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	require.NoError(t, h.Start(context.Background()))

	lyrID := types.LayerID(8)
	_, _, err := h.broker.Register(context.Background(), lyrID)
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	h.mockCoin.EXPECT().Set(lyrID, false)
	h.wcChan <- wcReport{id: lyrID, coinflip: false}
	h.outputChan <- report{id: lyrID, set: NewEmptySet(0), completed: true}
	lo := <-h.blockGenCh
	require.Equal(t, lyrID, lo.Layer)
	require.Empty(t, lo.Proposals)
	require.Eventually(t, func() bool {
		return len(h.wcChan) == 0
	}, time.Second, 100*time.Millisecond)
}

func TestHare_malfeasanceLoop(t *testing.T) {
	mpubsub := pubsubmocks.NewMockPublishSubsciber(gomock.NewController(t))
	mpubsub.EXPECT().Register(pubsub.HareProtocol, gomock.Any())
	mockMesh := newMockMesh(t)
	h := createTestHare(t, mockMesh, config.DefaultConfig(), newMockClock(), mpubsub, t.Name())

	lid := types.LayerID(11)
	round := uint32(3)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cdb := datastore.NewCachedDB(sql.InMemory(), h.Log)
	createIdentity(t, cdb.Database, sig)
	proof := &types.HareProof{
		Messages: [2]types.HareProofMsg{
			{
				InnerMsg: types.HareMetadata{
					Layer:   lid,
					Round:   round,
					MsgHash: types.RandomHash(),
				},
				SmesherID: sig.NodeID(),
				Signature: types.RandomEdSignature(),
			},
			{
				InnerMsg: types.HareMetadata{
					Layer:   lid,
					Round:   round,
					MsgHash: types.RandomHash(),
				},
				SmesherID: sig.NodeID(),
				Signature: types.RandomEdSignature(),
			},
		},
	}
	proof.Messages[0].Signature = sig.Sign(signing.HARE, proof.Messages[0].SignedBytes())
	proof.Messages[1].Signature = sig.Sign(signing.HARE, proof.Messages[1].SignedBytes())
	gossip := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: lid,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: proof,
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:  lid,
			Round:  round,
			NodeID: types.RandomNodeID(),
			Eligibility: types.HareEligibility{
				Proof: types.RandomVrfSignature(),
				Count: 3,
			},
		},
	}
	h.mchMalfeasance <- &gossip
	data, err := codec.Encode(&gossip)
	require.NoError(t, err)
	done := make(chan struct{})
	mockMesh.EXPECT().Cache().Return(cdb).AnyTimes()
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
}

func TestHare_onTick(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.RoundDuration = 1
	cfg.Hdist = 1

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clock := newMockClock()
	mockMesh := newMockMesh(t)
	h := createTestHare(t, mockMesh, cfg, clock, noopPubSub(t), t.Name())

	h.networkDelta = 0
	createdChan := make(chan struct{}, 1)
	startedChan := make(chan struct{}, 1)
	var nmcp *mockConsensusProcess
	h.factory = func(ctx context.Context, cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, et *EligibilityTracker, sig *signing.EdSigner, p2p pubsub.Publisher, comm communication, clock RoundClock) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, sig, p2p, comm.report, comm.wc, startedChan)
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
		mockMesh.EXPECT().GetAtxHeader(p.AtxID).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1, NodeID: p.SmesherID}, nil)
		for _, id := range p.ActiveSet {
			nodeID := types.RandomNodeID()
			mockMesh.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1, NodeID: nodeID}, nil)
			mockMesh.EXPECT().GetMalfeasanceProof(nodeID)
		}
	}
	mockMesh.EXPECT().GetEpochAtx(lyrID.GetEpoch(), h.nodeID).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil)
	mockMesh.EXPECT().Proposals(lyrID).Return(pList, nil)
	h.mockCoin.EXPECT().Set(lyrID, gomock.Any())

	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	h.beacons = mockBeacons
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), types.GetEffectiveGenesis()).Return(true, nil).AnyTimes()
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(true, nil).Times(1)
	mockBeacons.EXPECT().GetBeacon(lyrID.GetEpoch()).Return(beacon, nil).Times(1)

	var eg errgroup.Group
	eg.Go(func() error {
		clock.advanceLayer()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-createdChan:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-startedChan:
		}
		return nil
	})
	require.NoError(t, eg.Wait())

	out := <-h.blockGenCh
	require.Equal(t, lyrID, out.Layer)
	require.ElementsMatch(t, types.ToProposalIDs(pList), out.Proposals)

	lyrID = lyrID.Add(1)
	// consensus process is closed, should not process any tick
	eg.Go(func() error {
		clock.advanceLayer()

		h.Close()
		return nil
	})
	eg.Wait()

	// collect output one more time
	res2, err := h.getResult(lyrID)
	require.Equal(t, errNoResult, err)
	require.Empty(t, res2)
}

func TestHare_onTick_notMining(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.N = 2
	cfg.RoundDuration = 1
	cfg.Hdist = 1
	clock := newMockClock()
	mockMesh := newMockMesh(t)
	h := createTestHare(t, mockMesh, cfg, clock, noopPubSub(t), t.Name())

	h.networkDelta = 0
	createdChan := make(chan struct{}, 1)
	startedChan := make(chan struct{}, 1)
	wcSaved := make(chan struct{}, 1)
	var nmcp *mockConsensusProcess
	h.factory = func(ctx context.Context, cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, et *EligibilityTracker, sig *signing.EdSigner, p2p pubsub.Publisher, comm communication, clock RoundClock) Consensus {
		nmcp = newMockConsensusProcess(cfg, instanceId, s, oracle, sig, p2p, comm.report, comm.wc, startedChan)
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
		for _, id := range p.ActiveSet {
			nodeID := types.RandomNodeID()
			mockMesh.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1, NodeID: nodeID}, nil)
			mockMesh.EXPECT().GetMalfeasanceProof(nodeID)
		}
	}
	mockMesh.EXPECT().GetEpochAtx(lyrID.GetEpoch(), h.nodeID).Return(nil, sql.ErrNotFound)
	mockMesh.EXPECT().Proposals(lyrID).Return(pList, nil)
	h.mockCoin.EXPECT().Set(lyrID, gomock.Any()).DoAndReturn(
		func(_ types.LayerID, _ bool) error {
			close(wcSaved)
			return nil
		})

	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	h.beacons = mockBeacons
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyrID).Return(false, nil).Times(1)
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
	out := <-h.blockGenCh
	require.Equal(t, lyrID, out.Layer)
	require.ElementsMatch(t, types.ToProposalIDs(pList), out.Proposals)
	select {
	case <-wcSaved:
	case <-time.After(10 * time.Second):
		require.Fail(t, "Weakcoin::Set has unexpectedly not been called")
	}
}

func TestHare_onTick_NoBeacon(t *testing.T) {
	lyr := types.LayerID(199)

	h := createTestHare(t, newMockMesh(t), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), lyr).Return(true, nil).MaxTimes(1)
	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	h.beacons = mockBeacons
	mockBeacons.EXPECT().GetBeacon(lyr.GetEpoch()).Return(types.EmptyBeacon, errors.New("whatever")).Times(1)
	h.broker.Start(context.Background())
	defer h.broker.Close()

	started, err := h.onTick(context.Background(), lyr)
	require.NoError(t, err)
	require.False(t, started)
}

func TestHare_onTick_NotSynced(t *testing.T) {
	lyr := types.LayerID(199)

	h := createTestHare(t, newMockMesh(t), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	mockSyncS := smocks.NewMockSyncStateProvider(gomock.NewController(t))
	h.broker.nodeSyncState = mockSyncS
	h.broker.Start(context.Background())

	mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(false).Times(2)
	started, err := h.onTick(context.Background(), lyr)
	require.NoError(t, err)
	require.False(t, started)

	mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).Times(2)
	mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(false).Times(2)
	started, err = h.onTick(context.Background(), lyr)
	require.NoError(t, err)
	require.False(t, started)
	h.Close()
}

func TestHare_goodProposals(t *testing.T) {
	beacon := types.RandomBeacon()
	nodeBeacon := types.RandomBeacon()
	nodeBaseHeight := uint64(11)
	tt := []struct {
		name        string
		beacons     [3]types.Beacon
		baseHeights [3]uint64
		malicious   [3]bool
		atxids      [3]*types.ATXID
		refBallot   []int
		expected    []int
	}{
		{
			name:        "all good",
			beacons:     [3]types.Beacon{nodeBeacon, nodeBeacon, nodeBeacon},
			baseHeights: [3]uint64{nodeBaseHeight, nodeBaseHeight, nodeBaseHeight},
			expected:    []int{0, 1, 2},
		},
		{
			name:        "tick height too high",
			beacons:     [3]types.Beacon{nodeBeacon, nodeBeacon, nodeBeacon},
			baseHeights: [3]uint64{nodeBaseHeight + 1, nodeBaseHeight, nodeBaseHeight},
			expected:    []int{1, 2},
		},
		{
			name:        "get beacon from ref ballot",
			beacons:     [3]types.Beacon{nodeBeacon, nodeBeacon, nodeBeacon},
			baseHeights: [3]uint64{nodeBaseHeight, nodeBaseHeight, nodeBaseHeight},
			refBallot:   []int{1},
			expected:    []int{0, 1, 2},
		},
		{
			name:        "some bad beacon",
			beacons:     [3]types.Beacon{nodeBeacon, beacon, nodeBeacon},
			baseHeights: [3]uint64{nodeBaseHeight, nodeBaseHeight, nodeBaseHeight},
			expected:    []int{0, 2},
		},
		{
			name:        "all bad beacon",
			beacons:     [3]types.Beacon{beacon, beacon, beacon},
			baseHeights: [3]uint64{nodeBaseHeight, nodeBaseHeight, nodeBaseHeight},
		},
		{
			name:        "all malicious",
			beacons:     [3]types.Beacon{nodeBeacon, nodeBeacon, nodeBeacon},
			baseHeights: [3]uint64{nodeBaseHeight, nodeBaseHeight, nodeBaseHeight},
			malicious:   [3]bool{true, true, true},
		},
		{
			name:        "some malicious",
			beacons:     [3]types.Beacon{nodeBeacon, nodeBeacon, nodeBeacon},
			baseHeights: [3]uint64{nodeBaseHeight, nodeBaseHeight, nodeBaseHeight},
			malicious:   [3]bool{false, true, true},
			expected:    []int{0},
		},
		{
			name:        "reusing atxids",
			beacons:     [3]types.Beacon{nodeBeacon, nodeBeacon, nodeBeacon},
			baseHeights: [3]uint64{nodeBaseHeight, nodeBaseHeight, nodeBaseHeight},
			atxids:      [3]*types.ATXID{{1}, {1}},
			expected:    []int{2},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lyrID := types.GetEffectiveGenesis().Add(1)
			mockMesh := newMockMesh(t)
			pList := []*types.Proposal{
				randomProposal(lyrID, tc.beacons[0]),
				randomProposal(lyrID, tc.beacons[1]),
				randomProposal(lyrID, tc.beacons[2]),
			}
			if len(tc.refBallot) > 0 {
				for _, i := range tc.refBallot {
					refBallot := &randomProposal(lyrID.Sub(1), tc.beacons[i]).Ballot
					pList[i].EpochData = nil
					pList[i].RefBallot = refBallot.ID()
					mockMesh.EXPECT().Ballot(pList[1].RefBallot).Return(refBallot, nil)
					for _, id := range refBallot.ActiveSet {
						nodeID := types.RandomNodeID()
						mockMesh.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1, NodeID: nodeID}, nil).AnyTimes()
						mockMesh.EXPECT().GetMalfeasanceProof(nodeID).AnyTimes()
					}
				}
			}
			for i, p := range pList {
				if tc.malicious[i] {
					p.SetMalicious()
				} else if tc.atxids[i] != nil {
					p.AtxID = *tc.atxids[i]
				} else {
					mockMesh.EXPECT().GetAtxHeader(p.AtxID).Return(&types.ActivationTxHeader{BaseTickHeight: tc.baseHeights[i], TickCount: 1}, nil)
				}
				if p.EpochData != nil {
					for _, id := range p.ActiveSet {
						nodeID := types.RandomNodeID()
						mockMesh.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1, NodeID: nodeID}, nil).AnyTimes()
						mockMesh.EXPECT().GetMalfeasanceProof(nodeID).AnyTimes()
					}
				}
			}
			nodeID := types.NodeID{1, 2, 3}
			mockMesh.EXPECT().GetEpochAtx(lyrID.GetEpoch(), nodeID).Return(&types.ActivationTxHeader{BaseTickHeight: nodeBaseHeight, TickCount: 1}, nil)
			mockMesh.EXPECT().Proposals(lyrID).Return(pList, nil)

			expected := make([]types.ProposalID, 0, len(tc.expected))
			for _, i := range tc.expected {
				expected = append(expected, pList[i].ID())
			}
			got := goodProposals(context.Background(), logtest.New(t), mockMesh, nodeID, lyrID, nodeBeacon, time.Now(), time.Second)
			require.ElementsMatch(t, expected, got)
		})
	}
}

func TestHare_goodProposals_gradedAtxs(t *testing.T) {
	beacon := types.RandomBeacon()
	tickHeight := uint64(11)
	lyrID := types.GetEffectiveGenesis().Add(1)
	epochStart := time.Now()
	mockMesh := newMockMesh(t)
	activeSet := types.RandomActiveSet(10)
	pList := []*types.Proposal{
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
		randomProposal(lyrID, beacon),
	}
	for _, p := range pList {
		mockMesh.EXPECT().GetAtxHeader(p.AtxID).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1, NodeID: p.SmesherID}, nil)
	}

	delay := time.Second
	goodTime := epochStart.Add(-4*delay - time.Nanosecond)
	acceptableTime := epochStart.Add(-3*delay - time.Nanosecond)
	evilTime := epochStart.Add(-3 * delay)

	for i := 0; i < 4; i++ {
		good := &types.ActivationTxHeader{BaseTickHeight: tickHeight, TickCount: 1, Received: goodTime, NodeID: types.NodeID{byte(i + 1)}}
		mockMesh.EXPECT().GetAtxHeader(activeSet[i]).Return(good, nil).AnyTimes()
		mockMesh.EXPECT().GetMalfeasanceProof(good.NodeID).Return(nil, nil).AnyTimes()
	}

	earlyAtxLateMalicious := &types.ActivationTxHeader{BaseTickHeight: tickHeight, TickCount: 1, Received: goodTime, NodeID: types.NodeID{5}}
	mockMesh.EXPECT().GetAtxHeader(activeSet[4]).Return(earlyAtxLateMalicious, nil).AnyTimes()
	proof1 := &types.MalfeasanceProof{}
	proof1.SetReceived(epochStart)
	mockMesh.EXPECT().GetMalfeasanceProof(earlyAtxLateMalicious.NodeID).Return(proof1, nil).AnyTimes()

	earlyAtxMalicious := &types.ActivationTxHeader{BaseTickHeight: tickHeight, TickCount: 1, Received: goodTime, NodeID: types.NodeID{6}}
	mockMesh.EXPECT().GetAtxHeader(activeSet[5]).Return(earlyAtxMalicious, nil).AnyTimes()
	proof2 := &types.MalfeasanceProof{}
	proof2.SetReceived(epochStart.Add(-delay))
	mockMesh.EXPECT().GetMalfeasanceProof(earlyAtxMalicious.NodeID).Return(proof2, nil).AnyTimes()

	acceptable := &types.ActivationTxHeader{BaseTickHeight: tickHeight, TickCount: 1, Received: acceptableTime, NodeID: types.NodeID{7}}
	mockMesh.EXPECT().GetAtxHeader(activeSet[6]).Return(acceptable, nil).AnyTimes()
	mockMesh.EXPECT().GetMalfeasanceProof(acceptable.NodeID).Return(nil, nil).AnyTimes()

	acceptableMalicious := &types.ActivationTxHeader{BaseTickHeight: tickHeight, TickCount: 1, Received: acceptableTime, NodeID: types.NodeID{8}}
	mockMesh.EXPECT().GetAtxHeader(activeSet[7]).Return(acceptableMalicious, nil).AnyTimes()
	proof3 := &types.MalfeasanceProof{}
	proof3.SetReceived(epochStart.Add(-delay))
	mockMesh.EXPECT().GetMalfeasanceProof(acceptableMalicious.NodeID).Return(proof3, nil).AnyTimes()

	malicious := &types.ActivationTxHeader{BaseTickHeight: tickHeight, TickCount: 1, Received: goodTime, NodeID: types.NodeID{9}}
	mockMesh.EXPECT().GetAtxHeader(activeSet[8]).Return(malicious, nil).AnyTimes()
	proof4 := &types.MalfeasanceProof{}
	proof4.SetReceived(epochStart.Add(-delay - time.Nanosecond))
	mockMesh.EXPECT().GetMalfeasanceProof(malicious.NodeID).Return(proof4, nil).AnyTimes()

	evil := &types.ActivationTxHeader{BaseTickHeight: tickHeight, TickCount: 1, Received: evilTime, NodeID: types.NodeID{10}}
	mockMesh.EXPECT().GetAtxHeader(activeSet[9]).Return(evil, nil).AnyTimes()
	mockMesh.EXPECT().GetMalfeasanceProof(evil.NodeID).Return(nil, nil).AnyTimes()

	pList[0].ActiveSet = activeSet[:4]
	pList[1].ActiveSet = activeSet[:5]
	pList[2].ActiveSet = activeSet[:6]
	pList[3].ActiveSet = activeSet[:7]
	pList[4].ActiveSet = activeSet[:8]
	pList[5].ActiveSet = activeSet[:9]
	pList[6].ActiveSet = activeSet

	nodeID := types.NodeID{1, 2, 3}
	mockMesh.EXPECT().GetEpochAtx(lyrID.GetEpoch(), nodeID).Return(&types.ActivationTxHeader{BaseTickHeight: tickHeight, TickCount: 1}, nil)
	mockMesh.EXPECT().Proposals(lyrID).Return(pList, nil)
	got := goodProposals(context.Background(), logtest.New(t), mockMesh, nodeID, lyrID, beacon, epochStart, delay)
	require.ElementsMatch(t, types.ToProposalIDs(pList[:5]), got)
}

func TestHare_outputBuffer(t *testing.T) {
	h := createTestHare(t, newMockMesh(t), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.config.Hdist; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		require.NoError(t, h.collectOutput(context.Background(), report{id: lyr, set: NewEmptySet(0), completed: true}))
		_, ok := h.outputs[lyr]
		require.True(t, ok)
		require.EqualValues(t, i, len(h.outputs))
	}

	require.EqualValues(t, h.config.Hdist, len(h.outputs))

	// add another output
	lyr = lyr.Add(1)
	h.setLastLayer(lyr)
	require.NoError(t, h.collectOutput(context.Background(), report{id: lyr, set: NewEmptySet(0), completed: true}))
	_, ok := h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.config.Hdist, len(h.outputs))

	// make sure the oldest layer is no longer there
	_, ok = h.outputs[lyr.Sub(h.config.Hdist)]
	require.False(t, ok)
}

func TestHare_IsTooLate(t *testing.T) {
	h := createTestHare(t, newMockMesh(t), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.config.Hdist*2; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		_ = h.collectOutput(context.Background(), report{id: lyr, set: NewEmptySet(0), completed: true})
		_, ok := h.outputs[lyr]
		require.True(t, ok)
		if i < h.config.Hdist {
			require.EqualValues(t, i, len(h.outputs))
		} else {
			require.EqualValues(t, h.config.Hdist, len(h.outputs))
		}
	}

	for i := uint32(1); i <= h.config.Hdist; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		require.True(t, h.outOfBufferRange(types.GetEffectiveGenesis().Add(1)))
	}
	require.False(t, h.outOfBufferRange(lyr.Add(1)))
}

func TestHare_oldestInBuffer(t *testing.T) {
	h := createTestHare(t, newMockMesh(t), config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	var lyr types.LayerID
	for i := uint32(1); i <= h.config.Hdist; i++ {
		lyr = types.GetEffectiveGenesis().Add(i)
		h.setLastLayer(lyr)
		require.NoError(t, h.collectOutput(context.Background(), report{id: lyr, set: NewEmptySet(0), completed: true}))
		_, ok := h.outputs[lyr]
		require.True(t, ok)
		require.EqualValues(t, i, len(h.outputs))
	}

	require.Equal(t, types.GetEffectiveGenesis().Add(1), h.oldestResultInBuffer())

	lyr = lyr.Add(1)
	h.setLastLayer(lyr)
	require.NoError(t, h.collectOutput(context.Background(), report{id: lyr, set: NewEmptySet(0), completed: true}))
	_, ok := h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.config.Hdist, len(h.outputs))

	require.Equal(t, types.GetEffectiveGenesis().Add(2), h.oldestResultInBuffer())

	lyr = lyr.Add(2)
	h.setLastLayer(lyr)
	require.NoError(t, h.collectOutput(context.Background(), report{id: lyr, set: NewEmptySet(0), completed: true}))
	_, ok = h.outputs[lyr]
	require.True(t, ok)
	require.EqualValues(t, h.config.Hdist, len(h.outputs))

	require.Equal(t, types.GetEffectiveGenesis().Add(3), h.oldestResultInBuffer())
}

// make sure that Hare writes a weak coin value for a layer to the mesh after the CP completes,
// regardless of whether it succeeds or fails.
func TestHare_WeakCoin(t *testing.T) {
	mockMesh := newMockMesh(t)
	h := createTestHare(t, mockMesh, config.DefaultConfig(), newMockClock(), noopPubSub(t), t.Name())
	layerID := types.LayerID(10)
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

	set := NewSet([]types.ProposalID{{1}, {2}})

	// complete + coin flip true
	h.mockCoin.EXPECT().Set(layerID, true)
	h.outputChan <- report{id: layerID, set: set, completed: true}
	h.wcChan <- wcReport{id: layerID, coinflip: true}
	require.NoError(t, waitForMsg())

	// incomplete + coin flip true
	h.mockCoin.EXPECT().Set(layerID, true)
	h.outputChan <- report{id: layerID, set: set, completed: false}
	h.wcChan <- wcReport{id: layerID, coinflip: true}
	require.Error(t, waitForMsg())

	// complete + coin flip false
	h.mockCoin.EXPECT().Set(layerID, false)
	h.outputChan <- report{id: layerID, set: set, completed: true}
	h.wcChan <- wcReport{id: layerID, coinflip: false}
	require.NoError(t, waitForMsg())

	// incomplete + coin flip false
	h.mockCoin.EXPECT().Set(layerID, false)
	h.outputChan <- report{id: layerID, set: set, completed: false}
	h.wcChan <- wcReport{id: layerID, coinflip: false}
	require.Error(t, waitForMsg())
}
