package hare

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	bMocks "github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/lp2p"
	"github.com/spacemeshos/go-spacemesh/lp2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type HareWrapper struct {
	totalCP     uint32
	termination util.Closer
	lCh         []chan types.LayerID
	hare        []*Hare
	initialSets []*Set // all initial sets
	outputs     map[types.LayerID][]*Set
	name        string
}

func newHareWrapper(totalCp uint32) *HareWrapper {
	hs := new(HareWrapper)
	hs.lCh = make([]chan types.LayerID, 0)
	hs.totalCP = totalCp
	hs.termination = util.NewCloser()
	hs.outputs = make(map[types.LayerID][]*Set, 0)

	return hs
}

func (his *HareWrapper) fill(set *Set, begin int, end int) {
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
	nd           pubsub.PublisherSubscriber
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
	return m.nd.Publish(ctx, protocol, payload)
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

// Test - runs a single CP for more than one iteration.
func Test_consensusIterations(t *testing.T) {
	test := newConsensusTest()

	totalNodes := 20
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 1, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := &trueOracle{}
	i := 0
	creationFunc := func() {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		p2pm := &p2pManipulator{nd: ps, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}
		proc := createConsensusProcess(t, true, cfg, oracle, p2pm, test.initialSets[i], types.NewLayerID(1), t.Name())
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(totalNodes, creationFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
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

type mockBlockProvider struct {
	lyrBlocks map[types.LayerID][]*types.Block
}

func (mbp *mockBlockProvider) HandleValidatedLayer(context.Context, types.LayerID, []types.BlockID) {
}

func (mbp *mockBlockProvider) InvalidateLayer(context.Context, types.LayerID) {
}

func (mbp *mockBlockProvider) RecordCoinflip(context.Context, types.LayerID, bool) {
}

func (mbp *mockBlockProvider) LayerBlocks(lyrID types.LayerID) ([]*types.Block, error) {
	return mbp.lyrBlocks[lyrID], nil
}

func (mbp *mockBlockProvider) GetBlock(bID types.BlockID) (*types.Block, error) {
	return nil, nil
}

func createMaatuf(t testing.TB, tcfg config.Config, layersCh chan types.LayerID, pid lp2p.Peer, p2p pubsub.PublisherSubscriber, rolacle Rolacle, name string, lyrBlocks map[types.LayerID][]*types.Block) *Hare {
	ed := signing.NewEdSigner()
	pub := ed.PublicKey()
	_, vrfPub, err := signing.NewVRFSigner(ed.Sign(pub.Bytes()))
	if err != nil {
		panic("failed to create vrf signer")
	}
	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: vrfPub}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(gomock.Any()).AnyTimes()
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(nil, nil).AnyTimes()

	hare := New(tcfg, pid, p2p, ed, nodeID, isSynced, &mockBlockProvider{lyrBlocks: lyrBlocks}, mockBeacons, rolacle, patrol, 10, &mockIdentityP{nid: nodeID},
		&MockStateQuerier{true, nil}, layersCh, logtest.New(t).WithName(name+"_"+ed.PublicKey().ShortString()))

	return hare
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	oracle := &trueOracle{}

	lyrBlocks := make(map[types.LayerID][]*types.Block)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
		for i := uint64(0); i < 200; i++ {
			blk := randomBlock(t, j, nil)
			lyrBlocks[j] = append(lyrBlocks[j], blk)
		}
	}
	pubsubs := []*pubsub.PubSub{}
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		// p2pm := &p2pManipulator{nd: s, err: errors.New("fake err")}
		test.lCh = append(test.lCh, make(chan types.LayerID, 1))
		h := createMaatuf(t, cfg, test.lCh[i], host.ID(), ps, oracle, t.Name(), lyrBlocks)
		test.hare = append(test.hare, h)
		e := h.Start(context.TODO())
		r.NoError(e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(protoName)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
			for i := 0; i < len(test.lCh); i++ {
				test.lCh[i] <- j
			}
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	oracle := &trueOracle{}

	lyrBlocks := make(map[types.LayerID][]*types.Block)
	for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
		for i := uint64(0); i < 200; i++ {
			blk := randomBlock(t, j, nil)
			lyrBlocks[j] = append(lyrBlocks[j], blk)
		}
	}
	pubsubs := []*pubsub.PubSub{}
	for i := 0; i < totalNodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		pubsubs = append(pubsubs, ps)
		mp2p := &p2pManipulator{nd: ps, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}
		test.lCh = append(test.lCh, make(chan types.LayerID, 1))
		h := createMaatuf(t, cfg, test.lCh[i], host.ID(), mp2p, oracle, t.Name(), lyrBlocks)
		test.hare = append(test.hare, h)
		e := h.Start(context.TODO())
		r.NoError(e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())
	require.Eventually(t, func() bool {
		for _, ps := range pubsubs {
			if len(ps.ProtocolPeers(protoName)) != len(mesh.Hosts())-1 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	go func() {
		for j := types.GetEffectiveGenesis().Add(1); !j.After(types.GetEffectiveGenesis().Add(totalCp)); j = j.Add(1) {
			for i := 0; i < len(test.lCh); i++ {
				test.lCh[i] <- j
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 50*time.Second)
}
