package gossip

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

type mockBaseNetwork struct {
	msgSentByPeer        map[string]uint32
	inbox                chan service.Message
	connSubs             []chan p2pcrypto.PublicKey
	discSubs             []chan p2pcrypto.PublicKey
	totalMsgCount        int
	processProtocolCount int
	msgMutex             sync.Mutex
	pcountwg             *sync.WaitGroup
	msgwg                *sync.WaitGroup
	lastMsg              []byte
}

func newMockBaseNetwork() *mockBaseNetwork {
	return &mockBaseNetwork{
		make(map[string]uint32),
		make(chan service.Message, 30),
		make([]chan p2pcrypto.PublicKey, 0, 5),
		make([]chan p2pcrypto.PublicKey, 0, 5),
		0,
		0,
		sync.Mutex{},
		&sync.WaitGroup{},
		&sync.WaitGroup{},
		[]byte(nil),
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

func (mbn *mockBaseNetwork) RegisterProtocol(protocol string) chan service.Message {
	return mbn.inbox
}

func (mbn *mockBaseNetwork) SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey) {
	conn = make(chan p2pcrypto.PublicKey, 20)
	disc = make(chan p2pcrypto.PublicKey, 20)

	mbn.connSubs = append(mbn.connSubs, conn)
	mbn.discSubs = append(mbn.discSubs, disc)
	return
}

func (mbn *mockBaseNetwork) ProcessProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error {
	mbn.processProtocolCount++
	releaseWaiters(mbn.pcountwg)
	if (validationCompletedChan != nil) {
		validationCompletedChan <- *service.NewMessageValidation(data.Bytes(), protocol, true)
	}
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
	data service.Data
	validationChan chan<- service.MessageValidation
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

func (tm TestMessage) ValidationCompletedChan() chan<- service.MessageValidation {
	return tm.validationChan
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
			AuthPubKey:    authPubkey.Bytes(),
		},
		Data: &pb.ProtocolMessage_Payload{payload},
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

	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			Timestamp:     time.Now().Unix(),
			ClientVersion: protocolVer,
			AuthPubKey:    newPubkey(t).Bytes(),
		},
		Data: &pb.ProtocolMessage_Payload{[]byte("LOL")},
	}

	payload := makePayload(t, pm)

	var msg service.Message = TestMessage{nil, payload, nil}
	net.pcountwg.Add(1)
	net.msgwg.Add(20)
	net.inbox <- msg
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

	n.Broadcast([]byte("LOL"), "")
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Relay2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			Timestamp:     time.Now().Unix(),
			ClientVersion: protocolVer,
			AuthPubKey:    newPubkey(t).Bytes(),
		},
		Data: &pb.ProtocolMessage_Payload{[]byte("LOL")},
	}

	payload := makePayload(t, pm)
	var msg service.Message = TestMessage{nil, 	payload, nil}
	net.pcountwg.Add(1)
	net.inbox <- msg
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())

	addPeersAndTest(t, 20, n, net, true)
	net.msgwg.Add(20)
	net.inbox <- msg
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Broadcast2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	msgB, _ := newTestMessageData(t, newPubkey(t), []byte("LOL"), "protocol")
	addPeersAndTest(t, 1, n, net, true)
	net.msgwg.Add(1) // sender also handle the message
	net.pcountwg.Add(1)
	n.Broadcast(msgB, "protocol")
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 1, net.totalMessageSent())

	addPeersAndTest(t, 20, n, net, true)
	net.msgwg.Add(20)
	payload, _ := newTestMessageData(t, newPubkey(t), net.lastMsg, "protocol")
	var msg service.Message = TestMessage{ nil, service.DataBytes{payload}, nil}
	net.inbox <- msg
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 21, net.totalMessageSent())
}

func TestNeighborhood_Broadcast3(t *testing.T) {
	// todo : Fix this test, because the first message is broadcasted `Broadcast` attaches metadata to it with the current authoring timestamp
	// to test that the the next message doesn't get processed by the protocol we must create an exact copy of the message produced at `Broadcast`
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	addPeersAndTest(t, 20, n, net, true)

	msgB := []byte("LOL")
	net.msgwg.Add(20)
	net.pcountwg.Add(1)
	n.Broadcast(msgB, "protocol")
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())

	payload, _ := newTestMessageData(t, newPubkey(t), net.lastMsg, "protocol")
	var msg service.Message = TestMessage{ nil, service.DataBytes{payload}, nil}
	net.inbox <- msg
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
	n.Broadcast(msgB, "")
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())

	n.Broadcast(msgB, "")
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Relay3(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newPubkey(t), log.New("tesT", "", ""))
	n.Start()

	payload, _ := newTestMessageData(t, newPubkey(t), []byte("LOL"), "protocol")
	var msg service.Message = TestMessage{ nil, service.DataBytes{payload}, nil}
	net.pcountwg.Add(1)
	net.inbox <- msg
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())

	addPeersAndTest(t, 20, n, net, true)

	net.msgwg.Add(20)
	net.inbox <- msg
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())

	addPeersAndTest(t, 1, n, net, true)

	net.msgwg.Add(1)
	net.inbox <- msg
	passOrDeadlock(t, net.msgwg)

	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 21, net.totalMessageSent())
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

	msg, _ := newTestMessageData(t, newPubkey(t), []byte("LOL"), "protocol")

	net.pcountwg.Add(1)
	net.msgwg.Add(2)
	net.inbox <- TestMessage{ nil, service.DataBytes{msg}, nil}
	passOrDeadlock(t, net.pcountwg)
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 2, net.totalMessageSent())

	msg2, _ := newTestMessageData(t, newPubkey(t), []byte("LOL2"), "protocol")

	n.removePeer(pub1)
	net.pcountwg.Add(1)
	net.msgwg.Add(1)
	net.inbox <- TestMessage{ nil, service.DataBytes{msg2}, nil}
	passOrDeadlock(t, net.pcountwg)
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 3, net.totalMessageSent())

	n.addPeer(pub1)
	net.msgwg.Add(1)
	net.inbox <- TestMessage{ nil, service.DataBytes{msg2}, nil}
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 4, net.totalMessageSent())
}

//func TestMarkAndValidateMessages(t *testing.T) {
//	net := newMockBaseNetwork()
//	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
//
//	_, msg := newSignedGossipMessageData(t, newTestSigner(t))
//	h := hash(666) // it doesn't really have to be the real hash
//	isOld, isInvalid := n.markAndValidateMessage(h, msg)
//	assert.False(t, isOld)
//	assert.False(t, isInvalid)
//
//	isOld, isInvalid = n.markAndValidateMessage(h, msg)
//	assert.True(t, isOld)
//	assert.False(t, isInvalid)
//
//	msg.Metadata.AuthPubKey = nil
//	h = hash(999) // it doesn't really have to be the real hash
//	isOld, isInvalid = n.markAndValidateMessage(h, msg)
//	assert.False(t, isOld)
//	assert.True(t, isInvalid)
//
//	isOld, isInvalid = n.markAndValidateMessage(h, msg)
//	assert.True(t, isOld)
//	assert.True(t, isInvalid)
//}