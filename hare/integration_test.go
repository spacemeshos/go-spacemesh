package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

func validateOutput(outputs []*Set, honestInitial []*Set, u *Set) bool {
	// check consistency
	for i := 0; i < len(outputs)-1; i++ {
		if !outputs[i].Equals(outputs[i+1]) {
			return false
		}
	}

	// build intersection
	inter := u.Intersection(honestInitial[0])
	for i := 1; i < len(honestInitial); i++ {
		inter = inter.Intersection(honestInitial[i])
	}

	// check that the output contains the intersection
	if !inter.IsSubSetOf(outputs[0]) {
		return false
	}

	// build union
	union := honestInitial[0]
	for i := 1; i < len(honestInitial); i++ {
		union = union.Union(honestInitial[i])
	}

	// check that the output has no intersection with the complement of the union of honest
	for _, v := range outputs[0].values {
		if union.Complement(u).Contains(v) {
			return false
		}
	}

	return true
}

// Integration Tests

type HareIntegrationSuite struct {
	termination Closer
	p2p.IntegrationTestSuite
	procs []*ConsensusProcess
	name  string
	// add more params you need
}

func (his *HareIntegrationSuite) waitForTermination() {
	for _, p := range his.procs {
		<-p.CloseChannel()
	}

	his.termination.Close()
}

type hareIntegrationThreeNodes struct {
	HareIntegrationSuite
}

func newIntegrationThreeNodes() *hareIntegrationThreeNodes {
	his := &hareIntegrationThreeNodes{}
	his.termination = NewCloser()

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
	sets := []*Set{set1, set1, set2}
	oracle := NewMockStaticOracle(cfg.N)
	his.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		broker := NewBroker(s)
		proc := NewConsensusProcess(cfg, generatePubKey(t), *instanceId1, *sets[idx], oracle, NewMockSigning(), s)
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
		return
	}
}
