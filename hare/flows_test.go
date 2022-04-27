package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type HareWrapper struct {
	totalCP     uint32
	termination util.Closer
	clock       *mockClock
	hare        []*Hare
	initialSets []*Set // all initial sets
	outputs     map[types.LayerID][]*Set
	name        string
}

func newHareWrapper(totalCp uint32) *HareWrapper {
	hs := new(HareWrapper)
	hs.clock = newMockClock()
	hs.totalCP = totalCp
	hs.termination = util.NewCloser()
	hs.outputs = make(map[types.LayerID][]*Set, 0)

	return hs
}

func (his *HareWrapper) fill(set *Set, begin, end int) {
	for i := begin; i <= end; i++ {
		his.initialSets[i] = set
	}
}

func (his *HareWrapper) waitForTermination() {
	for {
		count := 0
		for _, p := range his.hare {
			for i := types.GetEffectiveGenesis().Add(1); !i.After(types.GetEffectiveGenesis().Add(his.totalCP)); i = i.Add(1) {
				proposalIDs, _ := p.getResult(i)
				if len(proposalIDs) > 0 {
					count++
				}
			}
		}

		if count == int(his.totalCP)*len(his.hare) {
			break
		}

		time.Sleep(300 * time.Millisecond)
	}

	for _, p := range his.hare {
		for i := types.GetEffectiveGenesis().Add(1); !i.After(types.GetEffectiveGenesis().Add(his.totalCP)); i = i.Add(1) {
			s := NewEmptySet(10)
			proposalIDs, _ := p.getResult(i)
			for _, p := range proposalIDs {
				s.Add(p)
			}
			his.outputs[i] = append(his.outputs[i], s)
		}
	}

	his.termination.Close()
}

func (his *HareWrapper) WaitForTimedTermination(t *testing.T, timeout time.Duration) {
	timer := time.After(timeout)
	go his.waitForTermination()
	total := types.NewLayerID(his.totalCP)
	select {
	case <-timer:
		t.Fatal("Timeout")
		return
	case <-his.termination.CloseChannel():
		for i := types.NewLayerID(1); !i.After(total); i = i.Add(1) {
			his.checkResult(t, i)
		}
		return
	}
}

func (his *HareWrapper) checkResult(t *testing.T, id types.LayerID) {
	// check consistency
	out := his.outputs[id]
	for i := 0; i < len(out)-1; i++ {
		if !out[i].Equals(out[i+1]) {
			t.Errorf("Consistency check failed: Expected: %v Actual: %v", out[i], out[i+1])
		}
	}
}

type p2pManipulator struct {
	nd           pubsub.PublishSubsciber
	stalledLayer types.LayerID
	err          error
}

func (m *p2pManipulator) Register(protocol string, handler pubsub.GossipHandler) {
	m.nd.Register(protocol, handler)
}

func (m *p2pManipulator) Publish(ctx context.Context, protocol string, payload []byte) error {
	msg, _ := MessageFromBuffer(payload)
	if msg.InnerMsg.InstanceID == m.stalledLayer && msg.InnerMsg.K < 8 && msg.InnerMsg.K != preRound {
		return m.err
	}

	if err := m.nd.Publish(ctx, protocol, payload); err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}

	return nil
}

// Test - runs a single CP for more than one iteration.
func Test_consensusIterations(t *testing.T) {
	test := newConsensusTest()

	totalNodes := 15
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 1, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		p2pm := &p2pManipulator{nd: ps, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}
		proc, broker := createConsensusProcess(t, true, cfg, oracle, p2pm, test.initialSets[i], types.NewLayerID(1), t.Name())
		test.procs = append(test.procs, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	test.Create(totalNodes, creationFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 40*time.Second)
}

type hareWithMocks struct {
	*Hare
	ctrl           *gomock.Controller
	mockRoracle    *mocks.MockRolacle
	mockProposalDB *mocks.MockproposalProvider
	mockMeshDB     *mocks.MockmeshProvider
	mockBlockGen   *mocks.MockblockGenerator
	mockFetcher    *smocks.MockProposalFetcher
}

func createTestHare(t testing.TB, tcfg config.Config, clock *mockClock, pid p2p.Peer, p2p pubsub.PublishSubsciber, name string) *hareWithMocks {
	t.Helper()
	ed := signing.NewEdSigner()
	pub := ed.PublicKey()
	_, vrfPub, err := signing.NewVRFSigner(ed.Sign(pub.Bytes()))
	require.NoError(t, err)
	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: vrfPub}
	ctrl := gomock.NewController(t)
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(gomock.Any()).AnyTimes()
	patrol.EXPECT().CompleteHare(gomock.Any()).AnyTimes()
	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, nil).AnyTimes()
	mockIDProvider := mocks.NewMockidentityProvider(ctrl)
	mockIDProvider.EXPECT().GetIdentity(gomock.Any()).Return(nodeID, nil).AnyTimes()
	mockStateQ := mocks.NewMockstateQuerier(ctrl)
	mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	mockSyncS := smocks.NewMockSyncStateProvider(ctrl)
	mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()

	mockRoracle := mocks.NewMockRolacle(ctrl)
	mockBlockGen := mocks.NewMockblockGenerator(ctrl)
	mockMeshDB := mocks.NewMockmeshProvider(ctrl)
	mockProposalDB := mocks.NewMockproposalProvider(ctrl)
	mockFetcher := smocks.NewMockProposalFetcher(ctrl)

	hare := New(tcfg, pid, p2p, ed, nodeID, mockBlockGen, mockSyncS, mockMeshDB, mockProposalDB, mockBeacons, mockFetcher, mockRoracle, patrol, 10,
		mockIDProvider, mockStateQ, clock, logtest.New(t).WithName(name+"_"+ed.PublicKey().ShortString()))
	p2p.Register(ProtoName, hare.GetHareMsgHandler())

	return &hareWithMocks{
		Hare:           hare,
		ctrl:           ctrl,
		mockRoracle:    mockRoracle,
		mockProposalDB: mockProposalDB,
		mockMeshDB:     mockMeshDB,
		mockBlockGen:   mockBlockGen,
		mockFetcher:    mockFetcher,
	}
}

type mockClock struct {
	channels     map[types.LayerID]chan struct{}
	layerTime    map[types.LayerID]time.Time
	currentLayer types.LayerID
	m            sync.RWMutex
}

func newMockClock() *mockClock {
	return &mockClock{
		channels:     make(map[types.LayerID]chan struct{}),
		layerTime:    map[types.LayerID]time.Time{types.GetEffectiveGenesis(): time.Now()},
		currentLayer: types.GetEffectiveGenesis().Add(1),
	}
}

func (m *mockClock) LayerToTime(layer types.LayerID) time.Time {
	m.m.RLock()
	defer m.m.RUnlock()

	return m.layerTime[layer]
}

func (m *mockClock) AwaitLayer(layer types.LayerID) chan struct{} {
	m.m.Lock()
	defer m.m.Unlock()

	if _, ok := m.layerTime[layer]; ok {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	if ch, ok := m.channels[layer]; ok {
		return ch
	}
	ch := make(chan struct{})
	m.channels[layer] = ch
	return ch
}

func (m *mockClock) GetCurrentLayer() types.LayerID {
	m.m.RLock()
	defer m.m.RUnlock()

	return m.currentLayer
}

func (m *mockClock) advanceLayer() {
	time.Sleep(time.Millisecond)
	m.m.Lock()
	defer m.m.Unlock()

	log.Info("sending for layer %v", m.currentLayer)
	m.layerTime[m.currentLayer] = time.Now()
	if ch, ok := m.channels[m.currentLayer]; ok {
		close(ch)
		delete(m.channels, m.currentLayer)
	}
	m.currentLayer = m.currentLayer.Add(1)
}

type SharedRoundClock struct {
	currentRound     uint32
	rounds           map[uint32]chan struct{}
	minCount         int
	processingDelay  time.Duration
	sentMessages     uint16
	advanceScheduled bool
	m                sync.Mutex
}

func NewSharedClock(minCount int, totalCP uint32, processingDelay time.Duration) map[types.LayerID]*SharedRoundClock {
	m := make(map[types.LayerID]*SharedRoundClock)
	for i := types.GetEffectiveGenesis().Add(1); !i.After(types.GetEffectiveGenesis().Add(totalCP)); i = i.Add(1) {
		m[i] = &SharedRoundClock{
			currentRound:    preRound,
			rounds:          make(map[uint32]chan struct{}),
			minCount:        minCount,
			processingDelay: processingDelay,
			sentMessages:    0,
		}
	}
	return m
}

func (c *SharedRoundClock) AwaitWakeup() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (c *SharedRoundClock) AwaitEndOfRound(round uint32) <-chan struct{} {
	c.m.Lock()
	defer c.m.Unlock()

	ch, ok := c.rounds[round]
	if !ok {
		ch = make(chan struct{})
		c.rounds[round] = ch
	}
	return ch
}

func (c *SharedRoundClock) IncMessages(cnt uint16) {
	c.m.Lock()
	defer c.m.Unlock()

	c.sentMessages += cnt
	if int(c.sentMessages) >= c.minCount && !c.advanceScheduled {
		time.AfterFunc(c.processingDelay, func() {
			c.m.Lock()
			defer c.m.Unlock()

			c.sentMessages = 0
			c.advanceScheduled = false
			c.advanceRound()
		})
		c.advanceScheduled = true
	}
}

func (c *SharedRoundClock) advanceRound() {
	c.currentRound++
	if prevRound, ok := c.rounds[c.currentRound-1]; ok {
		close(prevRound)
	}
}

type SimRoundClock struct {
	clocks map[types.LayerID]*SharedRoundClock
	m      sync.Mutex
	s      pubsub.PublishSubsciber
}

func (c *SimRoundClock) Register(protocol string, handler pubsub.GossipHandler) {
	c.s.Register(protocol, handler)
}

func (c *SimRoundClock) Publish(ctx context.Context, protocol string, payload []byte) error {
	instanceID, cnt := extractInstanceID(payload)

	c.m.Lock()
	clock := c.clocks[instanceID]
	clock.IncMessages(cnt)
	c.m.Unlock()

	if err := c.s.Publish(ctx, protocol, payload); err != nil {
		return fmt.Errorf("failed to broadcast: %w", err)
	}
	return nil
}

func extractInstanceID(payload []byte) (types.LayerID, uint16) {
	m, err := MessageFromBuffer(payload)
	if err != nil {
		panic(err)
	}
	return m.InnerMsg.InstanceID, m.InnerMsg.EligibilityCount
}

func (c *SimRoundClock) NewRoundClock(layerID types.LayerID) RoundClock {
	return c.clocks[layerID]
}

func NewSimRoundClock(s pubsub.PublishSubsciber, clocks map[types.LayerID]*SharedRoundClock) *SimRoundClock {
	return &SimRoundClock{
		clocks: clocks,
		s:      s,
	}
}

// Test - run multiple CPs simultaneously.
func Test_multipleCPs(t *testing.T) {
	logtest.SetupGlobal(t)

	types.SetLayersPerEpoch(4)
	r := require.New(t)
	totalCp := uint32(3)
	test := newHareWrapper(totalCp)
	totalNodes := 10
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 5, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)

	proposals := make(map[types.LayerID][]*types.Proposal)
	proposalsByID := make(map[types.ProposalID]*types.Proposal)
	blocks := make(map[types.LayerID]*types.Block)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
		for i := uint64(0); i < 200; i++ {
			p := types.GenLayerProposal(j, nil)
			p.EpochData = &types.EpochData{
				Beacon: types.EmptyBeacon,
			}
			proposals[j] = append(proposals[j], p)
			proposalsByID[p.ID()] = p
		}
		blocks[j] = types.GenLayerBlock(j, types.RandomTXSet(199))
	}
	pubsubs := []*pubsub.PubSub{}
	scMap := NewSharedClock(totalNodes, totalCp, time.Duration(50*int(totalCp)*totalNodes)*time.Millisecond)
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		src := NewSimRoundClock(ps, scMap)
		pubsubs = append(pubsubs, ps)
		h := createTestHare(t, cfg, test.clock, host.ID(), src, t.Name())
		h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockRoracle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any()).Return(make([]byte, 100), nil).AnyTimes()
		h.mockRoracle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint16(1), nil).AnyTimes()
		h.mockRoracle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		h.mockProposalDB.EXPECT().LayerProposals(gomock.Any()).DoAndReturn(
			func(layerID types.LayerID) ([]*types.Proposal, error) {
				return proposals[layerID], nil
			}).AnyTimes()
		h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
			func(pids []types.ProposalID) ([]*types.Proposal, error) {
				props := make([]*types.Proposal, 0, len(pids))
				for _, pid := range pids {
					props = append(props, proposalsByID[pid])
				}
				return props, nil
			}).AnyTimes()
		h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, layerID types.LayerID, props []*types.Proposal) (*types.Block, error) {
				assert.ElementsMatch(t, proposals[layerID], props)
				return blocks[layerID], nil
			}).AnyTimes()
		h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, block *types.Block) error {
				assert.Equal(t, blocks[block.LayerIndex], block)
				return nil
			}).AnyTimes()
		h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		h.newRoundClock = src.NewRoundClock
		test.hare = append(test.hare, h.Hare)
		e := h.Start(context.TODO())
		r.NoError(e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(ProtoName)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
			test.clock.advanceLayer()
			time.Sleep(250 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 80*time.Second)
}

// Test - run multiple CPs where one of them runs more than one iteration.
func Test_multipleCPsAndIterations(t *testing.T) {
	logtest.SetupGlobal(t)

	types.SetLayersPerEpoch(4)
	r := require.New(t)
	totalCp := uint32(4)
	test := newHareWrapper(totalCp)
	totalNodes := 10
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 3, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)

	proposals := make(map[types.LayerID][]*types.Proposal)
	proposalsByID := make(map[types.ProposalID]*types.Proposal)
	blocks := make(map[types.LayerID]*types.Block)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
		for i := uint64(0); i < 200; i++ {
			p := types.GenLayerProposal(j, nil)
			p.EpochData = &types.EpochData{
				Beacon: types.EmptyBeacon,
			}
			proposals[j] = append(proposals[j], p)
			proposalsByID[p.ID()] = p
		}
		blocks[j] = types.GenLayerBlock(j, types.RandomTXSet(199))
	}
	pubsubs := []*pubsub.PubSub{}
	scMap := NewSharedClock(totalNodes, totalCp, time.Duration(50*int(totalCp)*totalNodes)*time.Millisecond)
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		mp2p := &p2pManipulator{nd: ps, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}
		src := NewSimRoundClock(mp2p, scMap)
		h := createTestHare(t, cfg, test.clock, host.ID(), src, t.Name())
		h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockRoracle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any()).Return(make([]byte, 100), nil).AnyTimes()
		h.mockRoracle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint16(1), nil).AnyTimes()
		h.mockRoracle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockMeshDB.EXPECT().RecordCoinflip(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		h.mockProposalDB.EXPECT().LayerProposals(gomock.Any()).DoAndReturn(
			func(layerID types.LayerID) ([]*types.Proposal, error) {
				return proposals[layerID], nil
			}).AnyTimes()
		h.mockFetcher.EXPECT().GetProposals(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		h.mockProposalDB.EXPECT().GetProposals(gomock.Any()).DoAndReturn(
			func(pids []types.ProposalID) ([]*types.Proposal, error) {
				props := make([]*types.Proposal, 0, len(pids))
				for _, pid := range pids {
					props = append(props, proposalsByID[pid])
				}
				return props, nil
			}).AnyTimes()
		h.mockBlockGen.EXPECT().GenerateBlock(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, layerID types.LayerID, props []*types.Proposal) (*types.Block, error) {
				assert.ElementsMatch(t, proposals[layerID], props)
				return blocks[layerID], nil
			}).AnyTimes()
		h.mockMeshDB.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, block *types.Block) error {
				assert.Equal(t, blocks[block.LayerIndex], block)
				return nil
			}).AnyTimes()
		h.mockMeshDB.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		h.newRoundClock = src.NewRoundClock
		test.hare = append(test.hare, h.Hare)
		e := h.Start(context.TODO())
		r.NoError(e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(ProtoName)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
			test.clock.advanceLayer()
			time.Sleep(500 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 100*time.Second)
}
