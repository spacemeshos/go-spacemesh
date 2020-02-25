package p2p

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
	"net"
	"sync"
	"testing"
	"time"
)

type NodeTestInstance interface {
	Service
	LocalNode() node.LocalNode // this holds the keys
}

// IntegrationTestSuite is a suite which bootstraps a network according to the given params
// and lets you run actions on this network.
// you must set the params before running the suite.
type IntegrationTestSuite struct {
	suite.Suite

	BootstrapNodesCount   int
	BootstrappedNodeCount int
	NeighborsCount        int

	BeforeHook func(idx int, s NodeTestInstance)
	AfterHook  func(idx int, s NodeTestInstance)

	boot      []*swarm
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
		_ = boot[i].Start() // ignore error ?

		if its.AfterHook != nil {
			its.AfterHook(i, boot[i])
		}
		testLog("BOOTNODE : %v", boot[i].LocalNode().PublicKey().String())
	}

	for i := 0; i < len(boot); i++ {
		for j := 0; j < len(boot); j++ {
			if j == i {
				continue
			}
			udpAddr := boot[j].udpnetwork.LocalAddr().(*net.UDPAddr)
			tcpAddr := boot[j].network.LocalAddr().(*net.TCPAddr)
			pk := boot[j].lNode.PublicKey()
			info := node.NewNode(pk, tcpAddr.IP, uint16(tcpAddr.Port), uint16(udpAddr.Port))
			boot[i].discover.Update(info, info)
		}
	}

	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = true
	cfg.SwarmConfig.Gossip = true
	cfg.SwarmConfig.RandomConnections = its.NeighborsCount
	cfg.SwarmConfig.BootstrapNodes = StringIdentifiers(boot...)

	tm := time.Now()
	testLog("Started up %d swarms", its.BootstrappedNodeCount)
	//var wg sync.WaitGroup
	totalTimeout := time.NewTimer((time.Second * 5) * time.Duration(len(swarm)))
	finchan := make(chan error)
	for i := 0; i < len(swarm); i++ {
		swarm[i] = createP2pInstance(its.T(), cfg)
		i := i
		//	wg.Add(1)
		go func() {
			// we add a timeout before starting to reduce the possibility or trying to connect at the same time
			// pretty rare occasion in real life (which we handle anyway), but happens a lot when running everything in 1 binary.
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			if its.BeforeHook != nil {
				its.BeforeHook(i, swarm[i])
			}

			err := swarm[i].Start() // ignore error?
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

	testLog("Launched all processes 🎉, now waiting...")

	i := 0
lop:
	for {
		select {
		case err := <-finchan:
			i++
			if err != nil {
				its.T().Fatal(err)
			}
			if i == len(swarm) {
				break lop
			}
		case <-totalTimeout.C:
			its.T().Fatal("timeout")
		}
	}
	testLog("Took %s for all swarms to boot up", time.Now().Sub(tm))

	// go interfaces suck with slices
	its.Instances = swarm
	its.boot = boot
}

func (its *IntegrationTestSuite) TearDownSuite() {
	testLog("Shutting down all nodes" + its.T().Name())
	_ = its.ForAll(func(idx int, s NodeTestInstance) error {
		s.Shutdown()
		return nil
	}, nil)
}

func createP2pInstance(t testing.TB, config config.Config) *swarm {
	config.TCPPort = 0
	p, err := newSwarm(context.TODO(), config, log.NewDefault("test instance"), "")
	require.NoError(t, err)
	require.NotNil(t, p)
	return p
}

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
		pk := boot[i].LocalNode().PublicKey()
		tcp := boot[i].network.LocalAddr().(*net.TCPAddr)
		udp := boot[i].udpnetwork.LocalAddr().(*net.UDPAddr)
		nodeinfo := node.NewNode(pk, net.IPv6loopback, uint16(tcp.Port), uint16(udp.Port))
		s[i] = nodeinfo.String() //node.StringFromNode(node.New(boot[i].LocalNode().Node.PublicKey(), boot[i].udpnetwork.LocalAddr().String())) )
	}
	return s
}
