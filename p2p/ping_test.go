package p2p

import (
	"bytes"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

func TestPingProtocol(t *testing.T) {

	const msg = "Hello Spacemash!"

	// create 2 nodes
	node1Local, _ := GenerateTestNode(t)
	node2Local, node2Remote := GenerateTestNode(t)

	// let node 1 know about node 2
	node1Local.GetSwarm().RegisterNode(node.NewRemoteNodeData(node2Remote.String(), node2Remote.TCPAddress()))

	// generate unique request id
	pingReqID := crypto.UUID()

	// specify callback for results or errors
	callback := make(chan SendPingResp)
	node1Local.GetPing().Register(callback)

	// send the ping
	t0 := time.Now()
	node1Local.GetPing().Send(msg, pingReqID, node2Remote.String())

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
				node1Local.Info("Got 2nd ping response: `%s`. Total RTT: %s", c.GetPong(), time.Now().Sub(t0).String())
				break Loop
			} else if bytes.Equal(c.GetMetadata().ReqId, pingReqID) {
				node1Local.Info("Got ping response: `%s`. Total RTT: %s", c.GetPong(), time.Now().Sub(t0))
				t0 = time.Now()

				// 2nd ping from node1 to node 2
				go node1Local.GetPing().Send(msg, ping1ReqID, node2Remote.String())
			}
		case <-time.After(time.Second * 30):
			t.Fatalf("Timeout eroror - expected callback")
		}
	}

	node1Local.Shutdown()
	node2Local.Shutdown()
}
