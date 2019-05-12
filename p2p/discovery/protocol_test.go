package discovery

import (
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func generateDiscNode() discNode {
	n := node.GenerateRandomNodeData()
	return discNodeFromNode(n, n.Address())
}

func generateDiscNodes(n int) []discNode {
	discs := make([]discNode, n)
	notdiscs := node.GenerateRandomNodesData(n)
	for i := 0; i < n; i++ {
		discs[i] = discNodeFromNode(notdiscs[i], notdiscs[i].Address())
	}
	return discs
}

func generateDiscNodesFakeIPs(n int) []discNode {
	discs := make([]discNode, n)
	//notdiscs := node.GenerateRandomNodesData(n)
	for i := 0; i < n; i++ {
		s := fmt.Sprintf("%d.%d.173.147:7513", i/128+60, i%128+60)
		nd := node.New(p2pcrypto.NewRandomPubkey(), s)
		discs[i] = discNodeFromNode(nd, s)
	}
	return discs
}

func GetTestLogger(name string) log.Log {
	return log.New(name, "", "")
}

type testNode struct {
	svc  *service.Node
	d    *mockAddrBook
	dscv *protocol
}

func newTestNode(simulator *service.Simulator) *testNode {
	nd := simulator.NewNode()
	d := &mockAddrBook{}
	disc := NewDiscoveryProtocol(nd.Node, d, nd, log.New(nd.String(), "", ""))
	return &testNode{nd, d, disc}
}

func TestPing_Ping(t *testing.T) {

	sim := service.NewSimulator()
	p1 := newTestNode(sim)
	p2 := newTestNode(sim)
	p3 := sim.NewNode()

	p1.d.LookupFunc = func(key p2pcrypto.PublicKey) (d discNode, e error) {
		return discNodeFromNode(p2.svc.Node, p2.svc.Node.Address()), nil
	}

	err := p1.dscv.Ping(p2.svc.PublicKey())
	require.NoError(t, err)

	p2.d.LookupFunc = func(key p2pcrypto.PublicKey) (d discNode, e error) {
		return discNodeFromNode(p1.svc.Node, p1.svc.Node.Address()), nil
	}
	err = p2.dscv.Ping(p1.svc.PublicKey())
	require.NoError(t, err)

	p1.d.LookupFunc = func(key p2pcrypto.PublicKey) (d discNode, e error) {
		return discNodeFromNode(p3.Node, p3.Node.Address()), nil
	}

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

// todo : test verifypinger

func TestFindNodeProtocol_FindNode(t *testing.T) {

	sim := service.NewSimulator()
	n1 := newTestNode(sim)
	n2 := newTestNode(sim)

	idarr, err := n1.dscv.GetAddresses(n2.svc.Node.PublicKey())

	require.NoError(t, err, "Should not return error")
	// when routing table is empty we get an empty result
	// todo: maybe this should error ?
	require.Equal(t, []discNode{}, idarr, "Should be an empty array")
}

//
func TestFindNodeProtocol_FindNode2(t *testing.T) {

	sim := service.NewSimulator()

	n1 := newTestNode(sim)
	n2 := newTestNode(sim)

	gen := generateDiscNodes(100)

	n2.d.AddressCacheResult = gen

	n2.dscv.table = n2.d

	idarr, err := n1.dscv.GetAddresses(n2.svc.Node.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, gen, idarr, "Should be array that contains the node")
	//
	gen = append(gen, generateDiscNodes(100)...)

	n2.d.AddressCacheResult = gen

	n2.dscv.table = n2.d

	idarr, err = n1.dscv.GetAddresses(n2.svc.Node.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, gen, idarr, "Should be same array")
}

func TestFindNodeProtocol_FindNode_Concurrency(t *testing.T) {

	concurrency := 100

	sim := service.NewSimulator()
	n1 := newTestNode(sim)
	gen := generateDiscNodes(100)
	n1.d.AddressCacheResult = gen
	n1.dscv.table = n1.d

	retchans := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		go func() {
			nx := newTestNode(sim)
			nx.d.LookupFunc = func(key p2pcrypto.PublicKey) (d discNode, e error) {
				return discNodeFromNode(n1.svc.Node, n1.svc.Node.Address()), nil
			}
			nx.dscv.table = nx.d
			res, err := nx.dscv.GetAddresses(n1.svc.PublicKey())
			if err != nil {
				t.Fatal("failed")
			}
			require.Equal(t, res, gen)
			retchans <- struct{}{}
		}()
	}

	for i := 0; i < concurrency; i++ {
		<-retchans // todo: this might deadlock if not working
	}
}

func Test_ToNodeInfo(t *testing.T) {
	many := generateDiscNodes(100)

	for i := 0; i < len(many); i++ {
		nds := toNodeInfo(many, many[i].String())
		for j := 0; j < len(many)-1; j++ {
			if base58.Encode(nds[j].NodeId) == many[i].String() {
				t.Error("it was there")
			}
		}
	}
}
