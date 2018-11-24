package gossip

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type mockCpool struct {
	f func(address string, pk crypto.PublicKey) (net.Connection, error)
}

func (mcp *mockCpool) GetConnection(address string, pk crypto.PublicKey) (net.Connection, error) {
	if mcp.f != nil {
		return mcp.f(address, pk)
	}
	c := net.NewConnectionMock(pk)
	c.SetSession(net.SessionMock{})
	return c, nil
}

type mockSampler struct {
	f func(count int) []node.Node
}

func (mcs *mockSampler) SelectPeers(count int) []node.Node {
	if mcs.f != nil {
		return mcs.f(count)
	}
	return node.GenerateRandomNodesData(count)
}

//todo : more unit tests

func TestNeighborhood_Broadcast(t *testing.T) {
	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, new(mockSampler), new(mockCpool), log.New("tesT", "", ""))
	err := n.Broadcast([]byte("msg"))
	assert.Error(t, err)
}

func TestNeighborhood_AddIncomingPeer(t *testing.T) {
	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, new(mockSampler), new(mockCpool), log.New("tesT", "", ""))
	n.Start()
	rnd := node.GenerateRandomNodeData()
	n.AddIncomingPeer(rnd, nil)

	n.inpeersMutex.RLock()
	p, ok := n.inpeers[rnd.PublicKey().String()]
	n.inpeersMutex.RUnlock()

	assert.True(t, ok)
	assert.NotNil(t, p)
}

func TestNeighborhood_Broadcast2(t *testing.T) {
	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, new(mockSampler), new(mockCpool), log.New("tesT", "", ""))
	n.Start()
	<-n.Initial()
	err := n.Broadcast([]byte("LOL"))
	assert.NoError(t, err)
	tm := time.NewTimer(time.Millisecond * 1)
loop:
	for {
		select {
		case <-tm.C:
			n.peersMutex.Lock()
			for _, p := range n.peers {
				i := p.conn.(*net.ConnectionMock).SendCount()
				if assert.True(t, i > 0) {
					n.peersMutex.Unlock()
					break loop
				}
			}
			n.peersMutex.Lock()
			break loop
		default:

		}
	}
}

func TestNeighborhood_Broadcast3(t *testing.T) {
	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, new(mockSampler), new(mockCpool), log.New("tesT", "", ""))
	n.Start()
	<-n.Initial()
	chk := generateID([]byte("LOL"))
	n.oldMessageQ[chk] = struct{}{}

	err := n.Broadcast([]byte("LOL"))
	assert.Error(t, err)
	tm := time.NewTimer(time.Millisecond * 1)
loop:
	for {
		select {
		case <-tm.C:
			n.peersMutex.Lock()
			for _, p := range n.peers {
				i := p.conn.(*net.ConnectionMock).SendCount()
				if assert.True(t, i > 0) {
					n.peersMutex.Unlock()
					break loop
				}
			}
			n.peersMutex.Lock()
			break loop
		default:

		}
	}
}

func TestNeighborhood_Broadcast4(t *testing.T) {
	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, new(mockSampler), new(mockCpool), log.New("tesT", "", ""))
	n.Start()
	<-n.Initial()

	rnd := node.GenerateRandomNodeData()
	con, _ := new(mockCpool).GetConnection(rnd.Address(), rnd.PublicKey())
	n.AddIncomingPeer(rnd, con)

	n.Broadcast([]byte("LOL"))
	tm := time.NewTimer(time.Second)
loop:
	for {
		select {
		case <-tm.C:
			t.Error()
		default:
		}
		n.inpeersMutex.RLock()
		for _, p := range n.inpeers {
			i := p.conn.(*net.ConnectionMock).SendCount()
			if i > 0 {
				break loop
			}
		}
		n.inpeersMutex.RUnlock()

	}
}

func Test_Neihborhood_getMorePeers(t *testing.T) {
	// test normal flow
	numpeers := 3
	sampMock := new(mockSampler)
	cpoolMock := new(mockCpool)
	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, sampMock, cpoolMock, log.New("test", "", ""))

	res := n.getMorePeers(0) // this should'nt work
	assert.Equal(t, res, 0)

	//test no peers
	sampMock.f = func(count int) []node.Node {
		return []node.Node{}
	}

	res = n.getMorePeers(10)
	assert.Equal(t, res, 0)
	sampMock.f = nil

	// test connection error
	cpoolMock.f = func(address string, pk crypto.PublicKey) (net.Connection, error) {
		return nil, errors.New("can't make connection")
	}

	res = n.getMorePeers(1) // this should'nt work
	assert.Equal(t, res, 0)
	cpoolMock.f = nil // for next tests

	// not starting so we can test getMorePeers
	res = n.getMorePeers(numpeers)
	assert.Equal(t, res, numpeers)
	assert.Equal(t, len(n.peers), numpeers)

	// test inc peer

	nd := node.GenerateRandomNodeData()
	cn, _ := cpoolMock.GetConnection(nd.Address(), nd.PublicKey())
	n.AddIncomingPeer(nd, cn)

	n.peersMutex.RLock()
	_, ok := n.inpeers[nd.PublicKey().String()]
	n.peersMutex.RUnlock()
	assert.True(t, ok)

	//test replacing inc peer

	sampMock.f = func(count int) []node.Node {
		some := node.GenerateRandomNodesData(count - 1)
		some = append(some, nd)
		return some
	}

	res = n.getMorePeers(numpeers)
	assert.Equal(t, res, numpeers)
	assert.Equal(t, len(n.peers), numpeers+res)

	n.peersMutex.RLock()
	_, ok = n.peers[nd.PublicKey().String()]
	n.peersMutex.RUnlock()
	assert.True(t, ok)

	n.peersMutex.RLock()
	_, ok = n.inpeers[nd.PublicKey().String()]
	n.peersMutex.RUnlock()
	assert.False(t, ok)
}

func TestNeighborhood_Initial(t *testing.T) {
	sampMock := new(mockSampler)
	cpoolMock := new(mockCpool)
	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, sampMock, cpoolMock, log.New("test", "", ""))
	err := n.Start()
	assert.NoError(t, err, "start returned err") // should never error
	in := n.Initial()
	select {
	case <-in:
		break
	case <-time.After(time.Second * 2):
		t.Error("2 seconds passed")
	}
	n.Close()
}

func TestNeighborhood_Disconnect(t *testing.T) {
	sampMock := new(mockSampler)
	cpoolMock := new(mockCpool)

	out := node.GenerateRandomNodeData()
	t.Log("MY PEER ", out)

	sampMock.f = func(count int) []node.Node {
		some := node.GenerateRandomNodesData(count - 1)
		some = append(some, out)
		return some
	}

	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, sampMock, cpoolMock, log.New("tesT", "", ""))
	err := n.Start()
	assert.NoError(t, err) // should never error

	select {
	case <-n.Initial():
		break
	case <-time.After(time.Second):
		t.Error("didnt initialize")
	}

	sampMock.f = nil // dont give out on a normal state

	nd := node.GenerateRandomNodeData()
	cn, _ := cpoolMock.GetConnection(nd.Address(), nd.PublicKey())
	n.AddIncomingPeer(nd, cn)

	n.inpeersMutex.RLock()
	_, ok := n.inpeers[nd.PublicKey().String()]
	assert.True(t, ok)
	n.inpeersMutex.RUnlock()

	n.Disconnect(nd.PublicKey())

	n.inpeersMutex.RLock()
	_, ok = n.inpeers[nd.PublicKey().String()]
	assert.False(t, ok)
	n.inpeersMutex.RUnlock()

	cpoolMock.f = func(address string, pk crypto.PublicKey) (net.Connection, error) {
		return nil, errors.New("no connections")
	}

	n.Disconnect(out.PublicKey())

	n.peersMutex.RLock()
	_, ok = n.peers[nd.PublicKey().String()]
	assert.False(t, ok)
	n.peersMutex.RUnlock()

}

func TestNeighborhood_Close(t *testing.T) {
	sampMock := new(mockSampler)
	cpoolMock := new(mockCpool)

	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, sampMock, cpoolMock, log.New("tesT", "", ""))
	err := n.Start()
	assert.NoError(t, err) // should never error

	n.Close()
	<-n.shutdown
}
