package discovery

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	"testing"
)

/* methods below are kept to keep tests working without big changes */

func generateDiscNode() *node.Info {
	return node.GenerateRandomNodeData()
}

func generateDiscNodes(n int) []*node.Info {
	return node.GenerateRandomNodesData(n)
}

func GetTestLogger(name string) log.Log {
	return log.NewDefault(name)
}

type testNode struct {
	svc  *service.Node
	d    *mockAddrBook
	dscv *protocol
}

func newTestNode(simulator *service.Simulator) *testNode {
	nd := simulator.NewNode()
	d := &mockAddrBook{}
	disc := newProtocol(context.TODO(), nd.Info.PublicKey(), d, nd, log.NewDefault(nd.String()))
	return &testNode{nd, d, disc}
}

func TestPing_Ping(t *testing.T) {

	sim := service.NewSimulator()
	p1 := newTestNode(sim)
	p2 := newTestNode(sim)
	p3 := sim.NewNode()

	p1.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
		return p2.svc.Info, nil
	}

	err := p1.dscv.Ping(context.TODO(), p2.svc.PublicKey())
	require.NoError(t, err)

	p2.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
		return p1.svc.Info, nil
	}
	err = p2.dscv.Ping(context.TODO(), p1.svc.PublicKey())
	require.NoError(t, err)

	p1.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
		return p3.Info, nil
	}

	err = p1.dscv.Ping(context.TODO(), p3.PublicKey())
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
		err := node1.dscv.Ping(context.TODO(), node2.svc.PublicKey())
		require.NoError(t, err)
		done <- struct{}{}
	}()

	go func() {
		err := node1.dscv.Ping(context.TODO(), node3.svc.PublicKey())
		require.NoError(t, err)
		done <- struct{}{}
	}()

	go func() {
		err := node1.dscv.Ping(context.TODO(), node4.svc.PublicKey())
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

	idarr, err := n1.dscv.GetAddresses(context.TODO(), n2.svc.Info.PublicKey())

	require.NoError(t, err, "Should not return error")
	// when routing table is empty we get an empty result
	// todo: maybe this should error ?
	require.Equal(t, []*node.Info{}, idarr, "Should be an empty array")
}

//
func TestFindNodeProtocol_FindNode2(t *testing.T) {

	sim := service.NewSimulator()

	n1 := newTestNode(sim)
	n2 := newTestNode(sim)

	gen := generateDiscNodes(100)

	n2.d.AddressCacheFunc = func() []*node.Info {
		return gen
	}

	n2.dscv.table = n2.d

	idarr, err := n1.dscv.GetAddresses(context.TODO(), n2.svc.Info.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, gen, idarr, "Should be array that contains the node")
	//
	gen = append(gen, generateDiscNodes(100)...)

	n2.d.AddressCacheFunc = func() []*node.Info {
		return gen
	}

	n2.dscv.table = n2.d

	idarr, err = n1.dscv.GetAddresses(context.TODO(), n2.svc.Info.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, gen, idarr, "Should be same array")
}

func TestFindNodeProtocol_FindNode_Concurrency(t *testing.T) {

	concurrency := 100

	sim := service.NewSimulator()
	n1 := newTestNode(sim)
	gen := generateDiscNodes(100)
	n1.d.AddressCacheFunc = func() []*node.Info {
		return gen
	}
	n1.dscv.table = n1.d

	retchans := make(chan []*node.Info)

	for i := 0; i < concurrency; i++ {
		go func() {
			nx := newTestNode(sim)
			nx.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
				return n1.svc.Info, nil
			}
			nx.dscv.table = nx.d
			res, err := nx.dscv.GetAddresses(context.TODO(), n1.svc.PublicKey())
			if err != nil {
				t.Log(err)
				retchans <- nil
			}
			retchans <- res
		}()
	}

	for i := 0; i < concurrency; i++ {
		res := <-retchans // todo: this might deadlock if not working
		require.Equal(t, gen, res)
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
