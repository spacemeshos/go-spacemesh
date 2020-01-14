package discovery

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
	"time"
)

/* methods below are kept to keep tests working without big changes */

func generateDiscNode() *node.NodeInfo {
	return node.GenerateRandomNodeData()
}

func generateDiscNodes(n int) []*node.NodeInfo {
	return node.GenerateRandomNodesData(n)
}

func generateDiscNodesFakeIPs(n int) []*node.NodeInfo {
	discs := make([]*node.NodeInfo, n)
	//notdiscs := node.GenerateRandomNodesData(n)
	for i := 0; i < n; i++ {
		s := fmt.Sprintf("%d.%d.173.147:7513", i/128+60, i%128+60)
		ip, _, _ := net.SplitHostPort(s)
		pip := net.ParseIP(ip)
		port := uint16(7513)
		nd := node.NewNode(p2pcrypto.NewRandomPubkey(), pip, port, port)
		discs[i] = nd
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
	disc := NewDiscoveryProtocol(nd.NodeInfo, d, nd, log.New(nd.String(), "", ""))
	return &testNode{nd, d, disc}
}

func TestPing_Ping(t *testing.T) {

	sim := service.NewSimulator()
	p1 := newTestNode(sim)
	p2 := newTestNode(sim)
	p3 := sim.NewNode()

	p1.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.NodeInfo, e error) {
		return p2.svc.NodeInfo, nil
	}
	p1.d.GetAddressRes = &KnownAddress{na: p2.svc.NodeInfo}

	err := p1.dscv.Ping(p2.svc.PublicKey())
	require.NoError(t, err)

	p2.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.NodeInfo, e error) {
		return p1.svc.NodeInfo, nil
	}
	p2.d.GetAddressRes = &KnownAddress{na: p1.svc.NodeInfo}
	err = p2.dscv.Ping(p1.svc.PublicKey())
	require.NoError(t, err)

	p1.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.NodeInfo, e error) {
		return p3.NodeInfo, nil
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
		node1.d.GetAddressRes = &KnownAddress{na: node2.svc.NodeInfo}
		err := node1.dscv.Ping(node2.svc.PublicKey())
		require.NoError(t, err)
		done <- struct{}{}
	}()

	go func() {
		node1.d.GetAddressRes = &KnownAddress{na: node3.svc.NodeInfo}
		err := node1.dscv.Ping(node3.svc.PublicKey())
		require.NoError(t, err)
		done <- struct{}{}
	}()

	go func() {
		node1.d.GetAddressRes = &KnownAddress{na: node4.svc.NodeInfo}
		err := node1.dscv.Ping(node4.svc.PublicKey())
		require.NoError(t, err)
		done <- struct{}{}
	}()

	<-done
	<-done
	<-done
}

func Addr() net.Addr {
	return &net.IPAddr{IP: net.ParseIP("0.0.0.0"), Zone: "ipv4"}
}

func TestPing_VerifyPinger(t *testing.T) {
	sim := service.NewSimulator()
	p1 := newTestNode(sim)
	p2 := newTestNode(sim)

	// This lookup should succeed
	err := p1.dscv.verifyPinger(Addr(), p2.svc.NodeInfo)
	require.NoError(t, err)

	// todo: verify that the ping gets sent (and received?)
}

func TestFindNodeProtocol_FindNode(t *testing.T) {

	sim := service.NewSimulator()
	n1 := newTestNode(sim)
	n2 := newTestNode(sim)

	// Node2 needs to be able to look up node1 when it receives the request
	n2.d.GetAddressRes = &KnownAddress{na: n1.svc.NodeInfo}
	idarr, err := n1.dscv.GetAddresses(n2.svc.NodeInfo.PublicKey())

	require.NoError(t, err, "Should not return error")
	// when routing table is empty we get an empty result
	// todo: maybe this should error ?
	require.Equal(t, []*node.NodeInfo{}, idarr, "Should be an empty array")
}

//
func TestFindNodeProtocol_FindNode2(t *testing.T) {

	sim := service.NewSimulator()

	n1 := newTestNode(sim)
	n2 := newTestNode(sim)

	gen := generateDiscNodes(100)

	n2.d.AddressCacheResult = gen

	n2.dscv.table = n2.d

	// Node2 needs to be able to look up node1 when it receives the request
	n2.d.GetAddressRes = &KnownAddress{na: n1.svc.NodeInfo}
	idarr, err := n1.dscv.GetAddresses(n2.svc.NodeInfo.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, gen, idarr, "Should be array that contains the node")
	//
	gen = append(gen, generateDiscNodes(100)...)

	n2.d.AddressCacheResult = gen

	n2.dscv.table = n2.d

	idarr, err = n1.dscv.GetAddresses(n2.svc.NodeInfo.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, gen, idarr, "Should be same array")
}

func TestFindNodeProtocol_FindNode_Concurrency(t *testing.T) {

	concurrency := 100

	sim := service.NewSimulator()
	n1 := newTestNode(sim)
	gen := generateDiscNodes(100)
	n1.d.AddressCacheResult = gen
	// The actual node address doesn't matter as it isn't used; just pretend we recently pinged it.
	n1.d.GetAddressFunc = func() (d *KnownAddress) {
		return &KnownAddress{lastping: time.Now()}
	}
	n1.dscv.table = n1.d

	retchans := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		go func() {
			nx := newTestNode(sim)
			nx.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.NodeInfo, e error) {
				return n1.svc.NodeInfo, nil
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

// todo test nodeinfo wire serialization
//func Test_ToNodeInfo(t *testing.T) {
//	many := generateDiscNodes(100)
//
//	for i := 0; i < len(many); i++ {
//		nds, err := marshalNodeInfo(many, many[i].String())
//		require.NoError(t, err)
//		for j := 0; j < len(many)-1; j++ {
//			if base58.Encode(nds[j]) == many[i].String() {
//				t.Error("it was there")
//			}
//		}
//	}
//}
