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

type hareWithMocks struct {
	*Hare
	mockRoracle *mocks.MockRolacle
}

func createTestHare(tb testing.TB, msh mesh, tcfg config.Config, clock *mockClock, p2p pubsub.PublishSubsciber, name string) *hareWithMocks {
	tb.Helper()
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	pub := signer.PublicKey()
	nodeID := types.BytesToNodeID(pub.Bytes())
	pke, err := signing.NewPubKeyExtractor()
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
		pke,
		nodeID,
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

// Test - run multiple CPs simultaneously.
func Test_multipleCPs(t *testing.T) {
	t.Skip("pending https://github.com/spacemeshos/go-spacemesh/issues/4001")

	logtest.SetupGlobal(t)

	r := require.New(t)
	totalCp := uint32(3)
	finalLyr := types.GetEffectiveGenesis().Add(totalCp)
	test := newHareWrapper(totalCp)
	totalNodes := 10
	networkDelay := time.Second * 4
	cfg := config.Config{N: totalNodes, WakeupDelta: networkDelay, RoundDuration: networkDelay, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)

	pList := make(map[types.LayerID][]*types.Proposal)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
		for i := uint64(0); i < 20; i++ {
			p := genLayerProposal(j, []types.TransactionID{})
			p.EpochData = &types.EpochData{
				Beacon: types.EmptyBeacon,
			}
			pList[j] = append(pList[j], p)
		}
	}
	meshes := make([]*mocks.Mockmesh, 0, totalNodes)
	ctrl, ctx := gomock.WithContext(ctx, t)
	for i := 0; i < totalNodes; i++ {
		mockMesh := mocks.NewMockmesh(ctrl)
		mockMesh.EXPECT().GetEpochAtx(gomock.Any(), gomock.Any()).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil).AnyTimes()
		mockMesh.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(0), nil).AnyTimes()
		mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
		mockMesh.EXPECT().SetWeakCoin(gomock.Any(), gomock.Any()).AnyTimes()
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			mockMesh.EXPECT().Proposals(lid).Return(pList[lid], nil)
			for _, p := range pList[lid] {
				mockMesh.EXPECT().GetAtxHeader(p.AtxID).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil).AnyTimes()
				mockMesh.EXPECT().Ballot(p.Ballot.ID()).Return(&p.Ballot, nil).AnyTimes()
			}
		}
		mockMesh.EXPECT().Proposals(gomock.Any()).Return([]*types.Proposal{}, nil).AnyTimes()
		meshes = append(meshes, mockMesh)
	}

	var pubsubs []*pubsub.PubSub
	outputs := make([]map[types.LayerID]LayerOutput, totalNodes)
	var outputsWaitGroup sync.WaitGroup
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		h := createTestHare(t, meshes[i], cfg, test.clock, ps, t.Name())
		h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockRoracle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(types.RandomVrfSignature(), nil).AnyTimes()
		h.mockRoracle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint16(1), nil).AnyTimes()
		h.mockRoracle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		outputsWaitGroup.Add(1)
		go func(idx int) {
			defer outputsWaitGroup.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case out, ok := <-h.blockGenCh:
					if !ok {
						return
					}
					if outputs[idx] == nil {
						outputs[idx] = make(map[types.LayerID]LayerOutput)
					}
					outputs[idx][out.Layer] = out
				}
			}
		}(i)
		test.hare = append(test.hare, h.Hare)
		e := h.Start(ctx)
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
	layerDuration := 250 * time.Millisecond
	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
			test.clock.advanceLayer()
			time.Sleep(layerDuration)
		}
	}()

	// There are 5 rounds per layer and totalCPs layers and we double for good measure.
	test.WaitForTimedTermination(t, 2*networkDelay*5*time.Duration(totalCp))
	for _, h := range test.hare {
		close(h.blockGenCh)
	}
	outputsWaitGroup.Wait()
	for _, out := range outputs {
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			require.NotNil(t, out[lid])
			require.ElementsMatch(t, types.ToProposalIDs(pList[lid]), out[lid].Proposals)
		}
	}
	t.Cleanup(func() {
		for _, h := range test.hare {
			h.Close()
		}
	})
}

// Test - run multiple CPs where one of them runs more than one iteration.
func Test_multipleCPsAndIterations(t *testing.T) {
	t.Skip("pending https://github.com/spacemeshos/go-spacemesh/issues/4001")

	logtest.SetupGlobal(t)

	r := require.New(t)
	totalCp := uint32(4)
	finalLyr := types.GetEffectiveGenesis().Add(totalCp)
	test := newHareWrapper(totalCp)
	totalNodes := 10
	networkDelay := time.Second * 4
	cfg := config.Config{N: totalNodes, WakeupDelta: networkDelay, RoundDuration: networkDelay, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)

	pList := make(map[types.LayerID][]*types.Proposal)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
		for i := uint64(0); i < 20; i++ {
			p := genLayerProposal(j, []types.TransactionID{})
			p.EpochData = &types.EpochData{
				Beacon: types.EmptyBeacon,
			}
			pList[j] = append(pList[j], p)
		}
	}

	meshes := make([]*mocks.Mockmesh, 0, totalNodes)
	ctrl, ctx := gomock.WithContext(ctx, t)
	for i := 0; i < totalNodes; i++ {
		mockMesh := mocks.NewMockmesh(ctrl)
		mockMesh.EXPECT().GetEpochAtx(gomock.Any(), gomock.Any()).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil).AnyTimes()
		mockMesh.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(0), nil).AnyTimes()
		mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
		mockMesh.EXPECT().SetWeakCoin(gomock.Any(), gomock.Any()).AnyTimes()
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			mockMesh.EXPECT().Proposals(lid).Return(pList[lid], nil)
			for _, p := range pList[lid] {
				mockMesh.EXPECT().GetAtxHeader(p.AtxID).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil).AnyTimes()
				mockMesh.EXPECT().Ballot(p.Ballot.ID()).Return(&p.Ballot, nil).AnyTimes()
			}
		}
		mockMesh.EXPECT().Proposals(gomock.Any()).Return([]*types.Proposal{}, nil).AnyTimes()
		meshes = append(meshes, mockMesh)
	}

	var pubsubs []*pubsub.PubSub
	outputs := make([]map[types.LayerID]LayerOutput, totalNodes)
	var outputsWaitGroup sync.WaitGroup
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		mp2p := &p2pManipulator{nd: ps, stalledLayer: types.GetEffectiveGenesis().Add(1), err: errors.New("fake err")}
		h := createTestHare(t, meshes[i], cfg, test.clock, mp2p, t.Name())
		h.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h.mockRoracle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(types.RandomVrfSignature(), nil).AnyTimes()
		h.mockRoracle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint16(1), nil).AnyTimes()
		h.mockRoracle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		outputsWaitGroup.Add(1)
		go func(idx int) {
			defer outputsWaitGroup.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case out, ok := <-h.blockGenCh:
					if !ok {
						return
					}
					if outputs[idx] == nil {
						outputs[idx] = make(map[types.LayerID]LayerOutput)
					}
					outputs[idx][out.Layer] = out
				}
			}
		}(i)
		test.hare = append(test.hare, h.Hare)
		e := h.Start(ctx)
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
	layerDuration := 250 * time.Millisecond
	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(finalLyr); j = j.Add(1) {
			test.clock.advanceLayer()
			time.Sleep(layerDuration)
		}
	}()

	// There are 5 rounds per layer and totalCPs layers and we double to allow
	// for the for good measure. Also one layer in this test will run 2
	// iterations so we increase the layer count by 1.
	test.WaitForTimedTermination(t, 2*networkDelay*5*time.Duration(totalCp+1))
	for _, h := range test.hare {
		close(h.blockGenCh)
	}
	outputsWaitGroup.Wait()
	for _, out := range outputs {
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(finalLyr); lid = lid.Add(1) {
			require.NotNil(t, out[lid])
			require.ElementsMatch(t, types.ToProposalIDs(pList[lid]), out[lid].Proposals)
		}
	}
	t.Cleanup(func() {
		for _, h := range test.hare {
			h.Close()
		}
	})
}
