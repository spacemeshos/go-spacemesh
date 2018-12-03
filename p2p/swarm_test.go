package p2p

import (
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"testing"
	"time"

	"context"
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/message"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"sync"
	"fmt"
)

type cpoolMock struct {
	f func(address string, pk crypto.PublicKey) (net.Connection, error)
}

func (cp *cpoolMock) GetConnection(address string, pk crypto.PublicKey) (net.Connection, error) {
	if cp.f != nil {
		return cp.f(address, pk)
	}
	return net.NewConnectionMock(pk), nil
}

func p2pTestInstance(t testing.TB, config config.Config) *swarm {
	port, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Error getting a port", err)
	config.TCPPort = port
	p, err := newSwarm(context.TODO(), config, true, true)
	assert.NoError(t, err, "Error creating p2p stack, err: %v", err)
	assert.NotNil(t, p)
	p.Start()
	return p
}

func p2pTestNoStart(t testing.TB, config config.Config) *swarm {
	port, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Error getting a port", err)
	config.TCPPort = port
	p, err := newSwarm(context.TODO(), config, true, true)
	assert.NoError(t, err, "Error creating p2p stack, err: %v", err)
	assert.NotNil(t, p)
	return p
}

const exampleProtocol = "EX"
const examplePayload = "Example"

func TestNew(t *testing.T) {
	s, err := New(context.TODO(), config.DefaultConfig())
	assert.NoError(t, err, err)
	err = s.Start()
	assert.NoError(t, err, err)
	assert.NotNil(t, s, "its nil")
	s.Shutdown()
}

func Test_newSwarm(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TCPPort = int(crypto.GetRandomUserPort())
	s, err := newSwarm(context.TODO(), cfg, true, false)
	assert.NoError(t, err)
	err = s.Start()
	assert.NoError(t, err, err)
	assert.NotNil(t, s)
	s.Shutdown()
}

func TestSwarm_Shutdown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TCPPort = int(crypto.GetRandomUserPort())
	s, err := newSwarm(context.TODO(), cfg, true, false)
	assert.NoError(t, err)
	err = s.Start()
	assert.NoError(t, err, err)
	s.Shutdown()

	select {
	case _, ok := <-s.shutdown:
		assert.False(t, ok)
	case <-time.After(1 * time.Second):
		t.Error("Failed to shutdown")
	}
}

func TestSwarm_ShutdownNoStart(t *testing.T) {
	s, err := newSwarm(context.TODO(), config.DefaultConfig(), true, false)
	assert.NoError(t, err)
	s.Shutdown()
}

func TestSwarm_RegisterProtocolNoStart(t *testing.T) {
	s, err := newSwarm(context.TODO(), config.DefaultConfig(), true, false)
	msgs := s.RegisterProtocol("Anton")
	assert.NotNil(t, msgs)
	assert.NoError(t, err)
	s.Shutdown()
}

func TestSwarm_processMessage(t *testing.T) {
	s := swarm{}
	s.lNode, _ = node.GenerateTestNode(t)
	r := node.GenerateRandomNodeData()
	c := &net.ConnectionMock{}
	c.SetRemotePublicKey(r.PublicKey())
	ime := net.IncomingMessageEvent{Message: []byte("0"), Conn: c}
	s.processMessage(ime) // should error

	assert.True(t, c.Closed())
}

func TestSwarm_authAuthor(t *testing.T) {
	// create a message

	priv, pub, err := crypto.GenerateKeyPair()

	assert.NoError(t, err, err)
	assert.NotNil(t, priv)
	assert.NotNil(t, pub)

	pm := &pb.ProtocolMessage{
		Metadata: message.NewProtocolMessageMetadata(pub, exampleProtocol, false),
		Data:     &pb.ProtocolMessage_Payload{Payload: []byte(examplePayload)},
	}
	ppm, err := proto.Marshal(pm)
	assert.NoError(t, err, "cant marshal msg ", err)

	// sign it
	s, err := priv.Sign(ppm)
	assert.NoError(t, err, "cant sign ", err)
	ssign := hex.EncodeToString(s)

	pm.Metadata.AuthorSign = ssign

	vererr := message.AuthAuthor(pm)
	assert.NoError(t, vererr)

	priv2, pub2, err := crypto.GenerateKeyPair()

	assert.NoError(t, err, err)
	assert.NotNil(t, priv2)
	assert.NotNil(t, pub2)

	s, err = priv2.Sign(ppm)
	assert.NoError(t, err, "cant sign ", err)
	ssign = hex.EncodeToString(s)

	pm.Metadata.AuthorSign = ssign

	vererr = message.AuthAuthor(pm)
	assert.Error(t, vererr)
}

func TestSwarm_SignAuth(t *testing.T) {
	n, _ := node.GenerateTestNode(t)
	pm := &pb.ProtocolMessage{
		Metadata: message.NewProtocolMessageMetadata(n.PublicKey(), exampleProtocol, false),
		Data:     &pb.ProtocolMessage_Payload{Payload: []byte(examplePayload)},
	}

	err := message.SignMessage(n.PrivateKey(), pm)
	assert.NoError(t, err)

	err = message.AuthAuthor(pm)

	assert.NoError(t, err)
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func sendDirectMessage(t *testing.T, sender *swarm, recvPub string, inChan chan service.Message, checkpayload bool) {
	payload := []byte(RandString(10))
	err := sender.SendMessage(recvPub, exampleProtocol, payload)
	assert.NoError(t, err)
	select {
	case msg := <-inChan:
		if checkpayload {
			assert.Equal(t, msg.Bytes(), payload)
		}
		assert.Equal(t, msg.Sender().String(), sender.lNode.String())
		break
	case <-time.After(2 * time.Second):
		t.Error("Took too much time to recieve")
	}
}

func TestSwarm_RoundTrip(t *testing.T) {
	p1 := p2pTestInstance(t, config.DefaultConfig())
	p2 := p2pTestInstance(t, config.DefaultConfig())

	exchan1 := p1.RegisterProtocol(exampleProtocol)
	assert.Equal(t, exchan1, p1.protocolHandlers[exampleProtocol])
	exchan2 := p2.RegisterProtocol(exampleProtocol)
	assert.Equal(t, exchan2, p2.protocolHandlers[exampleProtocol])

	p2.dht.Update(p1.lNode.Node)

	sendDirectMessage(t, p2, p1.lNode.PublicKey().String(), exchan1, true)
	sendDirectMessage(t, p1, p2.lNode.PublicKey().String(), exchan2, true)
}

func TestSwarm_MultipleMessages(t *testing.T) {
	p1 := p2pTestInstance(t, config.DefaultConfig())
	p2 := p2pTestInstance(t, config.DefaultConfig())

	exchan1 := p1.RegisterProtocol(exampleProtocol)
	assert.Equal(t, exchan1, p1.protocolHandlers[exampleProtocol])
	exchan2 := p2.RegisterProtocol(exampleProtocol)
	assert.Equal(t, exchan2, p2.protocolHandlers[exampleProtocol])

	err := p2.SendMessage(p1.lNode.String(), exampleProtocol, []byte(examplePayload))
	assert.Error(t, err, "ERR") // should'nt be in routing table
	p2.dht.Update(p1.lNode.Node)

	var wg sync.WaitGroup
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() { sendDirectMessage(t, p2, p1.lNode.String(), exchan1, false); wg.Done() }()
	}
	wg.Wait()
}

func TestSwarm_MultipleMessagesFromMultipleSenders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.Bootstrap = false

	p1 := p2pTestInstance(t, cfg)

	exchan1 := p1.RegisterProtocol(exampleProtocol)
	assert.Equal(t, exchan1, p1.protocolHandlers[exampleProtocol])

	pend := make(map[string]chan struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	go func() {
		for {
			msg := <-exchan1
			sender := msg.Sender().PublicKey().String()
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

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			p := p2pTestInstance(t, cfg)
			p.dht.Update(p1.LocalNode().Node)
			mychan := make(chan struct{})
			mu.Lock()
			pend[p.lNode.Node.PublicKey().String()] = mychan
			mu.Unlock()

			payload := []byte(RandString(10))
			err := p.SendMessage(p1.lNode.PublicKey().String(), exampleProtocol, payload)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
}

func TestSwarm_RegisterProtocol(t *testing.T) {
	const numPeers = 100
	nodechan := make(chan *swarm)
	cfg := config.DefaultConfig()
	for i := 0; i < numPeers; i++ {
		go func() { // protocols are registered before starting the node and read after that.
			// there ins't an actual need to sync them.
			nod := p2pTestInstance(t, cfg)
			nod.RegisterProtocol(exampleProtocol) // this is example
			nodechan <- nod
		}()
	}
	i := 0
	for r := range nodechan {
		_, ok := r.protocolHandlers[exampleProtocol]
		assert.True(t, ok)
		_, ok = r.protocolHandlers["/dht/1.0/find-node/"]
		assert.True(t, ok)
		i++
		if i == numPeers {
			close(nodechan)
		}
	}
}

func TestSwarm_onRemoteClientMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	id, err := node.NewNodeIdentity(cfg, "0.0.0.0:0000", false)
	assert.NoError(t, err, "we cant make node ?")

	p := p2pTestInstance(t, config.DefaultConfig())
	nmock := &net.ConnectionMock{}
	nmock.SetRemotePublicKey(id.PublicKey())

	// Test bad format

	msg := []byte("badbadformat")
	imc := net.IncomingMessageEvent{nmock, msg}
	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrBadFormat1)

	// Test out of sync

	realmsg := &pb.CommonMessageData{
		SessionId: []byte("test"),
		Payload:   []byte("test"),
		Timestamp: time.Now().Add(timesync.MaxAllowedMessageDrift + time.Minute).Unix(),
	}
	bin, _ := proto.Marshal(realmsg)

	imc.Message = bin

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrOutOfSync)

	// Test no payload

	cmd := &pb.CommonMessageData{
		SessionId: []byte("test"),
		Payload:   []byte(""),
		Timestamp: time.Now().Unix(),
	}

	bin, _ = proto.Marshal(cmd)
	imc.Message = bin
	err = p.onRemoteClientMessage(imc)

	assert.Equal(t, err, ErrNoPayload)

	// Test No Session

	cmd.Payload = []byte("test")

	bin, _ = proto.Marshal(cmd)
	imc.Message = bin

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrNoSession)

	// Test bad session

	session := &net.SessionMock{}
	session.SetDecrypt(nil, errors.New("fail"))
	imc.Conn.SetSession(session)

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrFailDecrypt)

	// Test bad format again

	session.SetDecrypt([]byte("wont_format_fo_protocol_message"), nil)

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrBadFormat2)

	// Test bad auth sign

	goodmsg := &pb.ProtocolMessage{
		Metadata: message.NewProtocolMessageMetadata(id.PublicKey(), exampleProtocol, false), // not signed
		Data:     &pb.ProtocolMessage_Payload{Payload: []byte(examplePayload)},
	}

	goodbin, _ := proto.Marshal(goodmsg)

	cmd.Payload = goodbin
	bin, _ = proto.Marshal(cmd)
	imc.Message = bin
	session.SetDecrypt(goodbin, nil)

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrAuthAuthor)

	// Test no server

	err = message.SignMessage(id.PrivateKey(), goodmsg)
	assert.NoError(t, err, err)

	goodbin, _ = proto.Marshal(goodmsg)
	cmd.Payload = goodbin
	bin, _ = proto.Marshal(cmd)
	imc.Message = bin
	session.SetDecrypt(goodbin, nil)

	err = p.onRemoteClientMessage(imc)
	assert.Equal(t, err, ErrNoProtocol)

	// Test no err

	c := p.RegisterProtocol(exampleProtocol)
	go func() { <-c }()

	err = p.onRemoteClientMessage(imc)

	assert.NoError(t, err)

	// todo : test gossip codepaths.
}

func assertNewPeerEvent(t *testing.T, peer crypto.PublicKey, connChan <-chan crypto.PublicKey) {
	select {
	case newPeer := <-connChan:
		assert.Equal(t, peer.String(), newPeer.String())
	default:
		assert.Error(t, errors.New("no new peer event"))
	}
}

func assertNewPeerEvents(t *testing.T, expCount int, connChan <-chan crypto.PublicKey) {
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

func assertNoNewPeerEvent(t *testing.T, eventChan <-chan crypto.PublicKey) {
	select {
	case newPeer := <-eventChan:
		assert.Error(t, errors.New("unexpected new peer event, peer " + newPeer.String()))
	default:
		return
	}
}

func assertNewDisconnectedPeerEvent(t *testing.T, peer crypto.PublicKey, discChan <-chan crypto.PublicKey) {
	select {
	case newPeer := <-discChan:
		assert.Equal(t, peer.String(), newPeer.String())
	default:
		assert.Error(t, errors.New("no new peer event"))
	}
}

func assertNoNewDisconnectedPeerEvent(t *testing.T, eventChan <-chan crypto.PublicKey) {
	select {
	case newPeer := <-eventChan:
		assert.Error(t, errors.New("unexpected new peer event, peer " + newPeer.String()))
	default:
		return
	}
}


func Test_Neihborhood_getMorePeers(t *testing.T) {
	// test normal flow
	numpeers := 3
	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = false
	cfg.SwarmConfig.Gossip = false
	n := p2pTestInstance(t, cfg)

	conn, _ := n.SubscribePeerEvents()

	res := n.getMorePeers(0) // this should'nt work
	assert.Equal(t, res, 0)
	assertNoNewPeerEvent(t, conn)

	mdht := new(dht.MockDHT)
	n.dht = mdht
	// this will return 0 peers because SelectPeers returns empty array when not set

	res = n.getMorePeers(10)
	assert.Equal(t, res, 0)
	assertNoNewPeerEvent(t, conn)

	testNode := node.GenerateRandomNodeData()
	fmt.Println("??????????? " + testNode.PublicKey().String())
	mdht.SelectPeersFunc = func(qty int) []node.Node {
		return []node.Node{testNode}
	}

	cpm := new(cpoolMock)

	// test connection error
	cpm.f = func(address string, pk crypto.PublicKey) (net.Connection, error) {
		return nil, errors.New("can't make connection")
	}

	n.cPool = cpm
	res = n.getMorePeers(1) // this should'nt work
	assert.Equal(t, res, 0)
	cpm.f = nil // for next tests
	assertNoNewPeerEvent(t, conn)

	res = n.getMorePeers(1)
	assert.Equal(t, 1, res)
	assert.Equal(t, len(n.outpeers), 1)
	assert.True(t, n.hasOutgoingPeer(testNode.PublicKey()))
	assertNewPeerEvents(t, 1, conn)
	assertNewPeerEvent(t, testNode.PublicKey(), conn)


	mdht.SelectPeersFunc = func(qty int) []node.Node {
		return node.GenerateRandomNodesData(qty)
	}

	res = n.getMorePeers(numpeers)
	assert.Equal(t, res, numpeers)
	assert.Equal(t, len(n.outpeers), numpeers)
	assertNewPeerEvents(t, numpeers, conn)

	//test inc peer

	nd := node.GenerateRandomNodeData()
	n.addIncomingPeer(nd.PublicKey())

	assert.True(t, n.hasIncomingPeer(nd.PublicKey()))
	assertNewPeerEvents(t, 1, conn)
	assertNewPeerEvent(t, nd.PublicKey(), conn)

	inlen := len(n.inpeers)

	//test replacing inc peer

	mdht.SelectPeersFunc = func(count int) []node.Node {
		some := node.GenerateRandomNodesData(count - 1)
		some = append(some, nd)
		return some
	}

	res = n.getMorePeers(numpeers)
	assert.Equal(t, res, numpeers)
	assert.Equal(t, len(n.outpeers), numpeers+res)
	assert.True(t, len(n.inpeers) == inlen-1)
	assert.True(t, n.hasOutgoingPeer(nd.PublicKey()))
	assert.False(t, n.hasIncomingPeer(nd.PublicKey()))
}


func TestNeighborhood_Initial(t *testing.T) {
	p := p2pTestNoStart(t, config.DefaultConfig())
	mdht := new(dht.MockDHT)
	mdht.SelectPeersFunc = func(qty int) []node.Node {
		return node.GenerateRandomNodesData(qty)
	}

	p.dht = mdht

	cpm := new(cpoolMock)
	cpm.f = func(address string, pk crypto.PublicKey) (net.Connection, error) {
		return net.NewConnectionMock(pk), nil
	}

	//err := p.Start()
	//assert.NoError(err)
	//ti := time.After(time.Second*2)
	//	select {
	//	case <-p.initial:
	//		break
	//	case <-ti:
	//		t.Error("2 seconds passed")
	//	}
	//

	p.Shutdown()
}

func TestNeighborhood_Disconnect(t *testing.T) {

	//n := p2pTestInstance(t, config.DefaultConfig())
	//
	//tim := time.After(time.Second)
	//select {
	//case <-n.initial:
	//	break
	//case <-tim:
	//	t.Error("didnt initialize")
	//}
	//
	////sampMock.f = nil // dont give out on a normal state
	//
	//nd := node.GenerateRandomNodeData()
	//cn, _ := cpoolMock.GetConnection(nd.Address(), nd.PublicKey())
	//n.addIncomingPeer(nd.PublicKey())
	//
	//assert.True(t, n.hasIncomingPeer(nd.PublicKey()))
	//
	//n.Disconnect(nd.PublicKey())
	//
	//assert.False(t, n.hasIncomingPeer(nd.PublicKey()))
	//
	//cpoolMock.f = func(address string, pk crypto.PublicKey) (net.Connection, error) {
	//	return nil, errors.New("no connections")
	//}
	//
	//n.Disconnect(out.PublicKey())
	//
	//assert.False(t, n.hasOutgoingPeer(nd.PublicKey()))

}

func TestSwarm_AddIncomingPeer(t *testing.T) {
	p := p2pTestInstance(t, config.DefaultConfig())
	rnd := node.GenerateRandomNodeData()
	p.addIncomingPeer(rnd.PublicKey())

	p.inpeersMutex.RLock()
	peer, ok := p.inpeers[rnd.PublicKey().String()]
	p.inpeersMutex.RUnlock()

	assert.True(t, ok)
	assert.NotNil(t, peer)
}
//
////TODO : Test this without real network
//func TestBootstrap(t *testing.T) {
//	bootnodes := []int{3}
//	nodes := []int{30}
//	rcon := []int{3}
//
//	bootcfg := config.DefaultConfig()
//	bootcfg.SwarmConfig.Bootstrap = false
//	bootcfg.SwarmConfig.Gossip = false
//
//
//	rand.Seed(time.Now().UnixNano())
//
//	for i := 0; i < len(nodes); i++ {
//		t.Run(fmt.Sprintf("Peers:%v/randconn:%v", nodes[i], rcon[i]), func(t *testing.T) {
//			bufchan := make(chan *swarm, nodes[i])
//
//			bnarr := []string{}
//
//			for k := 0; k < bootnodes[i]; k++ {
//				bn := p2pTestInstance(t, bootcfg)
//				bn.lNode.Info("This is a bootnode - %v", bn.lNode.Node.String())
//				bnarr = append(bnarr, node.StringFromNode(bn.lNode.Node))
//			}
//
//			cfg := config.DefaultConfig()
//			cfg.SwarmConfig.Bootstrap = true
//			cfg.SwarmConfig.Gossip = false
//			cfg.SwarmConfig.RandomConnections = rcon[i]
//			cfg.SwarmConfig.BootstrapNodes = bnarr
//
//			var wg sync.WaitGroup
//
//			for j := 0; j < nodes[i]; j++ {
//				wg.Add(1)
//				go func() {
//					sw := p2pTestInstance(t, cfg)
//					sw.waitForBoot()
//					bufchan <- sw
//					wg.Done()
//				}()
//			}
//
//			wg.Wait()
//			close(bufchan)
//			swarms := []*swarm{}
//			for s := range bufchan {
//				swarms = append(swarms, s)
//
//			}
//
//			randnode := swarms[rand.Int31n(int32(len(swarms)-1))]
//			randnode2 := swarms[rand.Int31n(int32(len(swarms)-1))]
//
//			for (randnode == nil || randnode2 == nil) || randnode.lNode.String() == randnode2.lNode.String() {
//				randnode = swarms[rand.Int31n(int32(len(swarms)-1))]
//				randnode2 = swarms[rand.Int31n(int32(len(swarms)-1))]
//			}
//
//			randnode.RegisterProtocol(exampleProtocol)
//			recv := randnode2.RegisterProtocol(exampleProtocol)
//
//			sendDirectMessage(t, randnode, randnode2.lNode.PublicKey().String(), recv, true)
//			time.Sleep(1* time.Second)
//		})
//	}
//}
