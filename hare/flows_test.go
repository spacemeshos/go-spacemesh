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
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type HareWrapper struct {
	totalCP     uint32
	termination chan struct{}
	clock       *mockClock
	hare        []*Hare
	initialSets []*Set // all initial sets
	outputs     map[types.LayerID][]*Set
}

func newHareWrapper(totalCp uint32) *HareWrapper {
	hs := new(HareWrapper)
	hs.clock = newMockClock()
	hs.totalCP = totalCp
	hs.termination = make(chan struct{})
	hs.outputs = make(map[types.LayerID][]*Set, 0)

	return hs
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

	close(his.termination)
}

func (his *HareWrapper) WaitForTimedTermination(t *testing.T, timeout time.Duration) {
	timer := time.After(timeout)
	go his.waitForTermination()
	total := types.NewLayerID(his.totalCP)
	select {
	case <-timer:
		t.Fatal("Timeout")
		return
	case <-his.termination:
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
	if msg.Layer == m.stalledLayer && msg.Round < 8 && msg.Round != preRound {
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
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, WakeupDelta: time.Second, RoundDuration: time.Second, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(types.ProposalID{1})
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		p2pm := &p2pManipulator{nd: ps, stalledLayer: instanceID1, err: errors.New("fake err")}
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		tcp := createConsensusProcess(t, ctx, sig, true, cfg, oracle, p2pm, test.initialSets[i], instanceID1)
		test.procs = append(test.procs, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
		i++
	}
	test.Create(totalNodes, creationFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 40*time.Second)
}

type hareWithMocks struct {
	*Hare
	mockRoracle *mocks.MockRolacle
}

func createTestHare(tb testing.TB, db *sql.Database, tcfg config.Config, clock *mockClock, p2p pubsub.PublishSubsciber, name string) *hareWithMocks {
	tb.Helper()
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	pub := signer.PublicKey()
	nodeID := types.BytesToNodeID(pub.Bytes())
	ctrl := gomock.NewController(tb)
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(gomock.Any()).AnyTimes()
	patrol.EXPECT().CompleteHare(gomock.Any()).AnyTimes()
	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, nil).AnyTimes()
	mockStateQ := mocks.NewMockstateQuerier(ctrl)
	mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	mockSyncS := smocks.NewMockSyncStateProvider(ctrl)
	mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()

	mockRoracle := mocks.NewMockRolacle(ctrl)

	mNonceFetcher := mocks.NewMocknonceFetcher(ctrl)
	mNonceFetcher.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(0), nil).AnyTimes()

	hare := New(
		datastore.NewCachedDB(db, logtest.New(tb)),
		tcfg,
		p2p,
		signer,
		nodeID,
		make(chan LayerOutput, 100),
		mockSyncS,
		mockBeacons,
		mockRoracle,
		patrol,
		mockStateQ,
		clock,
		logtest.New(tb).WithName(name+"_"+signer.PublicKey().ShortString()),
		withNonceFetcher(mNonceFetcher),
	)
	p2p.Register(pubsub.HareProtocol, hare.GetHareMsgHandler())

	return &hareWithMocks{
		Hare:        hare,
		mockRoracle: mockRoracle,
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
	return m.Layer, m.Eligibility.Count
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

	r := require.New(t)
	totalCp := uint32(3)
	finalLyr := types.GetEffectiveGenesis().Add(totalCp)
	test := newHareWrapper(totalCp)
	totalNodes := 10
	cfg := config.Config{N: totalNodes, F: totalNodes / 2, WakeupDelta: time.Second, RoundDuration: 5 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)

	dbs := make([]*sql.Database, 0, totalNodes)
	for i := 0; i < totalNodes; i++ {
		dbs = append(dbs, sql.InMemory())
	}
	pList := make(map[types.LayerID][]types.ProposalID)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
		for i := uint64(0); i < 200; i++ {
			p := genLayerProposal(j, []types.TransactionID{})
			p.EpochData = &types.EpochData{
				Beacon: types.EmptyBeacon,
			}
			for x := 0; x < totalNodes; x++ {
				require.NoError(t, ballots.Add(dbs[x], &p.Ballot))
				require.NoError(t, proposals.Add(dbs[x], p))
			}
			pList[j] = append(pList[j], p.ID())
		}
	}
	var pubsubs []*pubsub.PubSub
	scMap := NewSharedClock(totalNodes, totalCp, time.Duration(50*int(totalCp)*totalNodes)*time.Millisecond)
	outputs := make([]map[types.LayerID]LayerOutput, totalNodes)
	var outputsWaitGroup sync.WaitGroup
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		src := NewSimRoundClock(ps, scMap)
		pubsubs = append(pubsubs, ps)
		h := createTestHare(t, dbs[i], cfg, test.clock, src, t.Name())
		h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockRoracle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(make([]byte, 100), nil).AnyTimes()
		h.mockRoracle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint16(1), nil).AnyTimes()
		h.mockRoracle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		outputsWaitGroup.Add(1)
		go func(idx int) {
			defer outputsWaitGroup.Done()
			for out := range h.blockGenCh {
				if outputs[idx] == nil {
					outputs[idx] = make(map[types.LayerID]LayerOutput)
				}
				outputs[idx][out.Layer] = out
			}
		}(i)
		h.newRoundClock = src.NewRoundClock
		test.hare = append(test.hare, h.Hare)
		e := h.Start(context.TODO())
		r.NoError(e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(pubsub.HareProtocol)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
			test.clock.advanceLayer()
			time.Sleep(250 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 80*time.Second)
	for _, h := range test.hare {
		close(h.blockGenCh)
	}
	outputsWaitGroup.Wait()
	for _, out := range outputs {
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			require.NotNil(t, out[lid])
			require.ElementsMatch(t, pList[lid], out[lid].Proposals)
		}
	}
}

// Test - run multiple CPs where one of them runs more than one iteration.
func Test_multipleCPsAndIterations(t *testing.T) {
	logtest.SetupGlobal(t)

	r := require.New(t)
	totalCp := uint32(4)
	finalLyr := types.GetEffectiveGenesis().Add(totalCp)
	test := newHareWrapper(totalCp)
	totalNodes := 10
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, WakeupDelta: time.Second, RoundDuration: 5 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)

	dbs := make([]*sql.Database, 0, totalNodes)
	for i := 0; i < totalNodes; i++ {
		dbs = append(dbs, sql.InMemory())
	}
	pList := make(map[types.LayerID][]types.ProposalID)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
		for i := uint64(0); i < 200; i++ {
			p := genLayerProposal(j, []types.TransactionID{})
			p.EpochData = &types.EpochData{
				Beacon: types.EmptyBeacon,
			}
			pList[j] = append(pList[j], p.ID())
			for x := 0; x < totalNodes; x++ {
				require.NoError(t, ballots.Add(dbs[x], &p.Ballot))
				require.NoError(t, proposals.Add(dbs[x], p))
			}
		}
	}
	var pubsubs []*pubsub.PubSub
	scMap := NewSharedClock(totalNodes, totalCp, time.Duration(50*int(totalCp)*totalNodes)*time.Millisecond)
	outputs := make([]map[types.LayerID]LayerOutput, totalNodes)
	var outputsWaitGroup sync.WaitGroup
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		mp2p := &p2pManipulator{nd: ps, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}
		src := NewSimRoundClock(mp2p, scMap)
		h := createTestHare(t, dbs[i], cfg, test.clock, src, t.Name())
		h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockRoracle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(make([]byte, 100), nil).AnyTimes()
		h.mockRoracle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint16(1), nil).AnyTimes()
		h.mockRoracle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		outputsWaitGroup.Add(1)
		go func(idx int) {
			defer outputsWaitGroup.Done()
			for out := range h.blockGenCh {
				if outputs[idx] == nil {
					outputs[idx] = make(map[types.LayerID]LayerOutput)
				}
				outputs[idx][out.Layer] = out
			}
		}(i)
		h.newRoundClock = src.NewRoundClock
		test.hare = append(test.hare, h.Hare)
		e := h.Start(context.TODO())
		r.NoError(e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(pubsub.HareProtocol)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
			test.clock.advanceLayer()
			time.Sleep(500 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 100*time.Second)
	for _, h := range test.hare {
		close(h.blockGenCh)
	}
	outputsWaitGroup.Wait()
	for _, out := range outputs {
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			require.NotNil(t, out[lid])
			require.ElementsMatch(t, pList[lid], out[lid].Proposals)
		}
	}
}
