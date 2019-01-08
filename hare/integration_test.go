package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/config"
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

func (his *HareIntegrationSuite) waitForTermination() {
	for _, p := range his.procs {
		<-p.CloseChannel()
		his.outputs = append(his.outputs, p.s)
	}

	his.termination.Close()
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
			t.Error("Consistency check failed")
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

type hareIntegrationThreeNodes struct {
	HareIntegrationSuite
}

func newIntegrationThreeNodes() *hareIntegrationThreeNodes {
	his := &hareIntegrationThreeNodes{}
	his.termination = NewCloser()
	his.initialSets = make([]*Set, 0)
	his.honestSets = make([]*Set, 0)
	his.outputs = make([]*Set, 0)

	return his
}

func Test_ThreeNodes_HareIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	const roundDuration = time.Second * time.Duration(1)
	cfg := config.Config{N: 3, F: 0, SetSize: 10, RoundDuration: roundDuration}

	his := newIntegrationThreeNodes()
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
		proc := NewConsensusProcess(cfg, generatePubKey(t), *instanceId1, *his.initialSets[idx], oracle, NewMockSigning(), s)
		broker.Register(proc)
		broker.Start()
		his.procs = append(his.procs, proc)
		i++
	}
	suite.Run(t, his)
}

func (his *hareIntegrationThreeNodes) Test_ThreeNodes_AllHonest() {
	t := his.T()
	for _, proc := range his.procs {
		proc.Start()
	}

	timeout := time.After(30 * time.Second)
	go his.waitForTermination()
	select {
	case <-timeout:
		t.Error("Timeout")
		return
	case <-his.termination.CloseChannel():
		his.checkResult()
		return
	}
}
