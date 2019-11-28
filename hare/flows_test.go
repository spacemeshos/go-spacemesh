package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/amcl"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type HareWrapper struct {
	totalCP     int
	termination Closer
	lCh         []chan types.LayerID
	hare        []*Hare
	initialSets []*Set // all initial sets
	outputs     map[instanceId][]*Set
	name        string
}

func newHareWrapper(totalCp int) *HareWrapper {
	hs := new(HareWrapper)
	hs.lCh = make([]chan types.LayerID, 0)
	hs.totalCP = totalCp
	hs.termination = NewCloser()
	hs.outputs = make(map[instanceId][]*Set, 0)

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
			for i := types.LayerID(1); i <= types.LayerID(his.totalCP); i++ {
				blks, _ := p.GetResult(i)
				if len(blks) > 0 {
					count++
				}
			}
		}

		//log.Info("count is %v", count)
		if count == his.totalCP*len(his.hare) {
			break
		}

		time.Sleep(300 * time.Millisecond)
	}

	log.Info("Terminating. Validating outputs")

	for _, p := range his.hare {
		for i := types.LayerID(1); i <= types.LayerID(his.totalCP); i++ {
			s := NewEmptySet(10)
			blks, _ := p.GetResult(i)
			for _, b := range blks {
				s.Add(b)
			}
			his.outputs[instanceId(i)] = append(his.outputs[instanceId(i)], s)
		}
	}

	his.termination.Close()
}

func (his *HareWrapper) WaitForTimedTermination(t *testing.T, timeout time.Duration) {
	timer := time.After(timeout)
	go his.waitForTermination()
	select {
	case <-timer:
		t.Fatal("Timeout")
		return
	case <-his.termination.CloseChannel():
		for i := 1; i <= his.totalCP; i++ {
			his.checkResult(t, instanceId(i))
		}
		return
	}
}

func (his *HareWrapper) checkResult(t *testing.T, id instanceId) {
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
	stalledLayer instanceId
	err          error
}

func (m *p2pManipulator) RegisterGossipProtocol(protocol string) chan service.GossipMessage {
	ch := m.nd.RegisterGossipProtocol(protocol)
	wch := make(chan service.GossipMessage)

	go func() {
		for {
			x := <-ch
			wch <- x
		}
	}()

	return wch
}

func (m *p2pManipulator) Broadcast(protocol string, payload []byte) error {
	msg, e := MessageFromBuffer(payload)
	if msg.InnerMsg.InstanceId == m.stalledLayer && msg.InnerMsg.K < 8 && msg.InnerMsg.K != -1 {
		log.Warning("Not broadcasting in manipulator")
		return m.err
	}

	e = m.nd.Broadcast(protocol, payload)
	return e
}

type trueOracle struct {
}

func (trueOracle) Register(isHonest bool, id string) {
}

func (trueOracle) Unregister(isHonest bool, id string) {
}

func (trueOracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	return true, nil
}

func (trueOracle) Proof(layer types.LayerID, round int32) ([]byte, error) {
	x := make([]byte, 100)
	return x, nil
}

func (trueOracle) IsIdentityActiveOnConsensusView(edId string, layer types.LayerID) (bool, error) {
	return true, nil
}

// Test - runs a single CP for more than one iteration
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
		p2pm := &p2pManipulator{nd: s, stalledLayer: 1, err: errors.New("fake err")}
		proc := createConsensusProcess(true, cfg, oracle, p2pm, test.initialSets[i], 1, t.Name())
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(totalNodes, creationFunc)
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func validateBlock([]types.BlockID) bool {
	return true
}

func isSynced() bool {
	return true
}

type mockIdentityP struct {
	nid types.NodeId
}

func (m *mockIdentityP) GetIdentity(edId string) (types.NodeId, error) {
	return m.nid, nil
}

func buildSet() []types.BlockID {
	s := make([]types.BlockID, 200, 200)

	for i := uint64(0); i < 200; i++ {
		s = append(s, types.NewExistingBlock(1, util.Uint64ToBytes(i)).Id())
	}

	return s
}

type mockBlockProvider struct {
}

func (mbp *mockBlockProvider) GetUnverifiedLayerBlocks(layerId types.LayerID) ([]types.BlockID, error) {
	return buildSet(), nil
}

func createMaatuf(tcfg config.Config, rng *amcl.RAND, layersCh chan types.LayerID, p2p NetworkService, rolacle Rolacle, name string) *Hare {
	ed := signing.NewEdSigner()
	pub := ed.PublicKey()
	_, vrfPub := BLS381.GenKeyPair(rng)
	//vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	nodeID := types.NodeId{Key: pub.String(), VRFPublicKey: vrfPub}
	hare := New(tcfg, p2p, ed, nodeID, validateBlock, isSynced, &mockBlockProvider{}, rolacle, 10, &mockIdentityP{nid: nodeID},
		&MockStateQuerier{true, nil}, layersCh, log.NewDefault(name+"_"+ed.PublicKey().ShortString()))

	return hare
}

// Test - run multiple CPs simultaneously
func Test_multipleCPs(t *testing.T) {
	r := require.New(t)
	totalCp := 3
	test := newHareWrapper(totalCp)
	totalNodes := 20
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 3, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}
	rng := BLS381.DefaultSeed()
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	oracle := &trueOracle{}
	for i := 0; i < totalNodes; i++ {
		s := sim.NewNode()
		//p2pm := &p2pManipulator{nd: s, err: errors.New("fake err")}
		test.lCh = append(test.lCh, make(chan types.LayerID, 1))
		h := createMaatuf(cfg, rng, test.lCh[i], s, oracle, t.Name())
		test.hare = append(test.hare, h)
		e := h.Start()
		r.NoError(e)
	}

	go func() {
		for j := types.LayerID(1); j <= types.LayerID(totalCp); j++ {
			log.Info("sending for layer %v", j)
			for i := 0; i < len(test.lCh); i++ {
				log.Info("sending for instance %v", i)
				test.lCh[i] <- j
			}
			time.Sleep(150 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 30*time.Second)
}

// Test - run multiple CPs where one of them runs more than one iteration
func Test_multipleCPsAndIterations(t *testing.T) {
	r := require.New(t)
	totalCp := 4
	test := newHareWrapper(totalCp)
	totalNodes := 20
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 3, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100}
	rng := BLS381.DefaultSeed()
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	oracle := &trueOracle{}
	for i := 0; i < totalNodes; i++ {
		s := sim.NewNode()
		mp2p := &p2pManipulator{nd: s, stalledLayer: 1, err: errors.New("fake err")}
		test.lCh = append(test.lCh, make(chan types.LayerID, 1))
		h := createMaatuf(cfg, rng, test.lCh[i], mp2p, oracle, t.Name())
		test.hare = append(test.hare, h)
		e := h.Start()
		r.NoError(e)
	}

	go func() {
		for j := types.LayerID(1); j <= types.LayerID(totalCp); j++ {
			log.Info("sending for layer %v", j)
			for i := 0; i < len(test.lCh); i++ {
				log.Info("sending for instance %v", i)
				test.lCh[i] <- j
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()

	test.WaitForTimedTermination(t, 50*time.Second)
}
