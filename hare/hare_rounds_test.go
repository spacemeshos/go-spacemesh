package hare

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

const skipMoreTests = true

type TestHareWrapper struct {
	*HareWrapper
}

func newTestHareWrapper(count int) *TestHareWrapper {
	w := &TestHareWrapper{
		HareWrapper: newHareWrapper(count),
	}
	return w
}

func (w *TestHareWrapper) Tick(layer types.LayerID) {
	for i := 0; i < len(w.lCh); i++ {
		log.Debug("tick layer %v for instance %v", layer, i)
		w.lCh[i] <- layer
	}
}

func (w *TestHareWrapper) LayerTicker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	j := types.GetEffectiveGenesis() + 1
	last := j + types.LayerID(w.totalCP)

	for ; j < last; j++ {
		w.Tick(j)
		select {
		case <-w.termination.CloseChannel():
			return
		case <-ticker.C:
			// do nothing
		}
	}
}

type funcOracle func(types.LayerID, int32, int, types.NodeID, []byte, *testHare) (uint16, error)
type funcLayers func(types.LayerID, *testHare) ([]types.BlockID, error)
type funcValidate func(types.LayerID, []types.BlockID, *testHare)
type testHare struct {
	*Hare
	oracle   funcOracle
	layers   funcLayers
	validate funcValidate
	N        int
}

func (h *testHare) CalcEligibility(ctx context.Context, layer types.LayerID, round int32, committee int, id types.NodeID, sig []byte) (uint16, error) {
	return h.oracle(layer, round, committee, id, sig, h)
}

func (testHare) Register(bool, string)   {}
func (testHare) Unregister(bool, string) {}
func (testHare) Validate(context.Context, types.LayerID, int32, int, types.NodeID, []byte, uint16) (bool, error) {
	return true, nil
}
func (testHare) Proof(context.Context, types.LayerID, int32) ([]byte, error) {
	return []byte{}, nil
}
func (testHare) IsIdentityActiveOnConsensusView(context.Context, string, types.LayerID) (bool, error) {
	return true, nil
}
func (h *testHare) HandleValidatedLayer(ctx context.Context, layer types.LayerID, ids []types.BlockID) {
	h.validate(layer, ids, h)
}
func (h *testHare) LayerBlockIds(layer types.LayerID) ([]types.BlockID, error) {
	return h.layers(layer, h)
}
func (h *testHare) RecordCoinflip(ctx context.Context, layerID types.LayerID, coinflip bool) {}

func createTestHare(tcfg config.Config, layersCh chan types.LayerID, p2p NetworkService, rolacle Rolacle, name string, bp meshProvider) *Hare {
	ed := signing.NewEdSigner()
	pub := ed.PublicKey()
	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: pub.Bytes()}
	hare := New(tcfg, p2p, ed, nodeID, validateBlock, isSynced, bp, rolacle, 10, &mockIdentityP{nid: nodeID},
		&MockStateQuerier{true, nil}, layersCh, log.NewDefault(name+"_"+ed.PublicKey().ShortString()))
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

	sim := service.NewSimulator()
	for i := 0; i < nodes; i++ {
		s := sim.NewNode()
		mp2p := &p2pManipulator{nd: s, stalledLayer: 1, err: errors.New("fake err")}
		w.lCh = append(w.lCh, make(chan types.LayerID, 1))
		h := &testHare{nil, oracle, bp, validate, i}
		h.Hare = createTestHare(cfg, w.lCh[i], mp2p, h, t.Name(), h)
		w.hare = append(w.hare, h.Hare)
		e := h.Start(context.TODO())
		r.NoError(e)
	}

	return w
}

func Test_HarePreRoundEmptySet(t *testing.T) {
	types.SetLayersPerEpoch(1)
	const nodes = 5
	const layers = 2
	m := [layers][nodes]int{}

	w := runNodesFor(t, nodes, 2, layers, 2, 5,
		func(layer types.LayerID, round int32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round/4 > 1 {
				t.Fatal("out of round limit")
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]types.BlockID, error) {
			return []types.BlockID{}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer - types.GetEffectiveGenesis() - 1
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
		func(layer types.LayerID, round int32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round%4 == statusRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]types.BlockID, error) {
			return []types.BlockID{}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer - types.GetEffectiveGenesis() - 1
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
		func(layer types.LayerID, round int32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round%4 == proposalRound {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]types.BlockID, error) {
			return []types.BlockID{}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer - types.GetEffectiveGenesis() - 1
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
		func(layer types.LayerID, round int32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round%4 == commitRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]types.BlockID, error) {
			return []types.BlockID{genBlockID(1)}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer - types.GetEffectiveGenesis() - 1
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
		func(layer types.LayerID, round int32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			if round%4 == notifyRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]types.BlockID, error) {
			return []types.BlockID{genBlockID(1)}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer - types.GetEffectiveGenesis() - 1
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
		func(layer types.LayerID, round int32, committee int, id types.NodeID, blocks []byte, hare *testHare) (uint16, error) {
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) ([]types.BlockID, error) {
			return []types.BlockID{genBlockID(int(layer))}, nil
		},
		func(layer types.LayerID, blocks []types.BlockID, hare *testHare) {
			l := layer - types.GetEffectiveGenesis() - 1
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
