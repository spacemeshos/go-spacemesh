package p2p

import (
	"bytes"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
)

func TestFindNodeProtocolCore(t *testing.T) {

	// let there be 3 nodes - node1, node2 and node 3
	node1Local, node1Remote := GenerateTestNode(t)
	node2Local, _ := GenerateTestNode(t)
	node3Local, node3Remote := GenerateTestNode(t)

	// node 1 know about node 2 and node 3
	d := make(chan error, 2)
	node1Local.GetSwarm().ConnectTo(node2Local.GetRemoteNodeData(), d)
	node1Local.GetSwarm().ConnectTo(node3Local.GetRemoteNodeData(), d)

	<-d
	<-d

	// node 2 doesn't know about node 3 and asks node 1 to find it
	reqID := crypto.UUID()
	callback := make(chan FindNodeResp)
	node2Local.GetSwarm().getFindNodeProtocol().FindNode(reqID, node1Remote.String(), node3Remote.String(), callback)

Loop:
	for {
		select {
		case c := <-callback:
			assert.NotNil(t, c, "expected response slice")
			assert.Nil(t, c.err, "Expected no error")

			if !bytes.Equal(c.GetMetadata().ReqId, reqID) {
				t.Fatalf("Didn't expect callback on another reqID")
			}
			assert.True(t, len(c.NodeInfos) >= 1, "expected at least 1 node")

			for _, d := range c.NodeInfos {
				if bytes.Equal(d.NodeId, node3Remote.ID()) {
					log.Debug("Found node 3 :-)")
					break Loop
				}
			}
			t.Fatalf("didn't find node 3")

		case <-time.After(time.Second * 10):
			t.Fatalf("Test timed out")
		}
	}

	node1Local.Shutdown()
	node2Local.Shutdown()
	node3Local.Shutdown()
}

func TestFindNodeProtocolEmptyRes(t *testing.T) {

	// let there be 3 nodes - node1, node2 and node3
	node1Local, node1Remote := GenerateTestNode(t)
	node2Local, _ := GenerateTestNode(t)
	node3Local, node3Remote := GenerateTestNode(t)

	t.Logf("Node 1: %s %s", node1Remote.String(), node1Remote.TCPAddress())
	t.Logf("Node 2: %s %s", node2Local.String(), node2Local.TCPAddress())
	t.Logf("Node 3: %s %s", node3Remote.String(), node3Remote.TCPAddress())

	// node 2 knows about node 1. Nobody knows about node 3
	node2Local.GetSwarm().RegisterNode(node1Local.GetRemoteNodeData())

	// node 2 doesn't know about node 3 and asks node 1 to find it
	reqID := crypto.UUID()
	callback := make(chan FindNodeResp)
	node2Local.GetSwarm().getFindNodeProtocol().FindNode(reqID, node1Remote.String(), node3Remote.String(), callback)

Loop:
	for {
		select {
		case c := <-callback:
			assert.NotNil(t, c, "expected non nil response slice w 0 or more items")
			assert.Nil(t, c.err, "Expected no error")

			if !bytes.Equal(c.GetMetadata().ReqId, reqID) {
				t.Fatalf("Didn't expect callback on another reqID")
			}

			for _, d := range c.NodeInfos {
				if bytes.Equal(d.NodeId, node3Remote.ID()) {
					t.Fatalf("didn't expect result to include node 3")
				}
			}
			break Loop

		case <-time.After(time.Second * 10):
			t.Fatalf("Test timed out")
		}

	}

	node1Local.Shutdown()
	node2Local.Shutdown()
	node3Local.Shutdown()
}
