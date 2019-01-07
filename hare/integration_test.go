package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

// Integration

type HareIntegrationSuite struct {
	p2p.IntegrationTestSuite
	procs []*ConsensusProcess
	name  string
	// add more params you need
}

type hareIntegrationThreeNodes struct {
	HareIntegrationSuite
}

func Test_ThreeNodes_HareIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	const roundDuration = time.Second * time.Duration(1)
	cfg := config.Config{N: 3, F: 0, SetSize: 10, RoundDuration: roundDuration}

	his := &hareIntegrationThreeNodes{}
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

func (hit *hareIntegrationThreeNodes) Test_ThreeNodes_AllHonest() {
	t := hit.T()
	for _, proc := range hit.procs {
		proc.Start()
	}

	timeout := time.After(30 * time.Second)
	select {
	case  <-timeout:
		t.Error("Timeout")
		return
	case <-hit.procs[0].CloseChannel():
		return
	}
}

