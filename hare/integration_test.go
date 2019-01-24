package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

// Integration Tests

type HareIntegrationSuite struct {
	termination Closer
	p2p.IntegrationTestSuite
	procs       []*ConsensusProcess
	initialSets []*Set // all initial sets
	honestSets  []*Set // initial sets of honest
	outputs     []*Set
	name        string
	// add more params you need
}

func newIntegrationSuite() *HareIntegrationSuite {
	his := &HareIntegrationSuite{}
	his.termination = NewCloser()
	his.outputs = make([]*Set, 0)

	return his
}

func (his *HareIntegrationSuite) fill(set *Set, begin int, end int) {
	for i := begin; i <= end; i++ {
		his.initialSets[i] = set
	}
}

func (his *HareIntegrationSuite) waitForTermination() {
	for _, p := range his.procs {
		<-p.CloseChannel()
		his.outputs = append(his.outputs, p.s)
	}

	his.termination.Close()
}

func (his *HareIntegrationSuite) waitForTimedTermination(timeout time.Duration) {
	timer := time.After(timeout)
	go his.waitForTermination()
	select {
	case <-timer:
		his.T().Fatal("Timeout")
		return
	case <-his.termination.CloseChannel():
		his.checkResult()
		return
	}
}

func (his *HareIntegrationSuite) checkResult() {
	t := his.T()

	// build world of values (U)
	u := his.initialSets[0]
	for i := 1; i < len(his.initialSets); i++ {
		u = u.Union(his.initialSets[i])
	}

	// check consistency
	for i := 0; i < len(his.outputs)-1; i++ {
		if !his.outputs[i].Equals(his.outputs[i+1]) {
			t.Errorf("Consistency check failed: Expected: %v Actual: %v", his.outputs[i], his.outputs[i+1])
		}
	}

	// build intersection
	inter := u.Intersection(his.honestSets[0])
	for i := 1; i < len(his.honestSets); i++ {
		inter = inter.Intersection(his.honestSets[i])
	}

	// check that the output contains the intersection
	if !inter.IsSubSetOf(his.outputs[0]) {
		t.Error("Validity 1 failed: output does not contain the intersection of honest parties")
	}

	// build union
	union := his.honestSets[0]
	for i := 1; i < len(his.honestSets); i++ {
		union = union.Union(his.honestSets[i])
	}

	// check that the output has no intersection with the complement of the union of honest
	for _, v := range his.outputs[0].values {
		if union.Complement(u).Contains(v) {
			t.Error("Validity 2 failed: unexpected value encountered: ", v)
		}
	}
}

// Test 1: Three nodes sanity
type hareIntegrationThreeNodes struct {
	*HareIntegrationSuite
}

func Test_ThreeNodes_HareIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	const roundDuration = time.Second * time.Duration(1)
	cfg := config.Config{N: 3, F: 1, SetSize: 10, RoundDuration: roundDuration}

	his := &hareIntegrationThreeNodes{newIntegrationSuite()}
	his.BootstrappedNodeCount = cfg.N - 1
	his.BootstrapNodesCount = 1
	his.NeighborsCount = 2
	his.name = t.Name()

	i := 1
	set1 := NewEmptySet(cfg.SetSize)
	set1.Add(value1)
	set1.Add(value2)
	set2 := NewEmptySet(cfg.SetSize)
	set2.Add(value1)
	his.initialSets = []*Set{set1, set1, set2}
	his.honestSets = []*Set{set1}
	oracle := NewMockStaticOracle(cfg.N)
	his.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		broker := NewBroker(s)
		output := make(chan TerminationOutput, 1)
		proc := NewConsensusProcess(cfg, s.LocalNode().PublicKey(), *instanceId1, his.initialSets[idx], oracle, &signordie{s.LocalNode().PrivateKey()}, s, output)
		broker.Register(proc)
		broker.Start()
		his.procs = append(his.procs, proc)
		i++
	}
	suite.Run(t, his)
}

func (his *hareIntegrationThreeNodes) Test_ThreeNodes_AllHonest() {
	for _, proc := range his.procs {
		proc.Start()
	}

	his.waitForTimedTermination(30 * time.Second)
}

// Test 2: 100 procs

type hareIntegration100Nodes struct {
	*HareIntegrationSuite
}

type signordie struct {
	p crypto.PrivateKey
}

func (sod signordie) Sign(m []byte) ([]byte) {
	s, err := sod.p.Sign(m)
	if err != nil {
		panic(err)
	}
	return s
}

func Test_100Nodes_HareIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	const roundDuration = time.Second * time.Duration(5)
	cfg := config.Config{N: 20, F: 10, SetSize: 10, RoundDuration: roundDuration}

	his := &hareIntegration100Nodes{newIntegrationSuite()}
	his.BootstrappedNodeCount = cfg.N - 3
	his.BootstrapNodesCount = 3
	his.NeighborsCount = 8
	his.name = t.Name()

	i := 1
	set1 := NewSetFromValues(value1, value2, value3, value4)
	set2 := NewSetFromValues(value1, value2, value3)
	set3 := NewSetFromValues(value2, value3, value4, value5)

	his.initialSets = make([]*Set, cfg.N)
	his.fill(set1, 0, 5)
	his.fill(set2, 6, 12)
	his.fill(set3, 13, cfg.N-1)
	his.honestSets = []*Set{set1, set2, set3}
	oracle := NewMockStaticOracle(cfg.N)
	his.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		log.Info("Starting instance ", idx)
		broker := NewBroker(s)
		output := make(chan TerminationOutput, 1)
		proc := NewConsensusProcess(cfg, s.LocalNode().PublicKey(), *instanceId1, his.initialSets[idx], oracle, &signordie{s.LocalNode().PrivateKey()}, s, output)
		broker.Register(proc)
		broker.Start()
		his.procs = append(his.procs, proc)
		i++
	}
	suite.Run(t, his)
}

func (his *hareIntegration100Nodes) Test_100Nodes_AllHonest() {
	for _, proc := range his.procs {
		proc.Start()
	}

	his.waitForTimedTermination(120 * time.Second)
}

