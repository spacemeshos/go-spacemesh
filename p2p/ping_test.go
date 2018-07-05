package p2p

import (
	"bytes"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/stretchr/testify/assert"
)

func TestPingProtocol(t *testing.T) {

	const msg = "Hello Spacemash!"

	cfg := nodeconfig.DefaultConfig()
	// create 2 nodes
	node1Local := p2pTestInstance(t, cfg)

	node2Local := p2pTestInstance(t, cfg)

	// let node 1 know about node 2
	node1Local.RegisterNode(node.New(node2Local.LocalNode().PublicKey(), node2Local.LocalNode().Address()))

	// generate unique request id
	pingReqID := crypto.UUID()

	// specify callback for results or errors
	callback := make(chan SendPingResp)
	ping := NewPingProtocol(node1Local)
	ping.Register(callback)

	NewPingProtocol(node2Local)
	// send the ping
	t0 := time.Now()
	ping.Send(msg, pingReqID, node2Local.LocalNode().String())

	// Under the hood node 1 establishes an encrypted authenticated session with node 2 and sends the ping request
	// over that session once it is established. Node 1 registers an app-level callback to get the ping response from node 2.
	// The response includes the request id so node1 can match it with one or more tracked requests it has sent.

	// req1 id
	ping1ReqID := crypto.UUID()

Loop:
	for {
		select {
		case c := <-callback:
			assert.Nil(t, c.err, "expected no err in response")
			if bytes.Equal(c.GetMetadata().ReqId, ping1ReqID) { //2nd ping
				node1Local.LocalNode().Debug("Got 2nd ping response: `%s`. Total RTT: %s", c.GetPong(), time.Now().Sub(t0).String())
				break Loop
			} else if bytes.Equal(c.GetMetadata().ReqId, pingReqID) {
				node1Local.LocalNode().Debug("Got ping response: `%s`. Total RTT: %s", c.GetPong(), time.Now().Sub(t0))
				t0 = time.Now()

				// 2nd ping from node1 to node 2
				go ping.Send(msg, ping1ReqID, node2Local.LocalNode().String())
			}
		case <-time.After(time.Second * 30):
			t.Fatalf("Timeout eroror - expected callback")
		}
	}

	node1Local.Shutdown()
	node2Local.Shutdown()
}
