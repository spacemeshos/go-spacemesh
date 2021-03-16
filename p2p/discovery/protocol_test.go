package discovery

import (
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
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
	disc := newProtocol(nd.Info.PublicKey(), d, nd, log.NewDefault(nd.String()))
	return &testNode{nd, d, disc}
}

//document this code and test
func TestPing_Ping(t *testing.T) {

	sim := service.NewSimulator()
	p1 := newTestNode(sim)
	p2 := newTestNode(sim)
	p3 := sim.NewNode()

	p1.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
		return p2.svc.Info, nil
	}

	err := p1.dscv.Ping(p2.svc.PublicKey())
	require.NoError(t, err)

	p2.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
		return p1.svc.Info, nil
	}
	err = p2.dscv.Ping(p1.svc.PublicKey())
	require.NoError(t, err)

	p1.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
		return p3.Info, nil
	}

	err = p1.dscv.Ping(p3.PublicKey())
	require.Error(t, err)
}

//check that the error is empty as well
func TestDiscoveryPing(t *testing.T) {
	sim := service.NewSimulator()
	n1 := newTestNode(sim)
	n2 := newTestNode(sim)

	//assume that n1 has pinged n2, therefore n1 is in the addrBook of n2
	n2.d.LookupKnownFunc = func(pubkey p2pcrypto.PublicKey) (*KnownAddress, error) {
		return &KnownAddress{
			na:       n1.svc.Info,
			lastping: time.Unix(0, 0),
		}, nil
	}
	justnow := time.Now()
	//explain what happens inside GetAddresses => synchronous, ping should have happened
	n1.dscv.GetAddresses(n2.svc.PublicKey())

	// since n2 has never pinged n1, the GetAddresses method should result in a ping from n2 to n1
	// this means that n1.dscv.lastDiscoveryPing should be after justnow
	if !n1.dscv.lastDiscoveryPing.After(justnow) {
		t.Errorf("test case 1 : unpinged address should receive a ping during the GetAddresses request")
	}

	//assume that n1 has pinged n2, and we have verified the ping after the threshold (1 hour ago)
	n2.d.LookupKnownFunc = func(pubkey p2pcrypto.PublicKey) (*KnownAddress, error) {
		return &KnownAddress{
			na:       n1.svc.Info,
			lastping: time.Now().Add(-1 * time.Hour),
		}, nil
	}

	justnow = time.Now()

	n1.dscv.GetAddresses(n2.svc.PublicKey())
	if n1.dscv.lastDiscoveryPing.After(justnow) {
		t.Errorf("test case 2 : recently pinged address should not receive a ping during the GetAddresses request")
	}

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

	//we have to assume that n1 has pinged n2, and so it is added into its addrBook
	n2.d.LookupKnownFunc = func(pubkey p2pcrypto.PublicKey) (*KnownAddress, error) {
		return &KnownAddress{
			na:       n1.svc.Info,
			lastping: time.Unix(0, 0),
		}, nil
	}

	idarr, err := n1.dscv.GetAddresses(n2.svc.Info.PublicKey())

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
	//we have to assume that n1 has pinged n2, and so it is added into its addrBook
	n2.d.LookupKnownFunc = func(pubkey p2pcrypto.PublicKey) (*KnownAddress, error) {
		return &KnownAddress{
			na:       n1.svc.Info,
			lastping: time.Unix(0, 0),
		}, nil
	}

	idarr, err := n1.dscv.GetAddresses(n2.svc.Info.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, gen, idarr, "Should be array that contains the node")
	//
	gen = append(gen, generateDiscNodes(100)...)

	n2.d.AddressCacheFunc = func() []*node.Info {
		return gen
	}

	n2.dscv.table = n2.d

	idarr, err = n1.dscv.GetAddresses(n2.svc.Info.PublicKey())

	require.NoError(t, err, "Should not return error")
	require.Equal(t, gen, idarr, "Should be same array")
}

func TestFindNodeProtocol_FindNode_Concurrency(t *testing.T) {

	concurrency := 100

	sim := service.NewSimulator()
	n1 := newTestNode(sim)
	gen := generateDiscNodes(2)
	testNodes := make([]*testNode, concurrency)
	for i := 0; i < concurrency; i++ {
		//create all the nodes
		testNodes[i] = newTestNode(sim)
		testNodes[i].d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
			return n1.svc.Info, nil
		}
		testNodes[i].dscv.table = testNodes[i].d
	}
	n1.d.AddressCacheFunc = func() []*node.Info {
		return gen
	}
	n1.d.LookupKnownFunc = func(pubkey p2pcrypto.PublicKey) (*KnownAddress, error) {
		for i := 0; i < concurrency; i++ {
			if testNodes[i].svc.PublicKey() == pubkey {
				return &KnownAddress{
					na: testNodes[i].svc.Info,
				}, nil
			}
		}
		return nil, nil
	}
	n1.dscv.table = n1.d

	retchans := make(chan []*node.Info)

	for i := 0; i < concurrency; i++ {
		go func(index int) {
			// nx := newTestNode(sim)
			// nx.d.LookupFunc = func(key p2pcrypto.PublicKey) (d *node.Info, e error) {
			// 	return n1.svc.Info, nil
			// }
			// n1.d.LookupKnownFunc = func(pubkey p2pcrypto.PublicKey) (*KnownAddress, error) {
			// 	return &KnownAddress{
			// 		na: nx.svc.Info,
			// 	}, nil
			// }
			res, err := testNodes[index].dscv.GetAddresses(n1.svc.PublicKey())
			if err != nil {
				t.Log(err)
				retchans <- nil
				return
			}
			retchans <- res
		}(i)
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
