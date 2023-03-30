package hare

import (
	"context"
	"errors"
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
		case <-w.termination:
			return
		case <-ticker.C:
			// do nothing
		}
	}
}

type (
	funcOracle   func(types.LayerID, uint32, int, types.NodeID, types.VrfSignature, *testHare) (uint16, error)
	funcValidate func(types.LayerID, *testHare)
	testHare     struct {
		*hareWithMocks
		N int
	}
)

func runNodesFor(t *testing.T, ctx context.Context, nodes, leaders, maxLayers, limitIterations int, createProposal bool, oracle funcOracle, validate funcValidate) *TestHareWrapper {
	r := require.New(t)
	w := newTestHareWrapper(maxLayers)
	cfg := config.Config{
		N:               nodes,
		WakeupDelta:     time.Second,
		RoundDuration:   time.Second,
		ExpectedLeaders: leaders,
		LimitIterations: limitIterations,
		LimitConcurrent: maxLayers,
		Hdist:           20,
	}

	mesh, err := mocknet.FullMeshLinked(nodes)
	require.NoError(t, err)

	mockMesh := mocks.NewMockmesh(gomock.NewController(t))
	if createProposal {
		for lid := types.GetEffectiveGenesis().Add(1); !lid.After(types.GetEffectiveGenesis().Add(uint32(maxLayers))); lid = lid.Add(1) {
			p := genLayerProposal(lid, []types.TransactionID{})
			mockMesh.EXPECT().Ballot(p.Ballot.ID()).Return(&p.Ballot, nil).AnyTimes()
			mockMesh.EXPECT().Proposals(lid).Return([]*types.Proposal{p}, nil).AnyTimes()
			mockMesh.EXPECT().GetAtxHeader(p.AtxID).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil).AnyTimes()
		}
	} else {
		mockMesh.EXPECT().Proposals(gomock.Any()).Return([]*types.Proposal{}, nil).AnyTimes()
	}
	mockMesh.EXPECT().GetEpochAtx(gomock.Any(), gomock.Any()).Return(&types.ActivationTxHeader{BaseTickHeight: 11, TickCount: 1}, nil).AnyTimes()
	mockMesh.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(0), nil).AnyTimes()
	mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).Return(nil, nil).AnyTimes()
	mockMesh.EXPECT().SetWeakCoin(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	for i := 0; i < nodes; i++ {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		mp2p := &p2pManipulator{nd: ps, stalledLayer: types.NewLayerID(1), err: errors.New("fake err")}

		th := &testHare{createTestHare(t, mockMesh, cfg, w.clock, mp2p, t.Name()), i}
		th.mockRoracle.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		th.mockRoracle.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(types.EmptyVrfSignature, nil).AnyTimes()
		th.mockRoracle.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, nonce types.VRFPostIndex, sig types.VrfSignature) (uint16, error) {
				return oracle(layer, round, committeeSize, id, sig, th)
			}).AnyTimes()
		th.mockRoracle.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		go func() {
			for out := range th.blockGenCh {
				validate(out.Layer, th)
			}
		}()
		w.hare = append(w.hare, th.Hare)
		e := th.Start(ctx)
		r.NoError(e)
	}
	require.NoError(t, mesh.ConnectAllButSelf())

	return w
}

func Test_HarePreRoundEmptySet(t *testing.T) {
	const nodes = 5
	const layers = 2

	var mu sync.RWMutex
	m := [layers][nodes]int{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := runNodesFor(t, ctx, nodes, 2, layers, 2, false,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, sig types.VrfSignature, hare *testHare) (uint16, error) {
			if round/4 > 1 && round != preRound {
				t.Fatalf("out of round %d limit", round)
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1

			mu.Lock()
			m[l][hare.N] = 1
			mu.Unlock()
		})

	w.LayerTicker(100 * time.Millisecond)
	time.Sleep(time.Second * 6)

	cancel()

	mu.RLock()
	defer mu.RUnlock()

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

	const nodes = 5
	const layers = 2
	m := [layers][nodes]int{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := runNodesFor(t, ctx, nodes, 2, layers, 1, false,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, sig types.VrfSignature, hare *testHare) (uint16, error) {
			if round%4 == statusRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = 1
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
	const nodes = 5
	const layers = 2
	m := [layers][nodes]int{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := runNodesFor(t, ctx, nodes, 2, layers, 1, false,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, sig types.VrfSignature, hare *testHare) (uint16, error) {
			if round%4 == proposalRound {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = 1
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
	const nodes = 6
	const layers = 2
	m := [layers][nodes]int{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := runNodesFor(t, ctx, nodes, 2, layers, 1, true,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, sig types.VrfSignature, hare *testHare) (uint16, error) {
			if round%4 == commitRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = 1
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
	const nodes = 6
	const layers = 2
	m := [layers][nodes]int{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := runNodesFor(t, ctx, nodes, 2, layers, 1, true,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, sig types.VrfSignature, hare *testHare) (uint16, error) {
			if round%4 == notifyRound && hare.N >= committee/2-1 {
				return 0, nil
			}
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = 1
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
	const nodes = 6
	const layers = 2
	m := [layers][nodes]int{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := runNodesFor(t, ctx, nodes, 2, layers, 1, true,
		func(layer types.LayerID, round uint32, committee int, id types.NodeID, sig types.VrfSignature, hare *testHare) (uint16, error) {
			return 1, nil
		},
		func(layer types.LayerID, hare *testHare) {
			l := layer.Difference(types.GetEffectiveGenesis()) - 1
			m[l][hare.N] = 1
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
