package p2p

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"sync/atomic"
	"testing"
	"time"
)

func TestSwarm_GossipRoundTrip(t *testing.T) {
	type sp struct {
		s      *swarm
		protoC chan service.Message
	}

	numPeers, connections := 10, 5

	nodes := make([]*swarm, numPeers)
	chans := make([]chan service.Message, numPeers)
	nchan := make(chan *sp, numPeers)

	cfg := config.DefaultConfig()
	cfg.SwarmConfig.RandomConnections = connections
	cfg.SwarmConfig.Bootstrap = false
	bn := p2pTestInstance(t, cfg)
	// TODO: write protocol matching. so we won't crash connections because bad protocol messages.
	// if we're after protocol matching then we can crash the connection since its probably malicious
	bn.RegisterProtocol("gossip") // or else it will crash connections

	err := bn.Start()
	assert.NoError(t, err, "Bootnode didnt work")

	cfg2 := config.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = connections
	cfg2.SwarmConfig.Bootstrap = true
	cfg2.SwarmConfig.BootstrapNodes = []string{node.StringFromNode(bn.lNode.Node)}
	for i := 0; i < numPeers; i++ {
		go func() {
			nod := p2pTestInstance(t, cfg2)
			if nod == nil {
				t.Error("ITS NIL WTF")
			}
			nodchan := nod.RegisterProtocol("gossip") // this is example
			err := nod.Start()
			assert.NoError(t, err, err)
			nchan <- &sp{nod, nodchan}
		}()
	}

	i := 0
	for n := range nchan {
		nodes[i] = n.s
		chans[i] = n.protoC

		i++
		if i >= numPeers {
			close(nchan)
		}
	}

	fmt.Println(" ################################################ ALL PEERS BOOTSTRAPPED ################################################")

	msg := []byte("gossip")
	fmt.Println(" ################################################ GOSSIPING ################################################")
	b := time.Now()
	err = nodes[0].Broadcast("gossip", msg)

	fmt.Printf("%v GOSSIPED TO %v, err=%v\r\n", nodes[0].lNode.String(), err)

	var got int32 = 0
	didntget := make([]*swarm, 0)
	//var wg sync.WaitGroup

	for c := range chans {
		var resp service.Message
		timeout := time.NewTimer(time.Second * 10)
		select {
		case resp = <-chans[c]:
		case <-timeout.C:
			didntget = append(didntget, nodes[c])
			continue
		}

		if bytes.Equal(resp.Data(), msg) {
			nodes[c].lNode.Info("GOT THE gossip MESSAge ", atomic.AddInt32(&got, 1))
		}
	}
	//wg.Wait()
	bn.LocalNode().Info("THIS IS GOT ", got)
	assert.Equal(t, got, int32(numPeers))
	bn.lNode.Info("message spread to %v peers in %v", got, time.Since(b))
	didnt := ""
	for i := 0; i < len(didntget); i++ {
		didnt += fmt.Sprintf("%v\r\n", didntget[i].lNode.String())
	}
	bn.lNode.Info("didnt get : %v", didnt)
	time.Sleep(time.Millisecond * 1000) // to see the log
}
