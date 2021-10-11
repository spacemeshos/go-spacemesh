package hare

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/signing"
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
				blks, _ := p.GetResult(i)
				if len(blks) > 0 {
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
			blks, _ := p.GetResult(i)
			for _, b := range blks {
				s.Add(b)
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
	nd           *service.Node
	stalledLayer types.LayerID
	err          error
}

func (m *p2pManipulator) RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan service.GossipMessage {
	ch := m.nd.RegisterGossipProtocol(protocol, prio)
	wch := make(chan service.GossipMessage)

	go func() {
		for {
			x := <-ch
			wch <- x
		}
	}()

	return wch
}

func (m *p2pManipulator) Broadcast(ctx context.Context, protocol string, payload []byte) error {
	msg, _ := MessageFromBuffer(payload)
	if msg.InnerMsg.InstanceID == m.stalledLayer && msg.InnerMsg.K < 8 && msg.InnerMsg.K != preRound {
		return m.err
	}

	e := m.nd.Broadcast(ctx, protocol, payload)
	return fmt.Errorf("broadcast: %w", e)
}

type trueOracle struct{}

func (trueOracle) Register(bool, string) {
}

func (trueOracle) Unregister(bool, string) {
}

func (trueOracle) Validate(context.Context, types.LayerID, uint32, int, types.NodeID, []byte, uint16) (bool, error) {
	return true, nil
}

func (trueOracle) CalcEligibility(context.Context, types.LayerID, uint32, int, types.NodeID, []byte) (uint16, error) {
	return 1, nil
}

func (trueOracle) Proof(context.Context, types.LayerID, uint32) ([]byte, error) {
	x := make([]byte, 100)
	return x, nil
}

func (trueOracle) IsIdentityActiveOnConsensusView(context.Context, string, types.LayerID) (bool, error) {
	return true, nil
}

func (trueOracle) IsEpochBeaconReady(context.Context, types.EpochID) bool {
	return true
}

// Test - runs a single CP for more than one iteration.
func Test_consensusIterations(t *testing.T) {
	test := newConsensusTest()

	totalNodes := 20
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 1, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := &trueOracle{}
	i := 0
	creationFunc := func() {
		s := sim.NewNode()
		p2pm := &p2pManipulator{nd: s, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}
		proc := createConsensusProcess(t, true, cfg, oracle, p2pm, test.initialSets[i], types.NewLayerID(1), t.Name())
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(totalNodes, creationFunc)
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func isSynced(context.Context) bool {
	return true
}

type mockIdentityP struct {
	nid types.NodeID
}

func (m *mockIdentityP) GetIdentity(string) (types.NodeID, error) {
	return m.nid, nil
}

func buildSet() []types.BlockID {
	rng := rand.New(rand.NewSource(0))
	s := make([]types.BlockID, 200, 200)
	for i := uint64(0); i < 200; i++ {
		s = append(s, newRandBlockID(rng))
	}
	return s
}

func newRandBlockID(rng *rand.Rand) (id types.BlockID) {
	_, err := rng.Read(id[:])
	if err != nil {
		panic(err)
	}
	return id
}

type mockBlockProvider struct{}

func (mbp *mockBlockProvider) HandleValidatedLayer(context.Context, types.LayerID, []types.BlockID) {
}

func (mbp *mockBlockProvider) InvalidateLayer(context.Context, types.LayerID) {
}

func (mbp *mockBlockProvider) RecordCoinflip(context.Context, types.LayerID, bool) {
}

func (mbp *mockBlockProvider) LayerBlockIds(types.LayerID) ([]types.BlockID, error) {
	return buildSet(), nil
}

func createMaatuf(tb testing.TB, tcfg config.Config, clock *mockClock, p2p NetworkService, rolacle Rolacle, name string) *Hare {
	ed := signing.NewEdSigner()
	pub := ed.PublicKey()
	_, vrfPub, err := signing.NewVRFSigner(ed.Sign(pub.Bytes()))
	if err != nil {
		panic("failed to create vrf signer")
	}
	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: vrfPub}
	hare := New(tcfg, p2p, ed, nodeID, isSynced, &mockBlockProvider{}, rolacle, 10, &mockIdentityP{nid: nodeID},
		&MockStateQuerier{true, nil}, clock, logtest.New(tb).WithName(name+"_"+ed.PublicKey().ShortString()))

	return hare
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
	s      NetworkService
}

func (c *SimRoundClock) RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan service.GossipMessage {
	return c.s.RegisterGossipProtocol(protocol, prio)
}

func (c *SimRoundClock) Broadcast(ctx context.Context, protocol string, payload []byte) error {
	instanceID, cnt := extractInstanceID(payload)

	c.m.Lock()
	clock := c.clocks[instanceID]
	clock.IncMessages(cnt)
	c.m.Unlock()

	if err := c.s.Broadcast(ctx, protocol, payload); err != nil {
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

func NewSimRoundClock(s NetworkService, clocks map[types.LayerID]*SharedRoundClock) *SimRoundClock {
	return &SimRoundClock{
		clocks: clocks,
		s:      s,
	}
}

// Test - run multiple CPs simultaneously.
func Test_multipleCPs(t *testing.T) {
	// NOTE(dshulyak) spams with overwriting sessionID in context
	logtest.SetupGlobal(t)

	types.SetLayersPerEpoch(4)
	r := require.New(t)
	totalCp := uint32(3)
	test := newHareWrapper(totalCp)
	totalNodes := 20
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 5, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	oracle := &trueOracle{}
	scMap := NewSharedClock(totalNodes, totalCp, time.Duration(50*int(totalCp)*totalNodes)*time.Millisecond)
	for i := 0; i < totalNodes; i++ {
		s := sim.NewNode()
		src := NewSimRoundClock(s, scMap)
		h := createMaatuf(t, cfg, test.clock, src, oracle, t.Name())
		h.newRoundClock = src.NewRoundClock
		test.hare = append(test.hare, h)
		e := h.Start(context.TODO())
		r.NoError(e)
	}

	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
			test.clock.advanceLayer()
			time.Sleep(250 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 60*time.Second)
}

// Test - run multiple CPs where one of them runs more than one iteration.
func Test_multipleCPsAndIterations(t *testing.T) {
	logtest.SetupGlobal(t)

	types.SetLayersPerEpoch(4)
	r := require.New(t)
	totalCp := uint32(4)
	test := newHareWrapper(totalCp)
	totalNodes := 20
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 3, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	oracle := &trueOracle{}
	scMap := NewSharedClock(totalNodes, totalCp, time.Duration(50*int(totalCp)*totalNodes)*time.Millisecond)
	for i := 0; i < totalNodes; i++ {
		s := sim.NewNode()
		mp2p := &p2pManipulator{nd: s, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}
		src := NewSimRoundClock(mp2p, scMap)
		h := createMaatuf(t, cfg, test.clock, src, oracle, t.Name())
		h.newRoundClock = src.NewRoundClock
		test.hare = append(test.hare, h)
		e := h.Start(context.TODO())
		r.NoError(e)
	}

	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
			test.clock.advanceLayer()
			time.Sleep(500 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 50*time.Second)
}
