package p2p

import (
	"context"
	"fmt"
	inet "net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// NodeTestInstance is an instance of a p2p node for testing
type NodeTestInstance interface {
	service.Service
	LocalNode() node.LocalNode // this holds the keys
}

// IntegrationTestSuite is a suite which bootstraps a network according to the given params
// and lets you run actions on this network.
// You must set the params before running the suite.
type IntegrationTestSuite struct {
	suite.Suite
	logger log.Log

	BootstrapNodesCount   int
	BootstrappedNodeCount int
	NeighborsCount        int

	BeforeHook func(idx int, s NodeTestInstance)
	AfterHook  func(idx int, s NodeTestInstance)

	boot      []*Switch
	Instances []*Switch
}

// SetupSuite setups the configured nodes, bootstraps and connects them.
func (its *IntegrationTestSuite) SetupSuite() {
	its.logger = logtest.New(its.T())
	boot := make([]*Switch, its.BootstrapNodesCount)
	swarm := make([]*Switch, its.BootstrappedNodeCount)

	bootcfg := config.DefaultConfig()
	bootcfg.SwarmConfig.Bootstrap = false
	bootcfg.SwarmConfig.Gossip = true
	bootcfg.SwarmConfig.RandomConnections = its.NeighborsCount

	// start boot
	for i := 0; i < len(boot); i++ {
		boot[i] = createP2pInstance(its.T(), bootcfg, fmt.Sprintf("test-bootnode-%v", i))
		if its.BeforeHook != nil {
			its.BeforeHook(i, boot[i])
		}
		_ = boot[i].Start(context.TODO()) // ignore error ?

		if its.AfterHook != nil {
			its.AfterHook(i, boot[i])
		}
		its.logger.Info("BOOTNODE : %v", boot[i].LocalNode().PublicKey().String())
	}

	for i := 0; i < len(boot); i++ {
		for j := 0; j < len(boot); j++ {
			if j == i {
				continue
			}
			udpAddr := boot[j].udpnetwork.LocalAddr().(*inet.UDPAddr)
			tcpAddr := boot[j].network.LocalAddr().(*inet.TCPAddr)
			pk := boot[j].lNode.PublicKey()
			info := node.NewNode(pk, tcpAddr.IP, uint16(tcpAddr.Port), uint16(udpAddr.Port))
			boot[i].discover.Update(info, info)
		}
	}

	cfg := config.DefaultConfig()
	cfg.AcquirePort = false
	cfg.SwarmConfig.Bootstrap = true
	cfg.SwarmConfig.Gossip = true
	cfg.SwarmConfig.RandomConnections = its.NeighborsCount
	cfg.SwarmConfig.BootstrapNodes = StringIdentifiers(boot...)

	tm := time.Now()
	its.logger.Info("Started up %d swarms", its.BootstrappedNodeCount)
	//var wg sync.WaitGroup
	totalTimeout := time.NewTimer((time.Second * 5) * time.Duration(len(swarm)))
	finchan := make(chan error)
	for i := 0; i < len(swarm); i++ {
		swarm[i] = createP2pInstance(its.T(), cfg, fmt.Sprintf("test-node-%v", i))
		i := i
		//	wg.Add(1)
		go func() {
			// we add a timeout before starting to reduce the possibility or trying to connect at the same time
			// pretty rare occasion in real life (which we handle anyway), but happens a lot when running everything in 1 binary.
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			if its.BeforeHook != nil {
				its.BeforeHook(i, swarm[i])
			}

			err := swarm[i].Start(context.TODO()) // ignore error?
			if err != nil {
				finchan <- err
				return
				//its.T().Fatal(fmt.Sprintf("failed to start a node, %v", err))
			}
			err = swarm[i].waitForBoot()
			if err != nil {
				finchan <- err
				return
			}
			if its.AfterHook != nil {
				its.AfterHook(i, swarm[i])
			}
			finchan <- nil
		}()
	}

	its.logger.Info("Launched all processes 🎉, now waiting...")

	i := 0
loop:
	for {
		select {
		case err := <-finchan:
			i++
			if err != nil {
				its.T().Fatal(err)
			}
			if i == len(swarm) {
				break loop
			}
		case <-totalTimeout.C:
			its.T().Fatal("timeout")
		}
	}
	its.logger.Info("Took %s for all swarms to boot up", time.Since(tm))

	// go interfaces suck with slices
	its.Instances = swarm
	its.boot = boot
}

// TearDownSuite shutdowns all nodes.
func (its *IntegrationTestSuite) TearDownSuite() {
	its.logger.Info("Shutting down all nodes" + its.T().Name())
	_ = its.ForAll(func(idx int, s NodeTestInstance) error {
		s.Shutdown()
		return nil
	}, nil)
}

func createP2pInstance(t testing.TB, config config.Config, loggerName string) *Switch {
	config.TCPPort = 0
	config.AcquirePort = false
	p, err := newSwarm(context.TODO(), config, logtest.New(t).WithName(loggerName), "")
	require.NoError(t, err)
	require.NotNil(t, p)
	return p
}

// WaitForGossip waits that all nodes initialized gossip connections
func (its *IntegrationTestSuite) WaitForGossip(ctx context.Context) error {
	g, _ := errgroup.WithContext(ctx)
	for _, b := range its.boot {
		g.Go(func() error {
			return b.waitForGossip()
		})
	}
	for _, i := range its.Instances {
		g.Go(func() error {
			return i.waitForGossip()
		})
	}
	return g.Wait()
}

// ForAll executes f on all the node and returns error if it failed
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

boots:
	for i, s := range its.boot {
		for _, j := range filter {
			if j == i {
				continue boots
			}
		}
		e = append(e, f(i, s))
	}
	return e
}

// ForAllAsync executes f on all the nodes concurrently, it stops if ctx is cancelled.
func (its *IntegrationTestSuite) ForAllAsync(ctx context.Context, f func(idx int, s NodeTestInstance) error) ([]error, error) {
	var mtx sync.Mutex
	errs := make([]error, len(its.Instances))

	group, _ := errgroup.WithContext(ctx)
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

	return errs, group.Wait()
}

// StringIdentifiers turns Switch into string representation node for use as bootnodes.
func StringIdentifiers(boot ...*Switch) []string {
	s := make([]string, len(boot))
	for i := 0; i < len(s); i++ {
		pk := boot[i].LocalNode().PublicKey()
		tcp := boot[i].network.LocalAddr().(*inet.TCPAddr)
		udp := boot[i].udpnetwork.LocalAddr().(*inet.UDPAddr)
		nodeinfo := node.NewNode(pk, inet.IPv6loopback, uint16(tcp.Port), uint16(udp.Port))
		s[i] = nodeinfo.String() //node.StringFromNode(node.New(boot[i].LocalNode().Node.PublicKey(), boot[i].udpnetwork.LocalAddr().String())) )
	}
	return s
}
