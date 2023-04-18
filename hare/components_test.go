package hare

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type HareWrapper struct {
	totalCP     uint32
	termination chan struct{}
	clock       *mockClock
	hare        []*Hare
	//lint:ignore U1000 pending https://github.com/spacemeshos/go-spacemesh/issues/4001
	initialSets []*Set
	outputs     map[types.LayerID][]*Set
	mesh        mocknet.Mocknet
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

		time.Sleep(10 * time.Millisecond)
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
	total := types.LayerID(his.totalCP)
	select {
	case <-timer:
		t.Fatal("Timeout")
		return
	case <-his.termination:
		for i := types.LayerID(1); !i.After(total); i = i.Add(1) {
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

func (his *HareWrapper) Close() {
	his.mesh.Close()
	for _, h := range his.hare {
		h.Close()
	}
}

type hareWithMocks struct {
	*Hare
	mockRoracle *mocks.MockRolacle
}

func createTestHare(tb testing.TB, msh mesh, tcfg config.Config, clock *mockClock, p2p pubsub.PublishSubsciber, name string) *hareWithMocks {
	tb.Helper()
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	edVerifier, err := signing.NewEdVerifier()
	require.NoError(tb, err)

	ctrl := gomock.NewController(tb)
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(gomock.Any()).AnyTimes()
	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, nil).AnyTimes()
	mockStateQ := mocks.NewMockstateQuerier(ctrl)
	mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	mockSyncS := smocks.NewMockSyncStateProvider(ctrl)
	mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()

	mockRoracle := mocks.NewMockRolacle(ctrl)

	hare := New(
		nil,
		tcfg,
		p2p,
		signer,
		edVerifier,
		signer.NodeID(),
		make(chan LayerOutput, 100),
		mockSyncS,
		mockBeacons,
		mockRoracle,
		patrol,
		mockStateQ,
		clock,
		logtest.New(tb).WithName(name+"_"+signer.PublicKey().ShortString()),
		withMesh(msh),
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

func (m *mockClock) CurrentLayer() types.LayerID {
	m.m.RLock()
	defer m.m.RUnlock()

	return m.currentLayer
}

func (m *mockClock) advanceLayer() {
	m.m.Lock()
	defer m.m.Unlock()

	m.layerTime[m.currentLayer] = time.Now()
	if ch, ok := m.channels[m.currentLayer]; ok {
		close(ch)
		delete(m.channels, m.currentLayer)
	}
	m.currentLayer = m.currentLayer.Add(1)
}

// The sharedRoundClock acts as a synchronization mechanism to allow for rounds
// to progress only when some threshold of messages have been received. This
// allows for reliable tests because it removes the possibility of tests
// failing due to late arrival of messages. It also allows for fast tests
// because as soon as the required number of messages have been received the
// next round is started, this avoids any downtime that may have occurred in
// situations where we set a fixed round time. However there is a caveat,
// because this is hooking into the PubSub system there is still a delay
// between messages being delivered and hare actually processing those
// messages, that delay is accounted for by the processingDelay field. Setting
// this too low will result in tests failing due to late message delivery,
// increasing the value does however increase overall test time, so we don't
// want this to be huge. A better solution would be to extract message
// processing from consensus processes so that we could hook into actual
// message delivery to Hare, see - https://github.com/spacemeshos/go-spacemesh/issues/4248.
type sharedRoundClock struct {
	currentRound     uint32
	rounds           map[uint32]chan struct{}
	minCount         int
	processingDelay  time.Duration
	messageCount     int
	advanceScheduled bool
	m                sync.Mutex
	layer            types.LayerID
}

func (c *sharedRoundClock) AwaitWakeup() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (c *sharedRoundClock) AwaitEndOfRound(round uint32) <-chan struct{} {
	c.m.Lock()
	defer c.m.Unlock()
	return c.getRound(round)
}

// getRound returns the channel for the given round, if there is no channel for
// that round it will be created.
func (c *sharedRoundClock) getRound(round uint32) chan struct{} {
	ch, ok := c.rounds[round]
	if !ok {
		ch = make(chan struct{})
		c.rounds[round] = ch
	}
	return ch
}

// RoundEnd is currently called only by metrics reporting code, so the returned
// value should not affect how components behave.
func (c *sharedRoundClock) RoundEnd(round uint32) time.Time {
	return time.Now()
}

// incMessages increments the message count for the current round by cnt. If
// the required threshold has been met the clock will advance to the next round
// after the processingDelay.
func (c *sharedRoundClock) incMessages(cnt int) {
	c.m.Lock()
	defer c.m.Unlock()

	c.messageCount += cnt
	if c.messageCount >= c.minCount && !c.advanceScheduled {
		time.AfterFunc(c.processingDelay, func() {
			c.m.Lock()
			defer c.m.Unlock()
			c.advanceScheduled = false
			c.advanceRound()
		})
		c.advanceScheduled = true
	}
}

func (c *sharedRoundClock) advanceRound() {
	c.messageCount = 0
	c.currentRound++
	if prevRound, ok := c.rounds[c.currentRound-1]; ok {
		close(prevRound)
	}
}

// advanceToRound advances the clock to the given round.
func (c *sharedRoundClock) advanceToRound(round uint32) {
	c.m.Lock()
	defer c.m.Unlock()

	for c.currentRound < round {
		// Ensure that the channel exists before we close it
		c.getRound(c.currentRound)
		c.advanceRound()
	}
}

// sharedRoundClocks provides functionality to create shared round clocks that
// can be injected into Hare, it uses a map to enusre that all hare instances
// share the same round clock instance for each layer and provies methods to
// safely interact with the map from multiple goroutines.
type sharedRoundClocks struct {
	// The number of messages that need to be received before progressing to
	// the next round.
	roundMessageCount int
	// The time to wait between reaching the roundMessageCount and progressing
	// to the next round, this is to allow for messages received over pubsub to
	// be processed by hare before moving to the next round.
	processingDelay time.Duration
	clocks          map[types.LayerID]*sharedRoundClock
	m               sync.Mutex
}

func newSharedRoundClocks(roundMessageCount int, processingDelay time.Duration) *sharedRoundClocks {
	return &sharedRoundClocks{
		roundMessageCount: roundMessageCount,
		processingDelay:   processingDelay,
		clocks:            make(map[types.LayerID]*sharedRoundClock),
	}
}

// roundClock returns the shared round clock for the given layer, if the clock doesn't exist it will be created.
func (rc *sharedRoundClocks) roundClock(layer types.LayerID) RoundClock {
	rc.m.Lock()
	defer rc.m.Unlock()
	c, exist := rc.clocks[layer]
	if !exist {
		c = &sharedRoundClock{
			currentRound:    preRound,
			rounds:          make(map[uint32]chan struct{}),
			minCount:        rc.roundMessageCount,
			processingDelay: rc.processingDelay,
			messageCount:    0,
			layer:           layer,
		}
		rc.clocks[layer] = c
	}
	return c
}

// clock returns the clock for the given layer, if the clock does not exist the
// returned value will be nil.
func (rc *sharedRoundClocks) clock(layer types.LayerID) *sharedRoundClock {
	rc.m.Lock()
	defer rc.m.Unlock()
	return rc.clocks[layer]
}

func extractInstanceID(payload []byte) (types.LayerID, uint16) {
	m, err := MessageFromBuffer(payload)
	if err != nil {
		panic(err)
	}
	return m.Layer, m.Eligibility.Count
}

// testPublisherSubscriber wraps a PublisherSubscriber and hooks into the
// message handler to be able to be notified of received messages.
type testPublisherSubscriber struct {
	inner    pubsub.PublishSubsciber
	publish  func(ctx context.Context, protocol string, message []byte) error
	register func(protocol string, handler pubsub.GossipHandler)
}

func (ps *testPublisherSubscriber) Register(protocol string, handler pubsub.GossipHandler) {
	if ps.register == nil {
		ps.inner.Register(protocol, handler)
	} else {
		ps.register(protocol, handler)
	}
}

func (ps *testPublisherSubscriber) Publish(ctx context.Context, protocol string, message []byte) error {
	if ps.publish == nil {
		return ps.inner.Publish(ctx, protocol, message)
	} else {
		return ps.publish(ctx, protocol, message)
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
