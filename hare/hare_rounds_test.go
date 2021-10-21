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
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/lp2p"
	"github.com/spacemeshos/go-spacemesh/lp2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const skipMoreTests = true

type TestHareWrapper struct {
	*HareWrapper
}

func newTestHareWrapper(count int) *TestHareWrapper {
	w := &TestHareWrapper{
		HareWrapper: newHareWrapper(uint32(count)),
	}
	return w
}

func (w *TestHareWrapper) LayerTicker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	j := types.GetEffectiveGenesis().Add(1)
	last := j.Add(w.totalCP)

	for ; j.Before(last); j = j.Add(1) {
		w.clock.advanceLayer()
		select {
		case <-w.termination.CloseChannel():
			return
		case <-ticker.C:
			// do nothing
		}
	}
}

type (
	funcOracle   func(types.LayerID, uint32, int, types.NodeID, []byte, *testHare) (uint16, error)
	funcLayers   func(types.LayerID, *testHare) ([]*types.Block, error)
	funcValidate func(types.LayerID, []types.BlockID, *testHare)
	testHare     struct {
		*Hare
		oracle   funcOracle
		layers   funcLayers
		validate funcValidate
		N        int
	}
)

func (h *testHare) CalcEligibility(ctx context.Context, layer types.LayerID, round uint32, committee int, id types.NodeID, sig []byte) (uint16, error) {
	return h.oracle(layer, round, committee, id, sig, h)
}

func (testHare) Register(bool, string)   {}
func (testHare) Unregister(bool, string) {}

func (testHare) Validate(context.Context, types.LayerID, uint32, int, types.NodeID, []byte, uint16) (bool, error) {
	return true, nil
}

func (testHare) Proof(context.Context, types.LayerID, uint32) ([]byte, error) {
	return []byte{}, nil
}

func (testHare) IsIdentityActiveOnConsensusView(context.Context, string, types.LayerID) (bool, error) {
	return true, nil
}

func (h *testHare) HandleValidatedLayer(ctx context.Context, layer types.LayerID, ids []types.BlockID) {
	h.validate(layer, ids, h)
}

func (h *testHare) LayerBlocks(layer types.LayerID) ([]*types.Block, error) {
	return h.layers(layer, h)
}

func (h *testHare) GetBlock(id types.BlockID) (*types.Block, error) {
	return nil, nil
}

func (h *testHare) InvalidateLayer(ctx context.Context, layerID types.LayerID) {
	panic("implement me")
}
func (h *testHare) RecordCoinflip(ctx context.Context, layerID types.LayerID, coinflip bool) {}

func createTestHare(tb testing.TB, tcfg config.Config, clock *mockClock, pid lp2p.Peer, p2p pubsub.PublishSubsciber, rolacle Rolacle, name string, bp meshProvider) *Hare {
	ed := signing.NewEdSigner()
	pub := ed.PublicKey()
	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: pub.Bytes()}

	ctrl := gomock.NewController(tb)
	defer ctrl.Finish()

	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(nil, nil).AnyTimes()
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().SetHareInCharge(gomock.Any()).AnyTimes()
	hare := New(tcfg, pid, p2p, ed, nodeID, isSynced, bp, mockBeacons, rolacle, patrol, 10, &mockIdentityP{nid: nodeID},
		&MockStateQuerier{true, nil}, clock, logtest.New(tb).WithName(name+"_"+ed.PublicKey().ShortString()))
	return hare
}

func runNodesFor(t *testing.T, nodes, leaders, maxLayers, limitIterations, concurrent int, oracle funcOracle, bp funcLayers, validate funcValidate) *TestHareWrapper {
	r := require.New(t)
	w := newTestHareWrapper(maxLayers)
	cfg := config.Config{
		N:               nodes,
		F:               nodes/2 - 1,
		RoundDuration:   1,
		ExpectedLeaders: leaders,
		LimitIterations: limitIterations,
		LimitConcurrent: maxLayers,
	}

	mesh, err := mocknet.FullMeshLinked(context.TODO(), nodes)
	require.NoError(t, err)
	for i := 0; i < nodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(context.TODO(), logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		mp2p := &p2pManipulator{nd: ps, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}
		h := &testHare{nil, oracle, bp, validate, i}
		h.Hare = createTestHare(t, cfg, w.clock, host.ID(), mp2p, h, t.Name(), h)
		w.hare = append(w.hare, h.Hare)
		e := h.Start(context.TODO())
		r.NoError(e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())

	return w
}

func Test_HarePreRoundEmptySet(t *testing.T) {
	types.SetLayersPerEpoch(1)
	const nodes = 5
	const layers = 2
	m := [layers][nodes]int{}

	w := runNodesFor(t, nodes, 2, layers, 2, 5,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round/4 > 1 && round != preRound {
				t.Fatalf("out of round %d limit", round)
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]*types.Block, error) {
			return []*types.Block{}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = len(blocks) + 1
		})

	w.LayerTicker(100 * time.Millisecond)
	time.Sleep(time.Second * 6)

	for x := range m {
		for y := range m[x] {
			if m[x][y] != 1 {
				t.Errorf("at layer %v node %v has non-empty set in result (%v)", x, y, m[x][y])
			}
		}
	}
}

func Test_HareNotEnoughStatuses(t *testing.T) {
	if skipMoreTests {
		t.SkipNow()
	}

	types.SetLayersPerEpoch(1)
	const nodes = 5
	const layers = 2
	m := [layers][nodes]int{}

	w := runNodesFor(t, nodes, 2, layers, 1, 5,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round%4 == statusRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]*types.Block, error) {
			return []*types.Block{}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = len(blocks) + 1
		})

	w.LayerTicker(1 * time.Second)
	time.Sleep(time.Second * 6)

	for x := range m {
		for y := range m[x] {
			if m[x][y] != 1 {
				t.Errorf("at layer %v node %v has non-empty set in result (%v)", x, y, m[x][y])
			}
		}
	}
}

func Test_HareNotEnoughLeaders(t *testing.T) {
	if skipMoreTests {
		t.SkipNow()
	}
	types.SetLayersPerEpoch(1)
	const nodes = 5
	const layers = 2
	m := [layers][nodes]int{}

	w := runNodesFor(t, nodes, 2, layers, 1, 5,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round%4 == proposalRound {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]*types.Block, error) {
			return []*types.Block{}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = len(blocks) + 1
		})

	w.LayerTicker(1 * time.Second)
	time.Sleep(time.Second * 6)

	for x := range m {
		for y := range m[x] {
			if m[x][y] != 1 {
				t.Errorf("at layer %v node %v has non-empty set in result (%v)", x, y, m[x][y])
			}
		}
	}
}

func Test_HareNotEnoughCommits(t *testing.T) {
	if skipMoreTests {
		t.SkipNow()
	}
	types.SetLayersPerEpoch(1)
	const nodes = 6
	const layers = 2
	m := [layers][nodes]int{}

	w := runNodesFor(t, nodes, 2, layers, 1, 5,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round%4 == commitRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]*types.Block, error) {
			return []*types.Block{randomBlock(t, layer, nil)}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = len(blocks) + 1
		})

	w.LayerTicker(100 * time.Millisecond)
	time.Sleep(time.Second * 6)

	for x := range m {
		for y := range m[x] {
			if m[x][y] != 1 {
				t.Errorf("at layer %v node %v has non-empty set in result (%v)", x, y, m[x][y])
			}
		}
	}
}

func Test_HareNotEnoughNotifications(t *testing.T) {
	if skipMoreTests {
		t.SkipNow()
	}
	types.SetLayersPerEpoch(1)
	const nodes = 6
	const layers = 2
	m := [layers][nodes]int{}

	w := runNodesFor(t, nodes, 2, layers, 1, 5,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round%4 == notifyRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]*types.Block, error) {
			return []*types.Block{randomBlock(t, layer, nil)}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = len(blocks) + 1
		})

	w.LayerTicker(100 * time.Millisecond)
	time.Sleep(time.Second * 6)

	for x := range m {
		for y := range m[x] {
			if m[x][y] != 1 {
				t.Errorf("at layer %v node %v has non-empty set in result (%v)", x, y, m[x][y])
			}
		}
	}
}

func Test_HareComplete(t *testing.T) {
	if skipMoreTests {
		t.SkipNow()
	}
	types.SetLayersPerEpoch(1)
	const nodes = 6
	const layers = 2
	m := [layers][nodes]int{}

	w := runNodesFor(t, nodes, 2, layers, 1, 5,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]*types.Block, error) {
			return []*types.Block{randomBlock(t, layer, nil)}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = len(blocks)
		})

	w.LayerTicker(100 * time.Millisecond)
	time.Sleep(time.Second * 6)

	for x := range m {
		for y := range m[x] {
			if m[x][y] != 1 {
				t.Errorf("at layer %v node %v has emty set in result", x, y)
			}
		}
	}
}
