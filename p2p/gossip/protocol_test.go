package gossip

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
	"testing"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
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

type mockBaseNetwork struct {
	msgSentByPeer        map[string]uint32
	inbox                chan service.Message
	connSubs             []chan crypto.PublicKey
	discSubs             []chan crypto.PublicKey
	totalMsgCount        int
	processProtocolCount int
	msgMutex             sync.Mutex
}

func newMockBaseNetwork() *mockBaseNetwork {
	return &mockBaseNetwork{
		make(map[string]uint32),
		make(chan service.Message, 20),
		make([]chan crypto.PublicKey, 0, 5),
		make([]chan crypto.PublicKey, 0, 5),
		0,
		0,
		sync.Mutex{},
	}
}

func (mbn *mockBaseNetwork) SendMessage(peerPubKey string, protocol string, payload []byte) error {
	mbn.msgMutex.Lock()
	mbn.msgSentByPeer[peerPubKey]++
	mbn.totalMsgCount++
	mbn.msgMutex.Unlock()
	return nil
}

func (mbn *mockBaseNetwork) RegisterProtocol(protocol string) chan service.Message {
	return mbn.inbox
}

func (mbn *mockBaseNetwork) SubscribePeerEvents() (conn chan crypto.PublicKey, disc chan crypto.PublicKey) {
	conn = make(chan crypto.PublicKey, 20)
	disc = make(chan crypto.PublicKey, 20)

	mbn.connSubs = append(mbn.connSubs, conn)
	mbn.discSubs = append(mbn.discSubs, disc)
	return
}

func (mbn *mockBaseNetwork) ProcessProtocolMessage(sender node.Node, protocol string, payload []byte) error {
	mbn.processProtocolCount++
	return nil
}

func (mbn *mockBaseNetwork) addRandomPeers(cnt int) {
	for i := 0; i < cnt; i++ {
		_, pub, _ := crypto.GenerateKeyPair()
		mbn.addRandomPeer(pub)
	}
}

func (mbn *mockBaseNetwork) addRandomPeer(pub crypto.PublicKey) {
	for _, p := range (mbn.connSubs) {
		p <- pub
	}
}

func (mbn *mockBaseNetwork) totalMessageSent() int {
	return mbn.totalMsgCount
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

type TestMessage struct {
	data []byte
}

func (tm TestMessage) Sender() node.Node {
	return node.Node{}
}

func (tm TestMessage) setData(msg []byte) {
	tm.data = msg
}

func (tm TestMessage) Data() []byte {
	return tm.data
}

//todo : more unit tests

func TestNeighborhood_AddIncomingPeer(t *testing.T) {
	n := NewProtocol(config.DefaultConfig().SwarmConfig, newMockBaseNetwork(), log.New("tesT", "", ""))
	n.Start()
	_, pub, _ := crypto.GenerateKeyPair()
	n.addPeer(pub)

	assert.True(t, n.hasPeer(pub))
	assert.Equal(t, 1, n.peersCount())
}

func TestNeighborhood_Relay(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))
	n.Start()
	net.addRandomPeers(20)

	//n.Broadcast([]byte("LOL"))
	var msg service.Message = TestMessage{[]byte("LOL")}
	net.inbox <- msg
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, net.processProtocolCount)
}

func TestNeighborhood_Broadcast(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))
	n.Start()
	net.addRandomPeers(20)

	n.Broadcast([]byte("LOL"))
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t,20, net.totalMessageSent())
}

func TestNeighborhood_Relay2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))
	n.Start()

	var msg service.Message = TestMessage{[]byte("LOL")}
	net.inbox <- msg
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t,0, net.totalMessageSent())

	net.addRandomPeers(20)
	net.inbox <- msg
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t,20, net.totalMessageSent())
}

func TestNeighborhood_Broadcast2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))
	n.Start()

	msgB := []byte("LOL")
	n.Broadcast(msgB)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t,0, net.totalMessageSent())

	net.addRandomPeers(20)
	var msg service.Message = TestMessage{msgB}
	net.inbox <- msg
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t,20, net.totalMessageSent())
}

func TestNeighborhood_Broadcast3(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))
	n.Start()

	net.addRandomPeers(20)

	msgB := []byte("LOL")
	n.Broadcast(msgB)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t,20, net.totalMessageSent())

	var msg service.Message = TestMessage{msgB}
	net.inbox <- msg
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t,20, net.totalMessageSent())
}

func TestNeighborhood_Relay3(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))
	n.Start()

	var msg service.Message = TestMessage{[]byte("LOL")}
	net.inbox <- msg
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t,0, net.totalMessageSent())

	net.addRandomPeers(20)
	net.inbox <- msg
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t,20, net.totalMessageSent())

	net.addRandomPeers(1)
	net.inbox <- msg
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t,21, net.totalMessageSent())
}

func TestNeighborhood_Start(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))

	// before Start
	net.addRandomPeers(20)
	assert.Equal(t,0, n.peersCount())

	n.Start()

	net.addRandomPeers(20)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t,20, n.peersCount())
}

func TestNeighborhood_Close(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))

	n.Start()
	net.addRandomPeers(20)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t,20, n.peersCount())

	n.Close()
	net.addRandomPeers(20)
	assert.Equal(t,20, n.peersCount())
}

func TestNeighborhood_Disconnect(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, log.New("tesT", "", ""))

	n.Start()
	_, pub1, _ := crypto.GenerateKeyPair()
	n.addPeer(pub1)
	_, pub2, _ := crypto.GenerateKeyPair()
	n.addPeer(pub2)
	assert.Equal(t,2, n.peersCount())

	net.inbox <- TestMessage{[]byte("LOL")}
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t,2, net.totalMessageSent())

	n.removePeer(pub1)
	net.inbox <- TestMessage{[]byte("LOL2")}
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t,3, net.totalMessageSent())

	n.addPeer(pub1)
	net.inbox <- TestMessage{[]byte("LOL")}
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t,4, net.totalMessageSent())
}
