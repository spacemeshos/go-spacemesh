package p2p

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/connectionpool"
	"github.com/spacemeshos/go-spacemesh/p2p/discovery"
	"github.com/stretchr/testify/require"
	inet "net"
	"sync/atomic"
	"testing"
	"time"

	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"sync"
)

const debug = false

type cpoolMock struct {
	f           func(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error)
	fExists     func(pk p2pcrypto.PublicKey) (net.Connection, error)
	calledClose int
	keyRemoved  chan p2pcrypto.Key
}

func NewCpoolMock() *cpoolMock {
	return &cpoolMock{
		keyRemoved: make(chan p2pcrypto.Key, 10),
	}
}

func (cp *cpoolMock) CloseConnection(key p2pcrypto.PublicKey) {
	cp.keyRemoved <- key
	cp.calledClose++
}

func (cp *cpoolMock) GetConnection(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error) {
	if cp.f != nil {
		return cp.f(address, pk)
	}
	return net.NewConnectionMock(pk), nil
}
func (cp *cpoolMock) GetConnectionIfExists(pk p2pcrypto.PublicKey) (net.Connection, error) {
	if cp.fExists != nil {
		return cp.fExists(pk)
	}
	return net.NewConnectionMock(pk), nil
}

func (cp *cpoolMock) Shutdown() {

}

func p2pTestInstance(t testing.TB, config config.Config) *swarm {
	p := p2pTestNoStart(t, config)
	err := p.Start()
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func p2pTestNoStart(t testing.TB, config config.Config) *swarm {
	config.TCPPort = 0
	p, err := newSwarm(context.TODO(), config, log.NewDefault(t.Name()), "")
	if err != nil {
		t.Fatal("err creating a swarm", err)
	}
	if p == nil {
		t.Fatal("swarm was nil")
	}
	return p
}

const exampleProtocol = "EX"
const examplePayload = "Example"

func TestNew(t *testing.T) {
	s, err := New(context.TODO(), config.DefaultConfig(), log.NewDefault(t.Name()), "")
	require.NoError(t, err)
	err = s.Start()
	require.NoError(t, err)
	require.NotNil(t, s, "its nil")
	s.Shutdown()
}

func Test_newSwarm(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TCPPort = 0
	s, err := newSwarm(context.TODO(), cfg, log.NewDefault(t.Name()), "")
	assert.NoError(t, err)
	err = s.Start()
	assert.NoError(t, err)
	assert.NotNil(t, s)
	s.Shutdown()
}

func TestSwarm_Shutdown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TCPPort = 0
	s, err := newSwarm(context.TODO(), cfg, log.NewDefault(t.Name()), "")
	assert.NoError(t, err)
	err = s.Start()
	assert.NoError(t, err)
	conn, disc := s.SubscribePeerEvents()
	s.Shutdown()

	select {
	case _, ok := <-s.shutdown:
		assert.False(t, ok)
	case <-time.After(1 * time.Second):
		t.Error("Failed to shutdown")
	}

	select {
	case _, ok := <-conn:
		assert.False(t, ok)
	default:
		t.Error("conn was not closed")
	}
	select {
	case _, ok := <-disc:
		assert.False(t, ok)
	default:
		t.Error("disc was not closed")
	}
}

func TestSwarm_RegisterProtocolNoStart(t *testing.T) {
	s, err := newSwarm(context.TODO(), config.DefaultConfig(), log.NewDefault(t.Name()), "")
	msgs := s.RegisterDirectProtocol("Anton")
	assert.NotNil(t, msgs)
	assert.NoError(t, err)
}

func TestSwarm_processMessage(t *testing.T) {
	s := swarm{}
	s.config = config.DefaultConfig()
	s.logger = log.NewDefault(t.Name())
	s.lNode, _ = node.GenerateTestNode(t)
	r := node.GenerateRandomNodeData()
	c := &net.ConnectionMock{}
	c.SetRemotePublicKey(r.PublicKey())
	ime := net.IncomingMessageEvent{Message: []byte("0"), Conn: c}
	s.processMessage(ime) // should error

	assert.True(t, c.Closed())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func Test_ConnectionBeforeMessage(t *testing.T) {

	numNodes := 5
	var wg sync.WaitGroup

	p2 := p2pTestNoStart(t, config.DefaultConfig())
	c2 := p2.RegisterDirectProtocol(exampleProtocol)
	require.NoError(t, p2.Start())
	defer p2.Shutdown()

	go func() {
		for {
			msg := <-c2 // immediate response will probably trigger GetConnection fast
			require.NoError(t, p2.SendMessage(msg.Sender(), exampleProtocol, []byte("RESP")))
			wg.Done()
		}
	}()

	oldCpool := p2.cPool.(*connectionpool.ConnectionPool)

	//called := make(chan struct{}, numNodes)
	cpm := new(cpoolMock)
	cpm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error) {
		c, err := oldCpool.GetConnectionIfExists(pk)
		if err != nil {
			t.Fatal("Didn't get connection yet while SendMessage called GetConnection")
		}
		return c, nil
	}

	p2.cPool = cpm

	payload := []byte(RandString(10))

	sa := &swarmArray{}

	for i := 0; i < numNodes; i++ {
		wg.Add(1)
		go func() {
			p1 := p2pTestNoStart(t, config.DefaultConfig())
			_ = p1.RegisterDirectProtocol(exampleProtocol)
			require.NoError(t, p1.Start())
			sa.add(p1)
			_, err := p1.cPool.GetConnection(p2.network.LocalAddr(), p2.lNode.PublicKey())
			require.NoError(t, err)
			require.NoError(t, p1.SendMessage(p2.lNode.PublicKey(), exampleProtocol, payload))
		}()
	}

	wg.Wait()
	cpm.f = nil
	sa.clean()

}

func RandString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func sendDirectMessage(t *testing.T, sender *swarm, recvPub p2pcrypto.PublicKey, inChan chan service.DirectMessage, checkpayload bool) {
	payload := []byte(RandString(10))
	err := sender.SendMessage(recvPub, exampleProtocol, payload)
	require.NoError(t, err)
	select {
	case msg := <-inChan:
		if checkpayload {
			assert.Equal(t, msg.Bytes(), payload)
		}
		assert.Equal(t, msg.Sender().String(), sender.lNode.PublicKey().String())
		break
	case <-time.After(5 * time.Second):
		t.Error("Took too much time to receive")
	}
}

func TestSwarm_RoundTrip(t *testing.T) {
	p1 := p2pTestNoStart(t, config.DefaultConfig())
	p2 := p2pTestNoStart(t, config.DefaultConfig())

	exchan1 := p1.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan1, p1.directProtocolHandlers[exampleProtocol])
	exchan2 := p2.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan2, p2.directProtocolHandlers[exampleProtocol])

	require.NoError(t, p1.Start())
	require.NoError(t, p2.Start())

	_, err := p2.cPool.GetConnection(p1.network.LocalAddr(), p1.lNode.PublicKey())
	require.NoError(t, err)

	sendDirectMessage(t, p2, p1.lNode.PublicKey(), exchan1, true)
	sendDirectMessage(t, p1, p2.lNode.PublicKey(), exchan2, true)

	p1.Shutdown()
	p2.Shutdown()
}

func TestSwarm_MultipleMessages(t *testing.T) {
	p1 := p2pTestNoStart(t, config.DefaultConfig())
	p2 := p2pTestNoStart(t, config.DefaultConfig())

	exchan1 := p1.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan1, p1.directProtocolHandlers[exampleProtocol])
	exchan2 := p2.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan2, p2.directProtocolHandlers[exampleProtocol])

	require.NoError(t, p1.Start())
	require.NoError(t, p2.Start())

	err := p2.SendMessage(p1.lNode.PublicKey(), exampleProtocol, []byte(examplePayload))
	require.Error(t, err, "ERR") // should'nt be in routing table

	_, err = p2.cPool.GetConnection(p1.network.LocalAddr(), p1.lNode.PublicKey())
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() { sendDirectMessage(t, p2, p1.lNode.PublicKey(), exchan1, false); wg.Done() }()
	}
	wg.Wait()

	p1.Shutdown()
	p2.Shutdown()
}

type swarmArray struct {
	s   []*swarm
	mtx sync.Mutex
}

func (sa *swarmArray) add(s *swarm) {
	sa.mtx.Lock()
	sa.s = append(sa.s, s)
	sa.mtx.Unlock()
}

func (sa *swarmArray) clean() {
	sa.mtx.Lock()
	for _, s := range sa.s {
		s.Shutdown()
	}
	sa.mtx.Unlock()
}

func TestSwarm_MultipleMessagesFromMultipleSenders(t *testing.T) {
	const Senders = 100

	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.Bootstrap = false

	p1 := p2pTestInstance(t, cfg)

	exchan1 := p1.RegisterDirectProtocol(exampleProtocol)
	assert.Equal(t, exchan1, p1.directProtocolHandlers[exampleProtocol])

	pend := make(map[string]chan struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	go func() {
		for {
			msg := <-exchan1
			sender := msg.Sender().String()
			mu.Lock()
			c, ok := pend[sender]
			if !ok {
				t.FailNow()
			}
			close(c)
			delete(pend, sender)
			mu.Unlock()
			wg.Done()
		}
	}()

	sa := &swarmArray{}
	for i := 0; i < Senders; i++ {
		wg.Add(1)
		go func() {
			p := p2pTestNoStart(t, cfg)
			require.NoError(t, p.Start())
			sa.add(p)
			p.cPool.GetConnection(p1.network.LocalAddr(), p1.lNode.PublicKey())
			mychan := make(chan struct{})
			mu.Lock()
			pend[p.lNode.PublicKey().String()] = mychan
			mu.Unlock()

			payload := []byte(RandString(10))
			err := p.SendMessage(p1.lNode.PublicKey(), exampleProtocol, payload)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	p1.Shutdown()
	sa.clean()
}

func TestSwarm_MultipleMessagesFromMultipleSendersToMultipleProtocols(t *testing.T) {

	const Senders = 100
	const Protos = 50

	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.Bootstrap = false

	pend := make(map[string]chan struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	p1 := p2pTestNoStart(t, cfg)

	var protos []string

	for i := 0; i < Protos; i++ {
		prt := RandString(10)
		protos = append(protos, prt)
		exchan := p1.RegisterDirectProtocol(prt)

		go func() {
			for {
				msg := <-exchan
				sender := msg.Sender().String()
				mu.Lock()
				c, ok := pend[sender]
				if !ok {
					t.FailNow()
				}
				close(c)
				delete(pend, sender)
				mu.Unlock()
				wg.Done()
			}
		}()

	}

	require.NoError(t, p1.Start())

	sa := &swarmArray{}
	for i := 0; i < Senders; i++ {
		wg.Add(1)
		go func() {
			p := p2pTestNoStart(t, cfg)
			require.NoError(t, p.Start())
			sa.add(p)
			mychan := make(chan struct{})
			mu.Lock()
			pend[p.lNode.PublicKey().String()] = mychan
			mu.Unlock()

			randProto := rand.Int31n(Protos)
			if randProto == Protos {
				randProto--
			}

			payload := []byte(RandString(10))
			_, err := p.cPool.GetConnection(p1.network.LocalAddr(), p1.lNode.PublicKey())
			require.NoError(t, err)
			err = p.SendMessage(p1.lNode.PublicKey(), protos[randProto], payload)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	p1.Shutdown()
	sa.clean()
}

func TestSwarm_RegisterProtocol(t *testing.T) {
	const numPeers = 100
	nods := make([]*swarm, 0, numPeers)
	cfg := config.DefaultConfig()
	for i := 0; i < numPeers; i++ {
		nod := p2pTestNoStart(t, cfg)
		nod.RegisterDirectProtocol(exampleProtocol) // this is example
		nods = append(nods, nod)
	}
	count := 0
	for i := range nods {
		_, ok := nods[i].directProtocolHandlers[exampleProtocol]
		require.True(t, ok)
		count++
	}

	require.Equal(t, count, numPeers)
}

func TestSwarm_onRemoteClientMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	//addr := inet.TCPAddr{inet.ParseIP("0.0.0.0"), 0000, ""}

	id, err := node.NewNodeIdentity()
	assert.NoError(t, err, "we cant make node ?")

	p := p2pTestNoStart(t, cfg)
	nmock := new(net.ConnectionMock)
	nmock.SetRemotePublicKey(id.PublicKey())

	// Test bad format
	imc := net.IncomingMessageEvent{nmock, nil}
	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrBadFormat1)

	// Test No Session
	imc.Message = []byte("test")

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrNoSession)

	//Test bad session
	session := &net.SessionMock{}
	session.SetDecrypt(nil, errors.New("fail"))
	imc.Conn.SetSession(session)

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrFailDecrypt)

	//// Test bad format again
	session.SetDecrypt([]byte("wont_format_fo_protocol_message"), nil)

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrBadFormat2)

	goodmsg := &ProtocolMessage{
		Metadata: &ProtocolMessageMetadata{AuthPubkey: id.PublicKey().Bytes(), NextProtocol: exampleProtocol,
			Timestamp: time.Now().Unix(), ClientVersion: config.ClientVersion}, // not signed
		Payload: &Payload{Payload: []byte(examplePayload)},
	}

	goodbin, _ := types.InterfaceToBytes(goodmsg)

	imc.Message = goodbin
	session.SetDecrypt(goodbin, nil)

	goodmsg.Metadata.Timestamp = time.Now().Add(-time.Hour).Unix()
	nosynced, _ := types.InterfaceToBytes(goodmsg)
	session.SetDecrypt(nosynced, nil)
	// Test out of sync
	imc.Message = nosynced

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, ErrOutOfSync, err)

	// Test no protocol
	goodmsg.Metadata.Timestamp = time.Now().Unix()

	goodbin, _ = types.InterfaceToBytes(goodmsg)
	imc.Message = goodbin
	session.SetDecrypt(goodbin, nil)

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrNoProtocol)

	// Test no err

	var wg sync.WaitGroup
	c := p.RegisterDirectProtocol(exampleProtocol)
	go func() {
		ti := time.After(1 * time.Second)
		select {
		case <-c:
			wg.Done()
			break
		case <-ti:
			t.Error("Didn't get message in time")

		}
	}()
	wg.Add(1)
	err = p.onRemoteClientMessage(imc)
	assert.NoError(t, err)
	wg.Wait()

}

func assertNewPeerEvent(t *testing.T, peer p2pcrypto.PublicKey, connChan <-chan p2pcrypto.PublicKey) {
	select {
	case newPeer := <-connChan:
		assert.Equal(t, peer.String(), newPeer.String())
	default:
		assert.Error(t, errors.New("no new peer event"))
	}
}

func assertNewPeerEvents(t *testing.T, expCount int, connChan <-chan p2pcrypto.PublicKey) {
	//var actCount int
	//loop:
	//for {
	//	select {
	//	case _ = <-connChan:
	//		actCount++
	//	default:
	//		break loop
	//	}
	//}
	assert.Equal(t, expCount, len(connChan))
}

func assertNoNewPeerEvent(t *testing.T, eventChan <-chan p2pcrypto.PublicKey) {
	select {
	case newPeer := <-eventChan:
		assert.Error(t, errors.New("unexpected new peer event, peer "+newPeer.String()))
	default:
		return
	}
}

func assertNewDisconnectedPeerEvent(t *testing.T, peer p2pcrypto.PublicKey, discChan <-chan p2pcrypto.PublicKey) {
	select {
	case newPeer := <-discChan:
		assert.Equal(t, peer.String(), newPeer.String())
	default:
		assert.Error(t, errors.New("no new peer event"))
	}
}

func drainPeerEvents(eventChan <-chan p2pcrypto.PublicKey) {
loop:
	for {
		select {
		case <-eventChan:
			continue loop
		default:
			break loop
		}
	}
}

func Test_Swarm_getMorePeers(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	res := n.getMorePeers(0) // this should'nt work
	assert.Equal(t, res, 0)
	assertNoNewPeerEvent(t, conn)
}

func Test_Swarm_getMorePeers2(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	mdht := new(discovery.MockPeerStore)
	n.discover = mdht
	// this will return 0 peers because SelectPeers returns empty array when not set

	res := n.getMorePeers(10)
	assert.Equal(t, res, 0)
	assertNoNewPeerEvent(t, conn)
}

func Test_Swarm_getMorePeers3(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	mdht := new(discovery.MockPeerStore)
	n.discover = mdht
	testNode := node.GenerateRandomNodeData()
	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		return []*node.NodeInfo{testNode}
	}

	cpm := new(cpoolMock)

	// test connection error
	cpm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error) {
		return nil, errors.New("can't make connection")
	}

	n.cPool = cpm
	res := n.getMorePeers(1) // this should'nt work
	assert.Equal(t, res, 0)
	assertNoNewPeerEvent(t, conn)
}

func Test_Swarm_getMorePeers4(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	mdht := new(discovery.MockPeerStore)
	n.discover = mdht

	testNode := node.GenerateRandomNodeData()
	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		return []*node.NodeInfo{testNode}
	}

	cpm := new(cpoolMock)

	n.cPool = cpm

	res := n.getMorePeers(1)
	assert.Equal(t, 1, res)
	assert.Equal(t, len(n.outpeers), 1)
	assert.True(t, n.hasOutgoingPeer(testNode.PublicKey()))
	assertNewPeerEvents(t, 1, conn)
	assertNewPeerEvent(t, testNode.PublicKey(), conn)

	drainPeerEvents(conn)
}

func Test_Swarm_getMorePeers5(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	//res := n.getMorePeers(0) // this should'nt work
	//assert.Equal(t, res, 0)
	//assertNoNewPeerEvent(t, conn)

	mdht := new(discovery.MockPeerStore)
	n.discover = mdht

	cpm := new(cpoolMock)

	n.cPool = cpm

	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		return node.GenerateRandomNodesData(qty)
	}

	res := n.getMorePeers(numpeers)
	assert.Equal(t, res, numpeers)
	assert.Equal(t, len(n.outpeers), numpeers) // there's already one inside
	assertNewPeerEvents(t, numpeers, conn)
	drainPeerEvents(conn) // so they wont interrupt next test
}

func Test_Swarm_getMorePeers6(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	//res := n.getMorePeers(0) // this should'nt work
	//assert.Equal(t, res, 0)
	//assertNoNewPeerEvent(t, conn)

	mdht := new(discovery.MockPeerStore)
	n.discover = mdht

	cpm := new(cpoolMock)

	n.cPool = cpm

	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		return node.GenerateRandomNodesData(qty)
	}

	//test inc peer
	nd := node.GenerateRandomNodeData()
	n.addIncomingPeer(nd.PublicKey())

	assert.True(t, n.hasIncomingPeer(nd.PublicKey()))
	assertNewPeerEvents(t, 1, conn)
	assertNewPeerEvent(t, nd.PublicKey(), conn)

	//test not replacing inc peer
	//
	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		some := node.GenerateRandomNodesData(qty - 1)
		some = append(some, nd)
		return some
	}

	res := n.getMorePeers(numpeers)
	assert.Equal(t, res, numpeers-1)
	assert.False(t, n.hasOutgoingPeer(nd.PublicKey()))
	assert.True(t, n.hasIncomingPeer(nd.PublicKey()))
}

func Test_Swarm_callCpoolCloseCon(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxInboundPeers = 0
	p1 := p2pTestNoStart(t, cfg)
	p2 := p2pTestNoStart(t, cfg)
	exchan1 := p1.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan1, p1.directProtocolHandlers[exampleProtocol])
	exchan2 := p2.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan2, p2.directProtocolHandlers[exampleProtocol])

	require.NoError(t, p1.Start())
	require.NoError(t, p2.Start())

	cpm := NewCpoolMock()
	p1.cPool = cpm

	wg := sync.WaitGroup{}
	wg.Add(2)

	_, err := p2.cPool.GetConnection(p1.network.LocalAddr(), p1.lNode.PublicKey())
	require.NoError(t, err)

	_, err = p1.cPool.GetConnection(p2.network.LocalAddr(), p2.lNode.PublicKey())
	require.NoError(t, err)

	select {
	case key := <-cpm.keyRemoved:
		assert.True(t, key == p1.lNode.PublicKey() || key == p2.lNode.PublicKey())
	case <-time.After(5 * time.Second):
		t.Error("peers were not removed from cpool")
	}

	p1.Shutdown()
	p2.Shutdown()
}

func TestNeighborhood_Initial(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.RandomConnections = 3
	cfg.SwarmConfig.Gossip = true
	cfg.SwarmConfig.Bootstrap = false

	p := p2pTestNoStart(t, cfg)
	mdht := new(discovery.MockPeerStore)
	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		return node.GenerateRandomNodesData(qty)
	}

	p.discover = mdht

	err := p.Start()
	assert.NoError(t, err)
	ti := time.After(time.Millisecond)
	select {
	case <-p.initial:
		t.Error("Start succeded")
	case <-ti:
		break
	}

	p.Shutdown()

	p = p2pTestNoStart(t, cfg)
	p.discover = mdht
	cpm := NewCpoolMock()
	cpm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error) {
		return net.NewConnectionMock(pk), nil
	}
	p.cPool = cpm

	err = p.Start()
	assert.NoError(t, err)
	ti = time.After(time.Second * 1)
	select {
	case <-p.initial:
		break
	case <-ti:
		t.Error("Start succeded")
	}

	p.Shutdown()
}

func TestNeighborhood_Disconnect(t *testing.T) {
	n := p2pTestNoStart(t, config.DefaultConfig())
	_, disc := n.SubscribePeerEvents()
	rnd := node.GenerateRandomNodeData()
	n.addIncomingPeer(rnd.PublicKey())

	assert.True(t, n.hasIncomingPeer(rnd.PublicKey()))
	n.Disconnect(rnd.PublicKey())
	assertNewDisconnectedPeerEvent(t, rnd.PublicKey(), disc)
	ti := time.After(time.Millisecond)
	select {
	case <-n.morePeersReq:
		t.Error("got more peers on inbound")
	case <-ti:
		break
	}
	assert.False(t, n.hasIncomingPeer(rnd.PublicKey()))

	// manualy add an incoming peer
	rnd2 := node.GenerateRandomNodeData()
	n.outpeers[rnd2.PublicKey()] = struct{}{} // no need to lock nothing's happening
	go n.Disconnect(rnd2.PublicKey())
	ti = time.After(time.Millisecond)
	select {
	case <-n.morePeersReq:
		break
	case <-ti:
		t.Error("didnt get morepeers")
	}
	assertNewDisconnectedPeerEvent(t, rnd2.PublicKey(), disc)
}

func TestSwarm_AddIncomingPeer(t *testing.T) {
	p := p2pTestInstance(t, config.DefaultConfig())
	rnd := node.GenerateRandomNodeData()
	p.addIncomingPeer(rnd.PublicKey())

	p.inpeersMutex.RLock()
	_, ok := p.inpeers[rnd.PublicKey()]
	p.inpeersMutex.RUnlock()

	assert.True(t, ok)

	nds := node.GenerateRandomNodesData(config.DefaultConfig().MaxInboundPeers)
	for i := 0; i < len(nds); i++ {
		p.addIncomingPeer(nds[i].PublicKey())
	}

	require.Equal(t, len(p.inpeers), config.DefaultConfig().MaxInboundPeers)
	p.inpeersMutex.RLock()
	_, ok = p.inpeers[nds[len(nds)-1].PublicKey()]
	p.inpeersMutex.RUnlock()
	assert.False(t, ok)
}

func TestSwarm_AskPeersSerial(t *testing.T) {
	p := p2pTestNoStart(t, config.DefaultConfig())
	dsc := &discovery.MockPeerStore{}

	timescalled := uint32(0)
	block := make(chan struct{})

	dsc.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		atomic.AddUint32(&timescalled, 1)
		<-block
		return node.GenerateRandomNodesData(qty) // will trigger sending on morepeersreq
	}

	p.discover = dsc

	p.startNeighborhood()

	p.morePeersReq <- struct{}{}
	p.morePeersReq <- struct{}{}

	block <- struct{}{}
	require.Equal(t, atomic.LoadUint32(&timescalled), uint32(1))
	block <- struct{}{}
	require.Equal(t, atomic.LoadUint32(&timescalled), uint32(2))
	block <- struct{}{}
	require.Equal(t, atomic.LoadUint32(&timescalled), uint32(3))
}

func Test_NodeInfo(t *testing.T) {
	pinged := node.NewNode(p2pcrypto.NewRandomPubkey(), net.IPv4LoopbackAddress, 1010, 1020)
	raw, err := types.InterfaceToBytes(&pinged)
	if err != nil {
		panic("LOL")
	}
	fmt.Println("GOT MSG ", raw)
	pinged2 := &node.NodeInfo{}
	err = types.BytesToInterface(raw, pinged2)
	if err != nil {
		panic(err)
	}
}

func TestNeighborhood_ReportConnectionResult(t *testing.T) {
	const PeerNum = 10
	n := p2pTestNoStart(t, config.DefaultConfig())
	goodcount := 0
	attemptcount := 0

	ps := &discovery.MockPeerStore{}

	ps.GoodFunc = func(key p2pcrypto.PublicKey) {
		goodcount++
	}

	ps.AttemptFunc = func(key p2pcrypto.PublicKey) {
		attemptcount++
	}

	rnds := node.GenerateRandomNodesData(PeerNum)

	ps.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		return rnds
	}

	n.discover = ps

	cm := &cpoolMock{}

	cm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (connection net.Connection, e error) {
		return nil, errors.New("coudln't create connection")
	}

	n.cPool = cm

	//_, disc := n.SubscribePeerEvents()

	n.getMorePeers(PeerNum)
	require.Equal(t, attemptcount, PeerNum)

	goodlist := make(map[p2pcrypto.PublicKey]struct{})

	cm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (connection net.Connection, e error) {
		if _, ok := goodlist[pk]; ok {
			return net.NewConnectionMock(pk), nil
		}
		return nil, errors.New("not found")
	}

	realnode := p2pTestNoStart(t, config.DefaultConfig())
	realnode2 := p2pTestNoStart(t, config.DefaultConfig())
	realnodeinfo := &node.NodeInfo{realnode.lNode.PublicKey().Array(), inet.IPv4zero, 0, 0}
	realnode2info := &node.NodeInfo{realnode2.lNode.PublicKey().Array(), inet.IPv4zero, 0, 0}

	goodlist[realnode.lNode.PublicKey()] = struct{}{}
	goodlist[realnode2.lNode.PublicKey()] = struct{}{}

	newrnds := node.GenerateRandomNodesData(PeerNum - 2)

	ps.SelectPeersFunc = func(ctx context.Context, qty int) []*node.NodeInfo {
		return append([]*node.NodeInfo{realnodeinfo, realnode2info}, newrnds...)
	}

	n.getMorePeers(PeerNum)

	require.Equal(t, 2, goodcount)
	require.True(t, attemptcount == PeerNum*2-2)
}
