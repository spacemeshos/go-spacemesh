package p2p

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
	"sync"
	"testing"
	"time"
)

type NodeTestInstance interface {
	server.Service
	LocalNode() *node.LocalNode // this holds the keys
}

// IntegrationTestSuite is a suite which bootstraps a network according to the given params and lets you run actions on this network.
// you must set the params before running the suite.
type IntegrationTestSuite struct {
	suite.Suite

	BootstrapNodesCount   int
	BootstrappedNodeCount int
	NeighborsCount        int

	BeforeHook func(idx int, s NodeTestInstance)
	AfterHook  func(idx int, s NodeTestInstance)

	boot  []*swarm
	Instances []*swarm
}

func (its *IntegrationTestSuite) SetupSuite() {
	boot := make([]*swarm, its.BootstrapNodesCount)
	swarm := make([]*swarm, its.BootstrappedNodeCount)

	bootcfg := config.DefaultConfig()
	bootcfg.SwarmConfig.Bootstrap = false
	bootcfg.SwarmConfig.Gossip = true
	bootcfg.SwarmConfig.RandomConnections = its.NeighborsCount

	// start boot
	for i := 0; i < len(boot); i++ {
		boot[i] = createP2pInstance(its.T(), bootcfg)
		if its.BeforeHook != nil {
			its.BeforeHook(i, boot[i])
		}
		boot[i].Start()
		if its.AfterHook != nil {
			its.AfterHook(i, boot[i])
		}
		testLog("BOOTNODE : %v", boot[i].LocalNode().String())
	}

	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = true
	cfg.SwarmConfig.Gossip = true
	cfg.SwarmConfig.RandomConnections = its.NeighborsCount
	cfg.SwarmConfig.BootstrapNodes = StringIdentifiers(boot...)

	tm := time.Now()
	testLog("Started up %d swarms", its.BootstrappedNodeCount)
	var wg sync.WaitGroup

	for i := 0; i < len(swarm); i++ {
		swarm[i] = createP2pInstance(its.T(), cfg)
		i := i
		wg.Add(1)
		go func() {
			// we add a timeout before starting to reduce the possibility or trying to connect at the same time
			// pretty rare occasion in real life (which we handle anyway), but happens a lot when running everything in 1 binary.
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			if its.BeforeHook != nil {
				its.BeforeHook(i, swarm[i])
			}
			swarm[i].Start()
			err := swarm[i].waitForBoot()
			if err != nil {
				its.Require().NoError(err)
			}
			if its.AfterHook != nil {
				its.AfterHook(i, swarm[i])
			}
			wg.Done()
		}()
	}

	testLog("Launched all processes 🎉, now waiting...")

	wg.Wait()
	testLog("Took %s for all swarms to boot up", time.Now().Sub(tm))

	// go interfaces suck with slices
	its.Instances = swarm
}

func (its *IntegrationTestSuite) TearDownSuite() {
	_, _ = its.ForAllAsync(context.Background(), func(idx int, s NodeTestInstance) error {
		s.Shutdown()
		return nil
	})
}

func createP2pInstance(t testing.TB, config config.Config) *swarm {
	port, err := node.GetUnboundedPort()
	assert.Nil(t, err)
	config.TCPPort = port
	p, err := newSwarm(context.TODO(), config, true, true)
	assert.Nil(t, err)
	assert.NotNil(t, p)
	return p
}

func (its *IntegrationTestSuite) ForAll(f func(idx int, s NodeTestInstance) error, filter []int) []error {
	e := make([]error, 0)
swarms:
	for i, s := range its.Instances {
		for _, j := range filter {
			if j == i {
				continue swarms
			}
		}
		e = append(e, f(i, s))
	}
	return e
}

func (its *IntegrationTestSuite) ForAllAsync(ctx context.Context, f func(idx int, s NodeTestInstance) error) (error, []error) {
	var mtx sync.Mutex
	errs := make([]error, len(its.Instances))

	group, ctx := errgroup.WithContext(ctx)
	for i, s := range its.Instances {
		i, s := i, s
		group.Go(func() error {
			e := f(i, s)
			mtx.Lock()
			errs[i] = e
			mtx.Unlock()
			return e
		})
	}

	return group.Wait(), errs
}

func testLog(text string, args ...interface{}) {

	fmt.Println("################################################################################################")
	fmt.Println("Test Logger :")
	fmt.Println(fmt.Sprintf(text, args...))
	fmt.Println("################################################################################################")
}

func Errors(arr []error) []int {
	var idx []int
	for i, err := range arr {
		if err != nil {
			idx = append(idx, i)
		}
	}
	return idx
}

func StringIdentifiers(boot ...*swarm) []string {
	s := make([]string, len(boot))
	for i := 0; i < len(s); i++ {
		s[i] = node.StringFromNode(boot[i].LocalNode().Node)
	}

	return s
}
