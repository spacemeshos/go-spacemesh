package p2p

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)


// this is a long test, about 60sec - will be fixed on #289
func TestGossip(t *testing.T) {
	t.Skip()
	bootnodes := []int{1}
	nodes := []int{10}
	rcon := []int{3}

	rand.Seed(time.Now().UnixNano())

	for i := 0; i < len(nodes); i++ {
		t.Run(fmt.Sprintf("Peers:%v/randconn:%v", nodes[i], rcon[i]), func(t *testing.T) {
			var wg sync.WaitGroup

			bufchan := make(chan *swarm, nodes[i])

			bnarr := []string{}

			for k := 0; k < bootnodes[i]; k++ {
				bn := p2pTestInstance(t, config.DefaultConfig())
				bn.RegisterProtocol(exampleProtocol)
				bn.lNode.Info("This is a bootnode - %v", bn.lNode.Node.String())
				bnarr = append(bnarr, node.StringFromNode(bn.lNode.Node))

			}

			cfg := config.DefaultConfig()
			cfg.DiscoveryConfig.Bootstrap = true
			cfg.DiscoveryConfig.RandomConnections = rcon[i]
			cfg.DiscoveryConfig.BootstrapNodes = bnarr

			for j := 0; j < nodes[i]; j++ {
				wg.Add(1)
				go func() {
					sw := p2pTestInstance(t, cfg)
					sw.waitForBoot()
					sw.waitForGossip()
					bufchan <- sw
					wg.Done()
				}()
			}

			wg.Wait()
			close(bufchan)

			msgchans := make(map[string]chan service.Message)

			swarms := []*swarm{}
			for s := range bufchan {
				msgchans[s.lNode.PublicKey().String()] = s.RegisterProtocol(exampleProtocol)
				swarms = append(swarms, s)
			}

			var got uint32 = 0

			first := swarms[0]

			for _, s := range swarms[1:] {
				wg.Add(1)
				ch := msgchans[s.lNode.PublicKey().String()]
				go func(s *swarm, ch chan service.Message) {
					_ = <-ch
					atomic.AddUint32(&got, 1)
					wg.Done()
				}(s, ch)
			}

			first.Broadcast(exampleProtocol, []byte("STAM"))
			wg.Wait()

			assert.True(t, got >= uint32(nodes[i]-1))
			time.Sleep(3 * time.Second)
		})
	}
}
