package dht

import (
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	"testing"
)

type testNode struct {
	svc  *service.Node
	d    *MockDHT
	dscv *discovery
}

func newTestNode(simulator *service.Simulator) *testNode {
	nd := simulator.NewNode()
	d := &MockDHT{}
	disc := NewDiscoveryProtocol(nd.Node, d, nd, log.New(nd.String(), "", ""))
	return &testNode{nd, &MockDHT{}, disc}
}

func TestPing_Ping(t *testing.T) {

	sim := service.NewSimulator()
	p1 := newTestNode(sim)
	p2 := newTestNode(sim)
	p3 := sim.NewNode()

	err := p1.dscv.Ping(p2.svc.PublicKey())
	require.NoError(t, err)

	err = p2.dscv.Ping(p1.svc.PublicKey())
	require.NoError(t, err)

	err = p1.dscv.Ping(p3.PublicKey())
	require.Error(t, err)
}

func TestPing_Ping_Concurrency(t *testing.T) {
	//TODO : bigger concurrency test
	sim := service.NewSimulator()
	node1 := newTestNode(sim)
	node2 := newTestNode(sim)
	node3 := newTestNode(sim)
	node4 := newTestNode(sim)

	done := make(chan struct{})

	go func() {
		err := node1.dscv.Ping(node2.svc.PublicKey())
		require.NoError(t, err)
		done <- struct{}{}
	}()

	go func() {
		err := node1.dscv.Ping(node3.svc.PublicKey())
		require.NoError(t, err)
		done <- struct{}{}
	}()

	go func() {
		err := node1.dscv.Ping(node4.svc.PublicKey())
		require.NoError(t, err)
		done <- struct{}{}
	}()

	<-done
	<-done
	<-done
}

//todo : test verifypinger

func TestFindNodeProtocol_FindNode(t *testing.T) {

	sim := service.NewSimulator()
	n1 := newTestNode(sim)
	n2 := newTestNode(sim)

	idarr, err := n1.dscv.FindNode(n2.svc.Node.PublicKey(), node.GenerateRandomNodeData().PublicKey())

	require.NoError(t, err, "Should not return error")
	// when routing table is empty we get an empty result
	// todo: maybe this should error ?
	require.Equal(t, []node.Node{}, idarr, "Should be an empty array")
}

func TestFindNodeProtocol_FindNode2(t *testing.T) {
	randnode := node.GenerateRandomNodeData()

	sim := service.NewSimulator()

	n1 := newTestNode(sim)
	n2 := newTestNode(sim)

	n2.d.InternalLookupFunc = func(key p2pcrypto.PublicKey) []node.Node {
		return []node.Node{randnode}
	}

	n2.dscv.table = n2.d

	idarr, err := n1.dscv.FindNode(n2.svc.Node.PublicKey(), randnode.PublicKey())

	expected := []node.Node{randnode}

	require.NoError(t, err, "Should not return error")
	require.Equal(t, expected, idarr, "Should be array that contains the node")
	//
	for _, n := range node.GenerateRandomNodesData(10) {
		expected = append(expected, n)
	}
	// sort because this is how its returned
	expected = node.SortByDhtID(expected, randnode.DhtID())

	n2.d.InternalLookupFunc = func(key p2pcrypto.PublicKey) []node.Node {
		return expected
	}

	n2.dscv.table = n2.d

	idarr, err = n1.dscv.FindNode(n2.svc.Node.PublicKey(), randnode.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, expected, idarr, "Should be same array")
}

func Test_ToNodeInfo(t *testing.T) {
	many := node.GenerateRandomNodesData(100)

	for i := 0; i < len(many); i++ {
		nds := toNodeInfo(many, many[i].String())
		for j := 0; j < len(many)-1; j++ {
			if base58.Encode(nds[j].NodeId) == many[i].String() {
				t.Error("it was there")
			}
		}
	}
}
