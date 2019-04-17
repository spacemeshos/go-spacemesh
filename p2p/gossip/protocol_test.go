package gossip

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

type mockBaseNetwork struct {
	msgSentByPeer        map[string]uint32
	directInbox          chan service.DirectMessage
	gossipInbox          chan service.GossipMessage
	connSubs             []chan p2pcrypto.PublicKey
	discSubs             []chan p2pcrypto.PublicKey
	totalMsgCount        int
	processProtocolCount int
	msgMutex             sync.Mutex
	pcountwg             *sync.WaitGroup
	msgwg                *sync.WaitGroup
	lastMsg              []byte
	isMessageValid       bool
}

func newMockBaseNetwork() *mockBaseNetwork {
	return &mockBaseNetwork{
		make(map[string]uint32),
		make(chan service.DirectMessage, 30),
		make(chan service.GossipMessage, 30),
		make([]chan p2pcrypto.PublicKey, 0, 5),
		make([]chan p2pcrypto.PublicKey, 0, 5),
		0,
		0,
		sync.Mutex{},
		&sync.WaitGroup{},
		&sync.WaitGroup{},
		[]byte(nil),
		true,
	}
}

func (mbn *mockBaseNetwork) SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error {
	mbn.msgMutex.Lock()
	mbn.lastMsg = payload
	mbn.msgSentByPeer[peerPubkey.String()]++
	mbn.totalMsgCount++
	mbn.msgMutex.Unlock()
	releaseWaiters(mbn.msgwg)
	return nil
}

func passOrDeadlock(t testing.TB, group *sync.WaitGroup) {
	ch := make(chan struct{})
	go func(ch chan struct{}, t testing.TB) {
		timer := time.NewTimer(time.Second * 3)
		for {
			select {
			case <-ch:
				return
			case <-timer.C:
				t.FailNow() // deadlocked
			}
		}
	}(ch, t)

	group.Wait()
	close(ch)
}

// we use releaseWaiters to release a waitgroup and not panic if we don't use it
func releaseWaiters(group *sync.WaitGroup) {
	group.Done()
}

func (mbn *mockBaseNetwork) RegisterDirectProtocol(protocol string) chan service.DirectMessage {
	return mbn.directInbox
}

func (mbn *mockBaseNetwork) RegisterGossipProtocol(protocol string) chan service.GossipMessage {
	return mbn.gossipInbox
}

func (mbn *mockBaseNetwork) SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey) {
	conn = make(chan p2pcrypto.PublicKey, 20)
	disc = make(chan p2pcrypto.PublicKey, 20)

	mbn.connSubs = append(mbn.connSubs, conn)
	mbn.discSubs = append(mbn.discSubs, disc)
	return
}

func (mbn *mockBaseNetwork) ProcessDirectProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data) error {
	mbn.processProtocolCount++
	releaseWaiters(mbn.pcountwg)
	return nil
}

func (mbn *mockBaseNetwork) ProcessGossipProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error {
	mbn.processProtocolCount++
	if validationCompletedChan != nil {
		validationCompletedChan <- service.NewMessageValidation(sender, data.Bytes(), protocol)
	}
	time.Sleep(time.Millisecond) // context switch to allow gossip to handle the validation report
	releaseWaiters(mbn.pcountwg)
	return nil
}

func (mbn *mockBaseNetwork) addRandomPeers(cnt int) {
	for i := 0; i < cnt; i++ {
		pub := p2pcrypto.NewRandomPubkey()
		mbn.addRandomPeer(pub)
	}
}

func (mbn *mockBaseNetwork) addRandomPeer(pub p2pcrypto.PublicKey) {
	for _, p := range mbn.connSubs {
		p <- pub
	}
}

func (mbn *mockBaseNetwork) totalMessageSent() int {
	mbn.msgMutex.Lock()
	total := mbn.totalMsgCount
	mbn.msgMutex.Unlock()
	return total
}

type TestMessage struct {
	sender p2pcrypto.PublicKey
	data   service.Data
}

func (tm TestMessage) Metadata() service.P2PMetadata {
	return service.P2PMetadata{}
}

func (tm TestMessage) Sender() p2pcrypto.PublicKey {
	return tm.sender
}

func (tm TestMessage) setData(msg service.Data) {
	tm.data = msg
}

func (tm TestMessage) Data() service.Data {
	return tm.data
}

func (tm TestMessage) Bytes() []byte {
	return tm.data.Bytes()
}

func newPubkey(t *testing.T) p2pcrypto.PublicKey {
	pubkey := p2pcrypto.NewRandomPubkey()
	return pubkey
}

func newTestMessageData(t testing.TB, authPubkey p2pcrypto.PublicKey, payload []byte, protocol string) ([]byte, *pb.ProtocolMessage) {
	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  protocol,
			Timestamp:     time.Now().Unix(),
			ClientVersion: protocolVer,
			AuthPubkey:    authPubkey.Bytes(),
		},
		Payload: &pb.Payload{Data: &pb.Payload_Payload{payload}},
	}

	return makePayload(t, pm).Bytes(), pm
}

func addPeersAndTest(t testing.TB, num int, p *Protocol, net *mockBaseNetwork, work bool) {

	pc := p.peersCount()
	reg, _ := net.SubscribePeerEvents()
	net.addRandomPeers(num)

	i := 0
lop:
	for {
		select {
		case <-reg:
			i++
			time.Sleep(time.Millisecond) // we need to somehow let other goroutines work before us
		default:
			break lop
		}
	}

	if i != num {
		t.Fatal("Didn't get added peers on chan")
	}

	newpc := p.peersCount()
	worked := pc+num == newpc
	if worked != work {
		t.Fatalf("adding the peers didn't work as expected old peer count: %d, tried to add: %d, new peercount: %d", pc, num, newpc)
	}
}

//todo : more unit tests

func TestNeighborhood_AddIncomingPeer(t *testing.T) {
	n := NewProtocol(config.DefaultConfig().SwarmConfig, newMockBaseNetwork(), newPubkey(t), log.New("tesT", "", ""))
	n.Start()
	pub := p2pcrypto.NewRandomPubkey()
	n.addPeer(pub)

	assert.True(t, n.hasPeer(pub))
	assert.Equal(t, 1, n.peersCount())
}

func makePayload(t testing.TB, message *pb.ProtocolMessage) service.Data {
	payload, err := proto.Marshal(message)
	assert.NoError(t, err)
	return service.DataBytes{Payload: payload}
}

func TestNeighborhood_Relay(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	addPeersAndTest(t, 20, n, net, true)
	pk := p2pcrypto.NewRandomPubkey()
	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			Timestamp:     time.Now().Unix(),
			ClientVersion: protocolVer,
			AuthPubkey:    pk.Bytes(),
		},
		Payload: &pb.Payload{Data: &pb.Payload_Payload{[]byte("LOL")}},
	}

	payload := makePayload(t, pm)

	var msg service.DirectMessage = TestMessage{pk, payload}
	net.pcountwg.Add(1)
	net.msgwg.Add(20)
	net.directInbox <- msg
	passOrDeadlock(t, net.pcountwg)
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMsgCount)
}

func TestNeighborhood_Broadcast(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()
	addPeersAndTest(t, 20, n, net, true)
	net.msgwg.Add(20)
	net.pcountwg.Add(1)

	err := n.Broadcast([]byte("LOL"), "")
	assert.NoError(t, err)

	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Relay2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	var pk p2pcrypto.PublicKey = nil
	for pk == nil {
		rnd := p2pcrypto.NewRandomPubkey()
		n.peersMutex.RLock()
		if _, ok := n.peers[rnd.String()]; ok {
			n.peersMutex.RUnlock()
			continue
		}
		n.peersMutex.RUnlock()
		pk = rnd
	}
	msgB, _ := newTestMessageData(t, pk, []byte("LOL1"), "protocol")
	var msg service.DirectMessage = TestMessage{pk, service.DataBytes{msgB}}
	net.pcountwg.Add(1)
	net.directInbox <- msg
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())

	net.msgwg.Add(20)
	net.pcountwg.Add(1)

	addPeersAndTest(t, 20, n, net, true)

	newmsg, _ := newTestMessageData(t, pk, []byte("LOL2"), "protocol")
	var ready service.DirectMessage = TestMessage{pk, service.DataBytes{newmsg}}

	net.directInbox <- ready
	passOrDeadlock(t, net.msgwg)
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Broadcast2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	payload := []byte("LOL")
	addPeersAndTest(t, 1, n, net, true)
	net.msgwg.Add(1) // sender also handle the message
	net.pcountwg.Add(1)

	err := n.Broadcast(payload, "protocol")
	assert.NoError(t, err)

	passOrDeadlock(t, net.msgwg)
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 1, net.totalMessageSent())

	addPeersAndTest(t, 20, n, net, true)
	net.msgwg.Add(21)
	net.pcountwg.Add(1)

	err = n.Broadcast([]byte("LOL2"), "protocol")
	require.NoError(t, err)

	passOrDeadlock(t, net.msgwg)
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 22, net.totalMessageSent()) // 1 + 21 now.
}

func TestNeighborhood_Broadcast3(t *testing.T) {
	// todo : Fix this test, because the first message is broadcasted `Broadcast` attaches metadata to it with the current authoring timestamp
	// to test that the the next message doesn't get processed by the protocol we must create an exact copy of the message produced at `Broadcast`
	net := newMockBaseNetwork()
	pk := p2pcrypto.NewRandomPubkey()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, pk, log.New("tesT", "", ""))
	n.Start()

	addPeersAndTest(t, 20, n, net, true)

	msgB := []byte("LOL")
	net.msgwg.Add(20)
	net.pcountwg.Add(1)
	err := n.Broadcast(msgB, "protocol")
	assert.NoError(t, err)

	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
	pk2 := newPubkey(t)
	payload, _ := newTestMessageData(t, pk2, msgB, "protocol")
	var msg service.DirectMessage = TestMessage{pk2, service.DataBytes{payload}}
	net.directInbox <- msg
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Broadcast4(t *testing.T) {
	// todo : Fix this test, because the first message is broadcasted `Broadcast` attaches metadata to it with the current authoring timestamp
	// to test that the the next message doesn't get processed by the protocol we must create an exact copy of the message produced at `Broadcast`
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	addPeersAndTest(t, 20, n, net, true)

	msgB := []byte("LOL")
	net.msgwg.Add(20)
	net.pcountwg.Add(1)

	err := n.Broadcast(msgB, "")
	assert.NoError(t, err)

	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())

	err = n.Broadcast(msgB, "")
	assert.NoError(t, err)

	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Relay3(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	pk := p2pcrypto.NewRandomPubkey()
	payload, _ := newTestMessageData(t, pk, []byte("LOL"), "protocol")
	var msg service.DirectMessage = TestMessage{pk, service.DataBytes{payload}}
	net.pcountwg.Add(1)
	net.directInbox <- msg
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())

	addPeersAndTest(t, 20, n, net, true)

	net.directInbox <- msg

	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())
}

func TestNeighborhood_Start(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))

	// before Start
	addPeersAndTest(t, 20, n, net, false)

	n.Start()

	addPeersAndTest(t, 20, n, net, true)
}

func TestNeighborhood_Close(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))

	n.Start()
	addPeersAndTest(t, 20, n, net, true)

	n.Close()
	addPeersAndTest(t, 20, n, net, false)
}

func TestNeighborhood_Disconnect(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))

	n.Start()
	pub1 := p2pcrypto.NewRandomPubkey()
	n.addPeer(pub1)
	pub2 := p2pcrypto.NewRandomPubkey()
	n.addPeer(pub2)
	assert.Equal(t, 2, n.peersCount())

	pk := p2pcrypto.NewRandomPubkey()
	msg, _ := newTestMessageData(t, pk, []byte("LOL"), "protocol")

	net.pcountwg.Add(1)
	net.msgwg.Add(2)
	net.directInbox <- TestMessage{pk, service.DataBytes{msg}}
	passOrDeadlock(t, net.pcountwg)
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 2, net.totalMessageSent())

	pk2 := p2pcrypto.NewRandomPubkey()
	msg2, _ := newTestMessageData(t, pk2, []byte("LOL2"), "protocol")

	n.removePeer(pub1)
	net.pcountwg.Add(1)
	net.msgwg.Add(1)
	net.directInbox <- TestMessage{pk2, service.DataBytes{msg2}}
	passOrDeadlock(t, net.pcountwg)
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 3, net.totalMessageSent())

	n.addPeer(pub1)
	net.msgwg.Add(1)
	net.directInbox <- TestMessage{pk2, service.DataBytes{msg2}}
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 3, net.totalMessageSent())
}

func TestHash(t *testing.T) {
	msg1 := []byte("msg1")
	msg2 := []byte("msg2")
	prot1 := "prot1"
	prot2 := "prot2"

	assert.NotEqual(t, calcHash(msg1, prot1), calcHash(msg1, prot2))
	assert.NotEqual(t, calcHash(msg1, prot1), calcHash(msg2, prot1))
}
