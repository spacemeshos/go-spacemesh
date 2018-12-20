package p2p

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
	"sync"
	"testing"
	"time"
)

func createP2pInstance(t testing.TB, config config.Config) *swarm {
	port, err := node.GetUnboundedPort()
	assert.Nil(t, err)
	config.TCPPort = port
	p, err := newSwarm(context.TODO(), config, true, true)
	assert.Nil(t, err)
	assert.NotNil(t, p)
	return p
}

type P2PSwarm struct {
	before func(s *swarm)
	after  func(s *swarm)

	boot  []*swarm
	Swarm []*swarm
}

func testLog(text string, args ...interface{}) {
	fmt.Println("################################################################################################")
	fmt.Println("Test Logger :")
	fmt.Println(fmt.Sprintf(text, args...))
	fmt.Println("################################################################################################")
}

func NewP2PSwarm(before func(s *swarm), after func(s *swarm)) *P2PSwarm {
	p2ps := new(P2PSwarm)
	p2ps.before = before
	p2ps.after = after
	return p2ps
}

func (p2ps *P2PSwarm) Start(t testing.TB, bootnodes int, networksize int, randconn int) {
	boot := make([]*swarm, bootnodes)
	swarm := make([]*swarm, networksize)

	bootcfg := config.DefaultConfig()
	bootcfg.SwarmConfig.Bootstrap = false
	bootcfg.SwarmConfig.Gossip = false

	// start boot
	for i := 0; i < len(boot); i++ {
		boot[i] = createP2pInstance(t, bootcfg)
		boot[i].Start()
		testLog("BOOTNODE : %v", boot[i].lNode.String())
	}

	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = true
	cfg.SwarmConfig.Gossip = true
	cfg.SwarmConfig.RandomConnections = randconn
	cfg.SwarmConfig.BootstrapNodes = StringIdentifiers(boot)

	tm := time.Now()
	testLog("Started up %d swarms", networksize)
	var wg sync.WaitGroup

	for i := 0; i < len(swarm); i++ {
		swarm[i] = createP2pInstance(t, cfg)
		if p2ps.before != nil {
			p2ps.before(swarm[i])
		}
		i := i
		wg.Add(1)
		go func() {
			swarm[i].Start()
			swarm[i].waitForBoot()
			wg.Done()
			if p2ps.after != nil {
				p2ps.after(swarm[i])
			}
		}()
	}

	testLog("Launched all proccess !, now Waiting")

	wg.Wait()
	testLog("Took %s to all swarms to boot up", time.Now().Sub(tm))

	p2ps.Swarm = swarm
	p2ps.boot = boot
	// todo: snapshot this
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

type filter []int

func (ps *P2PSwarm) ForAll(f func(s *swarm) error, fl filter) []error {
	e := make([]error, 0)
swarms:
	for i, s := range ps.Swarm {
		for _, j := range fl {
			if j == i {
				continue swarms
			}
		}
		e = append(e, f(s))
	}
	return e
}

func (ps *P2PSwarm) ForAllAsync(ctx context.Context, f func(s *swarm) error) (error, []error) {
	var mtx sync.Mutex
	errs := make([]error, len(ps.Swarm))

	group, ctx := errgroup.WithContext(ctx)
	for i, s := range ps.Swarm {
		i, s := i, s
		group.Go(func() error {
			e := f(s)
			mtx.Lock()
			errs[i] = e
			mtx.Unlock()
			return e
		})
	}

	return group.Wait(), errs
}

func StringIdentifiers(boot []*swarm) []string {
	s := make([]string, len(boot))
	for i := 0; i < len(s); i++ {
		s[i] = node.StringFromNode(boot[i].lNode.Node)
	}

	return s
}
