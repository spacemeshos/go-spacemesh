package p2p

import (
	"fmt"
	inet "net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nattraversal"
	"github.com/spacemeshos/go-spacemesh/p2p/connectionpool"
	"github.com/spacemeshos/go-spacemesh/p2p/discovery"
	"github.com/stretchr/testify/require"

	"context"
	"errors"
	"sync"

	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
)

type cpoolMock struct {
	f           func(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error)
	fExists     func(pk p2pcrypto.PublicKey) (net.Connection, error)
	calledClose int
	keyRemoved  chan p2pcrypto.Key
}

func newCpoolMock() *cpoolMock {
	return &cpoolMock{
		keyRemoved: make(chan p2pcrypto.Key, 10),
	}
}

func (cp *cpoolMock) CloseConnection(key p2pcrypto.PublicKey) {
	cp.keyRemoved <- key
	cp.calledClose++
}

func (cp *cpoolMock) GetConnection(ctx context.Context, address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error) {
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

func p2pTestInstance(t testing.TB, config config.Config) *Switch {
	p := p2pTestNoStart(t, config)
	err := p.Start(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func p2pTestNoStart(t testing.TB, config config.Config) *Switch {
	p, err := newSwarm(context.TODO(), config, log.NewDefault(t.Name()), "")
	if err != nil {
		t.Fatal("err creating a Switch", err)
	}
	if p == nil {
		t.Fatal("Switch was nil")
	}
	return p
}

const exampleProtocol = "EX"
const examplePayload = "Example"

func TestNew(t *testing.T) {
	s, err := New(context.TODO(), configWithPort(0), log.NewDefault(t.Name()), "")
	require.NoError(t, err)
	err = s.Start(context.TODO())
	require.NoError(t, err)
	require.NotNil(t, s, "its nil")
	s.Shutdown()
}

func Test_newSwarm(t *testing.T) {
	s, err := newSwarm(context.TODO(), configWithPort(0), log.NewDefault(t.Name()), "")
	assert.NoError(t, err)
	err = s.Start(context.TODO())
	assert.NoError(t, err)
	assert.NotNil(t, s)
	s.Shutdown()
}

func TestSwarm_Shutdown(t *testing.T) {
	s, err := newSwarm(context.TODO(), configWithPort(0), log.NewDefault(t.Name()), "")
	assert.NoError(t, err)
	err = s.Start(context.TODO())
	assert.NoError(t, err)
	conn, disc := s.SubscribePeerEvents()
	s.Shutdown()

	select {
	case _, ok := <-s.shutdownCtx.Done():
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
	s, err := newSwarm(context.TODO(), configWithPort(7513), log.NewDefault(t.Name()), "")
	assert.NoError(t, err)
	msgs := s.RegisterDirectProtocol("Anton")
	assert.NotNil(t, msgs)
}

func TestSwarm_processMessage(t *testing.T) {
	s := Switch{
		inpeers:      make(map[p2pcrypto.PublicKey]struct{}),
		outpeers:     make(map[p2pcrypto.PublicKey]struct{}),
		delPeerSub:   make([]chan p2pcrypto.PublicKey, 0),
		morePeersReq: make(chan struct{}, 1),
	}
	cpmock := newCpoolMock()
	s.cPool = cpmock
	s.config = config.DefaultConfig()
	s.logger = log.NewDefault(t.Name())
	s.lNode, _ = node.GenerateTestNode(t)
	r := node.GenerateRandomNodeData()

	s.inpeers[r.PublicKey()] = struct{}{}

	c := &net.ConnectionMock{}
	c.SetRemotePublicKey(r.PublicKey())
	ime := net.IncomingMessageEvent{Message: []byte("0"), Conn: c}
	s.processMessage(context.TODO(), ime) // should error

	select {
	case k := <-cpmock.keyRemoved:
		require.Equal(t, k.Bytes(), r.Bytes())
	case <-time.After(2 * time.Second):
		t.Error("didn't get key removed event")
	}

	_, ok := s.inpeers[r.PublicKey()]
	require.False(t, ok)
	assert.True(t, c.Closed())

	r2 := node.GenerateRandomNodeData()

	s.outpeers[r2.PublicKey()] = struct{}{}

	c2 := &net.ConnectionMock{}
	c2.SetRemotePublicKey(r2.PublicKey())
	ime2 := net.IncomingMessageEvent{Message: []byte("0"), Conn: c2}
	s.processMessage(context.TODO(), ime2) // should error

	select {
	case k := <-cpmock.keyRemoved:
		require.Equal(t, k.Bytes(), r2.Bytes())
	case <-time.After(2 * time.Second):
		t.Error("didn't get key removed event")
	}

	_, ok2 := s.inpeers[r2.PublicKey()]
	require.False(t, ok2)
	assert.True(t, c2.Closed())

	select {
	case <-s.morePeersReq:
		break
	case <-time.After(2 * time.Second):
		t.Error("didn't get morePeersReq")
	}
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func configWithPort(port int) config.Config {
	cfg := config.DefaultConfig()
	cfg.AcquirePort = false
	cfg.TCPPort = port
	return cfg
}

func Test_ConnectionBeforeMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	numNodes := 5
	var wg sync.WaitGroup

	p2 := p2pTestNoStart(t, configWithPort(0))
	c2 := p2.RegisterDirectProtocol(exampleProtocol)
	require.NoError(t, p2.Start(context.TODO()))
	defer p2.Shutdown()

	go func() {
		for {
			select {
			case msg := <-c2: // immediate response will probably trigger GetConnection fast
				require.NoError(t, p2.SendMessage(context.TODO(), msg.Sender(), exampleProtocol, []byte("RESP")))
				wg.Done()
			case <-ctx.Done():
				return
			}
		}
	}()

	oldCpool := p2.cPool.(*connectionpool.ConnectionPool)

	//called := make(chan struct{}, numNodes)
	cpm := new(cpoolMock)

	cpm.fExists = func(pk p2pcrypto.PublicKey) (net.Connection, error) {
		c, err := oldCpool.GetConnectionIfExists(pk)
		if err != nil {
			t.Fatal("Didn't get connection yet while called GetConnection")
		}
		return c, nil
	}

	p2.cPool = cpm

	payload := []byte(RandString(10))

	sa := &swarmArray{}

	for i := 0; i < numNodes; i++ {
		wg.Add(1)
		go func() {
			p1 := p2pTestNoStart(t, configWithPort(0))
			_ = p1.RegisterDirectProtocol(exampleProtocol)
			require.NoError(t, p1.Start(context.TODO()))
			sa.add(p1)
			_, err := p1.cPool.GetConnection(context.TODO(), p2.network.LocalAddr(), p2.lNode.PublicKey())
			require.NoError(t, err)
			require.NoError(t, p1.SendMessage(context.TODO(), p2.lNode.PublicKey(), exampleProtocol, payload))
		}()
	}

	wg.Wait()
	cpm.f = nil
	sa.clean()
	cancel()

}

func RandString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func sendDirectMessage(t *testing.T, sender *Switch, recvPub p2pcrypto.PublicKey, inChan chan service.DirectMessage, checkpayload bool) {
	payload := []byte(RandString(10))
	err := sender.SendMessage(context.TODO(), recvPub, exampleProtocol, payload)
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
	p1 := p2pTestNoStart(t, configWithPort(0))
	p2 := p2pTestNoStart(t, configWithPort(0))

	exchan1 := p1.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan1, p1.directProtocolHandlers[exampleProtocol])
	exchan2 := p2.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan2, p2.directProtocolHandlers[exampleProtocol])

	require.NoError(t, p1.Start(context.TODO()))
	require.NoError(t, p2.Start(context.TODO()))

	_, err := p2.cPool.GetConnection(context.TODO(), p1.network.LocalAddr(), p1.lNode.PublicKey())
	require.NoError(t, err)

	sendDirectMessage(t, p2, p1.lNode.PublicKey(), exchan1, true)
	sendDirectMessage(t, p1, p2.lNode.PublicKey(), exchan2, true)

	p1.Shutdown()
	p2.Shutdown()
}

func TestSwarm_MultipleMessages(t *testing.T) {
	p1 := p2pTestNoStart(t, configWithPort(0))
	p2 := p2pTestNoStart(t, configWithPort(0))

	exchan1 := p1.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan1, p1.directProtocolHandlers[exampleProtocol])
	exchan2 := p2.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan2, p2.directProtocolHandlers[exampleProtocol])

	require.NoError(t, p1.Start(context.TODO()))
	require.NoError(t, p2.Start(context.TODO()))

	err := p2.SendMessage(context.TODO(), p1.lNode.PublicKey(), exampleProtocol, []byte(examplePayload))
	require.Error(t, err, "ERR") // should'nt be in routing table

	_, err = p2.cPool.GetConnection(context.TODO(), p1.network.LocalAddr(), p1.lNode.PublicKey())
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
	s   []*Switch
	mtx sync.Mutex
}

func (sa *swarmArray) add(s *Switch) {
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

	cfg := configWithPort(0)
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.Bootstrap = false

	p1 := p2pTestNoStart(t, cfg)

	exchan1 := p1.RegisterDirectProtocol(exampleProtocol)
	assert.Equal(t, exchan1, p1.directProtocolHandlers[exampleProtocol])

	p1.Start(context.TODO())

	pend := make(map[string]chan error)
	var mu sync.Mutex

	go func() {
		for {
			msg := <-exchan1
			sender := msg.Sender().String()
			mu.Lock()
			c, ok := pend[sender]
			if !ok {
				c <- errors.New("not found")
				mu.Unlock()
				return
			}
			close(c)
			mu.Unlock()
		}
	}()

	sa := &swarmArray{}
	for i := 0; i < Senders; i++ {
		p := p2pTestNoStart(t, cfg)
		require.NoError(t, p.Start(context.TODO()))
		sa.add(p)
		_, err := p.cPool.GetConnection(context.TODO(), p1.network.LocalAddr(), p1.lNode.PublicKey())
		require.NoError(t, err)
		mychan := make(chan error)
		mu.Lock()
		pend[p.lNode.PublicKey().String()] = mychan
		mu.Unlock()
		payload := []byte(RandString(10))
		err = p.SendMessage(context.TODO(), p1.lNode.PublicKey(), exampleProtocol, payload)
		require.NoError(t, err)
	}

	for _, c := range pend {
		res := <-c
		require.NoError(t, res)
	}
	p1.Shutdown()
	sa.clean()
}

func TestSwarm_MultipleMessagesFromMultipleSendersToMultipleProtocols(t *testing.T) {

	const Senders = 100
	const Protos = 50

	cfg := configWithPort(0)
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.Bootstrap = false

	pend := make(map[string]chan error)
	var mu sync.Mutex

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
					c <- errors.New("not found")
					close(c)
					mu.Unlock()
				}
				close(c)
				mu.Unlock()
			}
		}()

	}

	require.NoError(t, p1.Start(context.TODO()))

	sa := &swarmArray{}
	for i := 0; i < Senders; i++ {
		p := p2pTestNoStart(t, cfg)
		require.NoError(t, p.Start(context.TODO()))
		sa.add(p)
		mychan := make(chan error)
		mu.Lock()
		pend[p.lNode.PublicKey().String()] = mychan
		mu.Unlock()
		randProto := rand.Int31n(Protos)
		if randProto == Protos {
			randProto--
		}

		payload := []byte(RandString(10))
		_, err := p.cPool.GetConnection(context.TODO(), p1.network.LocalAddr(), p1.lNode.PublicKey())
		require.NoError(t, err)
		err = p.SendMessage(context.TODO(), p1.lNode.PublicKey(), protos[randProto], payload)
		require.NoError(t, err)
	}

	for _, c := range pend {
		res := <-c
		require.NoError(t, res)
	}
	p1.Shutdown()
	sa.clean()
}

func TestSwarm_RegisterProtocol(t *testing.T) {
	const numPeers = 100
	nods := make([]*Switch, 0, numPeers)
	cfg := configWithPort(0)
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
	id, err := node.NewNodeIdentity()
	assert.NoError(t, err, "we cant make node ?")

	p := p2pTestNoStart(t, configWithPort(0))
	nmock := new(net.ConnectionMock)
	nmock.SetRemotePublicKey(id.PublicKey())

	// Test bad format
	ime := net.IncomingMessageEvent{Conn: nmock}
	err = p.onRemoteClientMessage(context.TODO(), ime)
	assert.Equal(t, ErrBadFormat1, err)

	// Test No Session
	ime.Message = []byte("test")

	err = p.onRemoteClientMessage(context.TODO(), ime)
	assert.Equal(t, ErrNoSession, err)

	//Test bad session
	session := &net.SessionMock{}
	session.OpenMessageFunc = func(boxedMessage []byte) (bytes []byte, err error) {
		return nil, errors.New("fail")
	}
	ime.Conn.SetSession(session)

	err = p.onRemoteClientMessage(context.TODO(), ime)
	assert.Equal(t, ErrFailDecrypt, err)

	//// Test bad format again
	session.OpenMessageFunc = func(boxedMessage []byte) (bytes []byte, err error) {
		return []byte("wont_format_fo_protocol_message"), nil
	}

	err = p.onRemoteClientMessage(context.TODO(), ime)
	assert.Equal(t, ErrBadFormat2, err)

	goodmsg := &ProtocolMessage{
		Metadata: &ProtocolMessageMetadata{AuthPubkey: id.PublicKey().Bytes(), NextProtocol: exampleProtocol,
			Timestamp: time.Now().Unix(), ClientVersion: config.ClientVersion}, // not signed
		Payload: &Payload{Payload: []byte(examplePayload)},
	}

	goodbin, _ := types.InterfaceToBytes(goodmsg)

	ime.Message = goodbin
	session.OpenMessageFunc = func(boxedMessage []byte) (bytes []byte, err error) {
		return goodbin, nil
	}

	goodmsg.Metadata.Timestamp = time.Now().Add(-time.Hour).Unix()
	nosynced, _ := types.InterfaceToBytes(goodmsg)

	session.OpenMessageFunc = func(boxedMessage []byte) (bytes []byte, err error) {
		return nosynced, nil
	}

	// Test out of sync
	ime.Message = nosynced

	err = p.onRemoteClientMessage(context.TODO(), ime)
	assert.Equal(t, ErrOutOfSync, err)

	// Test no protocol
	goodmsg.Metadata.Timestamp = time.Now().Unix()

	goodbin, _ = types.InterfaceToBytes(goodmsg)
	ime.Message = goodbin
	session.OpenMessageFunc = func(boxedMessage []byte) (bytes []byte, err error) {
		return goodbin, nil
	}

	err = p.onRemoteClientMessage(context.TODO(), ime)
	assert.Equal(t, ErrNoProtocol, err)

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
	err = p.onRemoteClientMessage(context.TODO(), ime)
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
	cfg := configWithPort(0)
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	res := n.getMorePeers(context.TODO(), 0) // this should'nt work
	assert.Equal(t, res, 0)
	assertNoNewPeerEvent(t, conn)
}

func Test_Swarm_getMorePeers2(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := configWithPort(0)
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	mdht := new(discovery.MockPeerStore)
	n.discover = mdht
	// this will return 0 peers because SelectPeers returns empty array when not set

	res := n.getMorePeers(context.TODO(), 10)
	assert.Equal(t, res, 0)
	assertNoNewPeerEvent(t, conn)
}

func Test_Swarm_getMorePeers3(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := configWithPort(0)
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	mdht := new(discovery.MockPeerStore)
	n.discover = mdht
	testNode := node.GenerateRandomNodeData()
	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		return []*node.Info{testNode}
	}

	cpm := new(cpoolMock)

	// test connection error
	cpm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error) {
		return nil, errors.New("can't make connection")
	}

	n.cPool = cpm
	res := n.getMorePeers(context.TODO(), 1) // this should'nt work
	assert.Equal(t, res, 0)
	assertNoNewPeerEvent(t, conn)
}

func Test_Swarm_getMorePeers4(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := configWithPort(0)
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.RandomConnections = numpeers
	n := p2pTestNoStart(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	mdht := new(discovery.MockPeerStore)
	n.discover = mdht

	testNode := node.GenerateRandomNodeData()
	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		return []*node.Info{testNode}
	}

	cpm := new(cpoolMock)

	n.cPool = cpm

	res := n.getMorePeers(context.TODO(), 1)
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
	cfg := configWithPort(0)
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

	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		return node.GenerateRandomNodesData(qty)
	}

	res := n.getMorePeers(context.TODO(), numpeers)
	assert.Equal(t, res, numpeers)
	assert.Equal(t, len(n.outpeers), numpeers) // there's already one inside
	assertNewPeerEvents(t, numpeers, conn)
	drainPeerEvents(conn) // so they wont interrupt next test
}

func Test_Swarm_getMorePeers6(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := configWithPort(0)
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

	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		return node.GenerateRandomNodesData(qty)
	}

	//test inc peer
	nd := node.GenerateRandomNodeData()
	require.NoError(t, n.addIncomingPeer(nd.PublicKey()))

	assert.True(t, n.hasIncomingPeer(nd.PublicKey()))
	assertNewPeerEvents(t, 1, conn)
	assertNewPeerEvent(t, nd.PublicKey(), conn)

	//test not replacing inc peer
	//
	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		some := node.GenerateRandomNodesData(qty - 1)
		some = append(some, nd)
		return some
	}

	res := n.getMorePeers(context.TODO(), numpeers)
	assert.Equal(t, res, numpeers-1)
	assert.False(t, n.hasOutgoingPeer(nd.PublicKey()))
	assert.True(t, n.hasIncomingPeer(nd.PublicKey()))
}

func Test_Swarm_callCpoolCloseCon(t *testing.T) {
	cfg := configWithPort(0)
	cfg.MaxInboundPeers = 0
	p1 := p2pTestNoStart(t, cfg)
	p2 := p2pTestNoStart(t, cfg)
	exchan1 := p1.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan1, p1.directProtocolHandlers[exampleProtocol])
	exchan2 := p2.RegisterDirectProtocol(exampleProtocol)
	require.Equal(t, exchan2, p2.directProtocolHandlers[exampleProtocol])

	require.NoError(t, p1.Start(context.TODO()))
	require.NoError(t, p2.Start(context.TODO()))

	cpm := newCpoolMock()
	p1.cPool = cpm

	wg := sync.WaitGroup{}
	wg.Add(2)

	_, err := p2.cPool.GetConnection(context.TODO(), p1.network.LocalAddr(), p1.lNode.PublicKey())
	require.NoError(t, err)

	_, err = p1.cPool.GetConnection(context.TODO(), p2.network.LocalAddr(), p2.lNode.PublicKey())
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
	cfg := configWithPort(0)
	cfg.SwarmConfig.RandomConnections = 3
	cfg.SwarmConfig.Gossip = true
	cfg.SwarmConfig.Bootstrap = false

	p := p2pTestNoStart(t, cfg)
	mdht := new(discovery.MockPeerStore)
	mdht.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		return node.GenerateRandomNodesData(qty)
	}

	p.discover = mdht

	err := p.Start(context.TODO())
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
	cpm := newCpoolMock()
	cpm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (net.Connection, error) {
		return net.NewConnectionMock(pk), nil
	}
	p.cPool = cpm

	err = p.Start(context.TODO())
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
	n := p2pTestNoStart(t, configWithPort(0))
	_, disc := n.SubscribePeerEvents()
	rnd := node.GenerateRandomNodeData()
	require.NoError(t, n.addIncomingPeer(rnd.PublicKey()))

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
	cfg := configWithPort(0)
	p := p2pTestInstance(t, cfg)
	rnd := node.GenerateRandomNodeData()
	require.NoError(t, p.addIncomingPeer(rnd.PublicKey()))

	p.inpeersMutex.RLock()
	_, ok := p.inpeers[rnd.PublicKey()]
	p.inpeersMutex.RUnlock()

	assert.True(t, ok)

	nds := node.GenerateRandomNodesData(cfg.MaxInboundPeers)
	for i := 0; i < len(nds)-1; i++ {
		err := p.addIncomingPeer(nds[i].PublicKey())
		require.NoError(t, err)
	}

	err := p.addIncomingPeer(node.GenerateRandomNodeData().PublicKey())
	require.Error(t, err)

	require.Equal(t, len(p.inpeers), cfg.MaxInboundPeers)
	p.inpeersMutex.RLock()
	_, ok = p.inpeers[nds[len(nds)-1].PublicKey()]
	p.inpeersMutex.RUnlock()
	assert.False(t, ok)
}

func TestSwarm_AskPeersSerial(t *testing.T) {
	p := p2pTestNoStart(t, configWithPort(0))
	dsc := &discovery.MockPeerStore{}

	timescalled := uint32(0)
	block := make(chan struct{})

	dsc.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		atomic.AddUint32(&timescalled, 1)
		<-block
		return node.GenerateRandomNodesData(qty) // will trigger sending on morepeersreq
	}

	p.discover = dsc

	require.NoError(t, p.startNeighborhood(context.TODO()))

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
	pinged2 := &node.Info{}
	err = types.BytesToInterface(raw, pinged2)
	if err != nil {
		panic(err)
	}
}

func TestNeighborhood_ReportConnectionResult(t *testing.T) {
	const PeerNum = 10
	n := p2pTestNoStart(t, configWithPort(0))
	var goodcount uint32 = 0
	var attemptcount uint32 = 0
	var getConnCount uint32 = 0

	resetCounters := func() {
		goodcount = 0
		attemptcount = 0
		getConnCount = 0
	}

	atomicIncOne := func(u32 *uint32) {
		atomic.AddUint32(u32, 1)
	}

	ps := &discovery.MockPeerStore{}

	ps.GoodFunc = func(key p2pcrypto.PublicKey) {
		atomicIncOne(&goodcount)
	}

	ps.AttemptFunc = func(key p2pcrypto.PublicKey) {
		atomicIncOne(&attemptcount)
	}

	rnds := node.GenerateRandomNodesData(PeerNum)

	for r := range rnds {
		if rnds[r].PublicKey() == n.LocalNode().PublicKey() {
			rnds[r] = node.GenerateRandomNodeData()
		}
	}

	ps.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		return rnds
	}

	n.discover = ps

	cm := &cpoolMock{}

	cm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (connection net.Connection, e error) {
		atomicIncOne(&getConnCount)
		return nil, errors.New("coudln't create connection")
	}

	n.cPool = cm

	got := n.getMorePeers(context.TODO(), PeerNum)
	require.Equal(t, 0, got, "getMorePeers result mismatch")
	require.Equal(t, uint32(PeerNum), attemptcount, "attempt count mismatch")
	require.Equal(t, uint32(PeerNum), getConnCount, "GetConnection count mismatch")

	resetCounters()

	goodlist := sync.Map{}

	cm.f = func(address inet.Addr, pk p2pcrypto.PublicKey) (connection net.Connection, e error) {
		if _, ok := goodlist.Load(pk); ok {
			return net.NewConnectionMock(pk), nil
		}
		return nil, errors.New("not found")
	}

	realnode := p2pTestNoStart(t, configWithPort(0))
	realnode2 := p2pTestNoStart(t, configWithPort(0))
	realnodeinfo := &node.Info{ID: realnode.lNode.PublicKey().Array(), IP: inet.IPv4zero}
	realnode2info := &node.Info{ID: realnode2.lNode.PublicKey().Array(), IP: inet.IPv4zero}

	goodlist.Store(realnode.lNode.PublicKey(), struct{}{})
	goodlist.Store(realnode2.lNode.PublicKey(), struct{}{})

	newrnds := node.GenerateRandomNodesData(PeerNum - 2)

	ps.SelectPeersFunc = func(ctx context.Context, qty int) []*node.Info {
		return append([]*node.Info{realnodeinfo, realnode2info}, newrnds...)
	}

	got = n.getMorePeers(context.TODO(), PeerNum)

	require.Equal(t, 2, got)
	require.Equal(t, uint32(2), goodcount)
	require.Equal(t, uint32(PeerNum), attemptcount)
}

func TestSwarm_SendMessage(t *testing.T) {
	p := p2pTestNoStart(t, configWithPort(0))

	cp := &cpoolMock{}
	ps := &discovery.MockPeerStore{}

	p.cPool = cp
	p.discover = ps

	ps.IsLocalAddressFunc = func(info *node.Info) bool {
		return true
	}

	someky := p2pcrypto.NewRandomPubkey()
	proto := exampleDirectProto
	//payload := service.DataBytes{Payload:[]byte("LOL")}

	err := p.SendMessage(context.TODO(), someky, proto, []byte("LOL"))
	require.Equal(t, err, errors.New("can't send message to self"))

	ps.IsLocalAddressFunc = func(info *node.Info) bool {
		return false
	}

	cp.fExists = func(pk p2pcrypto.PublicKey) (connection net.Connection, err error) {
		return nil, errors.New("no conn")
	}

	err = p.SendMessage(context.TODO(), someky, proto, []byte("LOL"))
	require.Equal(t, errors.New("peer not a neighbor or connection lost: no conn"), err)

	cp.fExists = func(pk p2pcrypto.PublicKey) (connection net.Connection, err error) {
		return net.NewConnectionMock(pk), nil
	}

	err = p.SendMessage(context.TODO(), someky, proto, []byte("LOL"))
	require.Equal(t, err, ErrNoSession)

	c := net.NewConnectionMock(someky)
	session := net.NewSessionMock(someky)
	c.SetSession(session)

	cp.fExists = func(pk p2pcrypto.PublicKey) (connection net.Connection, err error) {
		return c, nil
	}

	err = p.SendMessage(context.TODO(), someky, proto, nil)
	require.Equal(t, err, errors.New("unable to send empty payload"))

	session.SealMessageFunc = func(message []byte) []byte {
		return nil
	}

	err = p.SendMessage(context.TODO(), someky, proto, []byte("LOL"))
	require.Equal(t, err, errors.New("encryption failed"))

	session.SealMessageFunc = func(message []byte) []byte {
		return message
	}

	c.SetSendResult(errors.New("fail"))

	err = p.SendMessage(context.TODO(), someky, proto, []byte("LOL"))
	require.Equal(t, err, errors.New("fail"))

	c.SetSendResult(nil)

	err = p.SendMessage(context.TODO(), someky, proto, []byte("LOL"))
	require.Equal(t, err, nil)
}

type tcpListenerMock struct {
	port int
}

func (t tcpListenerMock) Accept() (inet.Conn, error) { panic("not mocked") }
func (t tcpListenerMock) Close() error               { return nil } // TODO: assert closed
func (t tcpListenerMock) Addr() inet.Addr            { return &inet.TCPAddr{Port: t.port} }

type tcpResponse struct {
	listener inet.Listener
	err      error
}

type udpResponse struct {
	err error
}

var ErrPortUnavailable = fmt.Errorf("failed to acquire port")

func TestSwarm_getListeners_randomPort(t *testing.T) {
	r := require.New(t)

	port := 0
	tcpResponses := map[int][]tcpResponse{
		0: {
			tcpResponse{listener: tcpListenerMock{port: 1234}},
			tcpResponse{listener: tcpListenerMock{port: 1337}},
		},
	}
	udpResponses := map[int][]udpResponse{
		1234: {udpResponse{err: ErrPortUnavailable}},
		1337: {udpResponse{}},
	}

	r.NoError(testGetListenersScenario(t, port, tcpResponses, udpResponses, createDiscoverUpnpFunc(nil, 1337, nil), true))

	// UPnP first attempt failure and second attempt success
	tcpResponses = map[int][]tcpResponse{
		0: {
			tcpResponse{listener: tcpListenerMock{port: 1234}},
			tcpResponse{listener: tcpListenerMock{port: 1337}},
		},
	}
	udpResponses = map[int][]udpResponse{
		1234: {udpResponse{}},
		1337: {udpResponse{}},
	}
	f := func() (igd nattraversal.UPNPGateway, err error) {
		return &UpnpGatewayMock{errs: map[uint16][]error{1234: {ErrPortUnavailable}, 1337: {nil}}}, nil
	}

	r.NoError(testGetListenersScenario(t, port, tcpResponses, udpResponses, f, true))
}

func TestSwarm_getListeners_specificPort(t *testing.T) {
	r := require.New(t)

	port := 1337
	tcpResponses := map[int][]tcpResponse{
		1337: {tcpResponse{listener: tcpListenerMock{port: 1337}}},
	}
	udpResponses := map[int][]udpResponse{
		1337: {udpResponse{}},
	}

	r.NoError(testGetListenersScenario(t, port, tcpResponses, udpResponses, createDiscoverUpnpFunc(nil, 1337, nil), true))
}

func TestSwarm_getListeners_specificPortUnavailable(t *testing.T) {
	r := require.New(t)
	port := 1337

	// Specific TCP port unavailable
	tcpResponses := map[int][]tcpResponse{
		1337: {tcpResponse{err: ErrPortUnavailable}},
	}
	udpResponses := map[int][]udpResponse{}

	r.EqualError(testGetListenersScenario(t, port, tcpResponses, udpResponses, createDiscoverUpnpFunc(nil, 1337, nil), true),
		"failed to acquire requested tcp port: failed to acquire port")

	// Specific UDP port unavailable
	tcpResponses = map[int][]tcpResponse{
		1337: {tcpResponse{listener: tcpListenerMock{port: 1337}}},
	}
	udpResponses = map[int][]udpResponse{
		1337: {udpResponse{err: ErrPortUnavailable}},
	}

	r.EqualError(testGetListenersScenario(t, port, tcpResponses, udpResponses, createDiscoverUpnpFunc(nil, 1337, nil), true),
		"failed to acquire requested udp port: failed to acquire port")

	// Specific port unavailable on UPnP
	tcpResponses = map[int][]tcpResponse{
		1337: {tcpResponse{listener: tcpListenerMock{port: 1337}}},
	}
	udpResponses = map[int][]udpResponse{
		1337: {udpResponse{}},
	}

	r.NoError(testGetListenersScenario(t, port, tcpResponses, udpResponses, createDiscoverUpnpFunc(nil, 1337, ErrPortUnavailable), true))
}

func TestSwarm_getListeners_upnpMoreCases(t *testing.T) {
	r := require.New(t)
	port := 1337

	// Specific port unavailable on UPnP, but acquirePort is false
	tcpResponses := map[int][]tcpResponse{
		1337: {tcpResponse{listener: tcpListenerMock{port: 1337}}},
	}
	udpResponses := map[int][]udpResponse{
		1337: {udpResponse{}},
	}

	r.NoError(testGetListenersScenario(t, port, tcpResponses, udpResponses, createDiscoverUpnpFunc(nil, 1337, ErrPortUnavailable), false))

	// Specific port, UPnP connection error
	tcpResponses = map[int][]tcpResponse{
		1337: {tcpResponse{listener: tcpListenerMock{port: 1337}}},
	}
	udpResponses = map[int][]udpResponse{
		1337: {udpResponse{}},
	}

	r.NoError(testGetListenersScenario(t, port, tcpResponses, udpResponses, createDiscoverUpnpFunc(ErrPortUnavailable, 1337, ErrPortUnavailable), true))
}

type UDPConnMock struct{}

func (UDPConnMock) LocalAddr() inet.Addr                               { panic("implement me") }
func (UDPConnMock) Close() error                                       { return nil }
func (UDPConnMock) WriteToUDP([]byte, *inet.UDPAddr) (int, error)      { panic("implement me") }
func (UDPConnMock) ReadFrom([]byte) (n int, addr inet.Addr, err error) { panic("implement me") }
func (UDPConnMock) WriteTo([]byte, inet.Addr) (n int, err error)       { panic("implement me") }
func (UDPConnMock) SetDeadline(time.Time) error                        { panic("implement me") }
func (UDPConnMock) SetReadDeadline(time.Time) error                    { panic("implement me") }
func (UDPConnMock) SetWriteDeadline(time.Time) error                   { panic("implement me") }

func testGetListenersScenario(
	t *testing.T,
	port int,
	tcpResponses map[int][]tcpResponse,
	udpResponses map[int][]udpResponse,
	discoverUpnp func() (igd nattraversal.UPNPGateway, err error),
	acquirePort bool,
) error {

	r := require.New(t)

	cfg := configWithPort(port)
	cfg.AcquirePort = acquirePort
	swarm := p2pTestNoStart(t, cfg)

	getTCP := func(addr *inet.TCPAddr) (listener inet.Listener, err error) {
		port := addr.Port
		r.NotEmpty(tcpResponses[port], "no response for port %v", port)
		res := tcpResponses[port][0]
		tcpResponses[port] = tcpResponses[port][1:]
		return res.listener, res.err
	}
	getUDP := func(addr *inet.UDPAddr) (conn net.UDPListener, err error) {
		port := addr.Port
		r.NotEmpty(udpResponses[port], "no response for port %v", port)
		res := udpResponses[port][0]
		udpResponses[port] = udpResponses[port][1:]

		return &UDPConnMock{}, res.err
	}
	_, _, err := swarm.getListeners(getTCP, getUDP, discoverUpnp)

	for port, responses := range tcpResponses {
		r.Empty(responses, "not all responses for tcp port %v were consumed", port)
	}
	for port, responses := range udpResponses {
		r.Empty(responses, "not all responses for udp port %v were consumed", port)
	}

	if swarm.releaseUpnp != nil {
		swarm.releaseUpnp()
	}

	return err
}

type UpnpGatewayMock struct {
	errs map[uint16][]error
}

func (u *UpnpGatewayMock) Forward(port uint16, desc string) error {
	if len(u.errs[port]) == 0 {
		panic(fmt.Sprintf("not enough values to return from UpnpGatewayMock for port %d", port))
	}
	err := u.errs[port][0]
	u.errs[port] = u.errs[port][1:]
	return err
}

func (u *UpnpGatewayMock) Clear(port uint16) error {
	if responses, ok := u.errs[port]; !ok {
		panic("closing unexpected port")
	} else if len(responses) != 0 {
		panic("unused responses")
	}
	delete(u.errs, port)
	return nil
}

func createDiscoverUpnpFunc(funcErr error, port uint16, gatewayForwardErr error) func() (igd nattraversal.UPNPGateway, err error) {
	return func() (igd nattraversal.UPNPGateway, err error) {
		if funcErr != nil {
			return nil, funcErr
		}
		return &UpnpGatewayMock{errs: map[uint16][]error{port: {gatewayForwardErr}}}, nil
	}
}
