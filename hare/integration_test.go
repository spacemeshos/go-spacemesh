package hare

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	signing2 "github.com/spacemeshos/go-spacemesh/signing"
)

// Integration Tests

type HareIntegrationSuite struct {
	p2p.IntegrationTestSuite
	*HareSuite
}

func newIntegrationSuite() *HareIntegrationSuite {
	his := new(HareIntegrationSuite)
	his.HareSuite = newHareSuite()

	return his
}

// Test 1: 16 nodes sanity
type hareIntegrationThreeNodes struct {
	*HareIntegrationSuite
}

func Test_16Nodes_HareIntegrationSuite(t *testing.T) {
	t.Skip()

	const roundDuration = 2
	cfg := config.Config{N: 16, F: 8, RoundDuration: roundDuration, ExpectedLeaders: 5}
	totalNodes := 16
	his := &hareIntegrationThreeNodes{newIntegrationSuite()}
	his.BootstrappedNodeCount = totalNodes - 1
	his.BootstrapNodesCount = 1
	his.NeighborsCount = 8
	his.name = t.Name()

	i := 1
	set1 := NewSetFromValues(value1, value2)
	set2 := NewSetFromValues(value1)
	his.initialSets = make([]*Set, totalNodes)
	his.fill(set1, 0, 10)
	his.fill(set2, 11, totalNodes-1)
	his.honestSets = []*Set{set1}
	oracle := eligibility.New()
	his.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		signing := signing2.NewEdSigner()
		lg := log.NewDefault(signing.PublicKey().String())
		broker := newBroker(s, newEligibilityValidator(eligibility.New(), 10, &mockIDProvider{}, cfg.N, cfg.ExpectedLeaders, lg), NewMockStateQuerier(), (&mockSyncer{true}).IsSynced, 10, cfg.LimitIterations, util.Closer{}, lg)
		output := make(chan TerminationOutput, 1)
		oracle.Register(true, signing.PublicKey().String())
		proc := newConsensusProcess(cfg, instanceID1, his.initialSets[idx], oracle, NewMockStateQuerier(), 10, signing, types.NodeID{}, s, output, truer{}, lg)
		c, _ := broker.Register(context.TODO(), proc.ID())
		proc.SetInbox(c)
		broker.Start(context.TODO())
		his.procs = append(his.procs, proc)
		i++
	}
	suite.Run(t, his)
}

func (his *hareIntegrationThreeNodes) Test_16Nodes_AllHonest() {
	for _, proc := range his.procs {
		proc.Start(context.TODO())
	}

	his.WaitForTimedTermination(his.T(), 60*time.Second)
}

// Test 2: 20 nodes sanity

type hareIntegration20Nodes struct {
	*HareIntegrationSuite
}

func Test_20Nodes_HareIntegrationSuite(t *testing.T) {
	t.Skip()

	const roundDuration = 5
	cfg := config.Config{N: 20, F: 8, RoundDuration: roundDuration, ExpectedLeaders: 5}
	totalNodes := 20
	his := &hareIntegration20Nodes{newIntegrationSuite()}
	his.BootstrappedNodeCount = cfg.N - 3
	his.BootstrapNodesCount = 3
	his.NeighborsCount = 8
	his.name = t.Name()

	i := 1
	set1 := NewSetFromValues(value1, value2, value3, value4)
	set2 := NewSetFromValues(value1, value2, value3)
	set3 := NewSetFromValues(value2, value3, value4, value5)

	his.initialSets = make([]*Set, totalNodes)
	his.fill(set1, 0, 5)
	his.fill(set2, 6, 12)
	his.fill(set3, 13, totalNodes-1)
	his.honestSets = []*Set{set1, set2, set3}
	oracle := eligibility.New()
	his.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		signing := signing2.NewEdSigner()
		lg := log.NewDefault(signing.PublicKey().String())
		broker := newBroker(s, newEligibilityValidator(eligibility.New(), 10, &mockIDProvider{}, cfg.N, cfg.ExpectedLeaders, lg), NewMockStateQuerier(), (&mockSyncer{true}).IsSynced, 10, cfg.LimitIterations, util.Closer{}, lg)
		output := make(chan TerminationOutput, 1)
		oracle.Register(true, signing.PublicKey().String())
		proc := newConsensusProcess(cfg, instanceID1, his.initialSets[idx], oracle, NewMockStateQuerier(), 10, signing, types.NodeID{}, s, output, truer{}, log.NewDefault(signing.PublicKey().String()))
		c, _ := broker.Register(context.TODO(), proc.ID())
		proc.SetInbox(c)
		broker.Start(context.TODO())
		his.procs = append(his.procs, proc)
		i++
	}
	suite.Run(t, his)
}

func (his *hareIntegration20Nodes) Test_20Nodes_AllHonest() {
	for _, proc := range his.procs {
		proc.Start(context.TODO())
	}

	his.WaitForTimedTermination(his.T(), 120*time.Second)
}
