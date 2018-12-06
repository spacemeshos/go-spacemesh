package gossip

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
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
	connSubs             []chan crypto.PublicKey
	discSubs             []chan crypto.PublicKey
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
		make(chan service.Message, 20),
		make([]chan crypto.PublicKey, 0, 5),
		make([]chan crypto.PublicKey, 0, 5),
		0,
		0,
		sync.Mutex{},
		&sync.WaitGroup{},
		&sync.WaitGroup{},
		[]byte(nil),
	}
}

func (mbn *mockBaseNetwork) SendMessage(peerPubKey string, protocol string, payload []byte) error {
	mbn.lastMsg = payload
	mbn.msgMutex.Lock()
	mbn.msgSentByPeer[peerPubKey]++
	mbn.totalMsgCount++
	mbn.msgMutex.Unlock()
	releaseWaiters(mbn.msgwg)
	return nil
}

func onRelease(t testing.TB, group *sync.WaitGroup) {
	ch := make(chan struct{})
	go func(wg *sync.WaitGroup, ch chan struct{}) {
		wg.Wait()
		close(ch)
	}(group, ch)

	timer := time.NewTimer(time.Millisecond * 5)
	select {
	case <-ch:
		return
	case <-timer.C:
		t.Error("deadlock")
	}
}

// we use releaseWaiters to release a waitgroup and not panic if we don't use it
func releaseWaiters(group *sync.WaitGroup) {
	ch := make(chan struct{})
	go func() {
		group.Wait()
		ch <- struct{}{}
	}()
	ti := time.After(time.Millisecond)
	select {
	case <-ch:
		return
	case <-ti:
		group.Done()
		go func() { <-ch }() // just waste it
	}
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
	releaseWaiters(mbn.pcountwg)
	return nil
}

func (mbn *mockBaseNetwork) addRandomPeers(cnt int) {
	for i := 0; i < cnt; i++ {
		_, pub, _ := crypto.GenerateKeyPair()
		mbn.addRandomPeer(pub)
	}
}

func (mbn *mockBaseNetwork) addRandomPeer(pub crypto.PublicKey) {
	for _, p := range mbn.connSubs {
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

type testSigner struct {
	pv crypto.PrivateKey
}

func (ms testSigner) PublicKey() crypto.PublicKey {
	return ms.pv.GetPublicKey()
}

func (ms testSigner) Sign(data []byte) ([]byte, error) {
	return ms.pv.Sign(data)
}

func newTestSigner(t testing.TB) testSigner {
	pv, _, err := crypto.GenerateKeyPair()
	assert.NoError(t, err)
	return testSigner{pv}
}

func newTestSignedMessageData(t testing.TB, signer Signer) []byte {
	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			AuthPubKey:    signer.PublicKey().Bytes(),
			Timestamp:     time.Now().Unix(),
			ClientVersion: config.ClientVersion,
		},
		Payload: []byte("LOL"),
	}

	return signedMessage(t, signer, pm)
}

//todo : more unit tests

func TestNeighborhood_AddIncomingPeer(t *testing.T) {
	n := NewProtocol(config.DefaultConfig().SwarmConfig, newMockBaseNetwork(), newTestSigner(t), log.New("tesT", "", ""))
	n.Start()
	_, pub, _ := crypto.GenerateKeyPair()
	n.addPeer(pub)

	assert.True(t, n.hasPeer(pub))
	assert.Equal(t, 1, n.peersCount())
}

func signedMessage(t testing.TB, s Signer, message *pb.ProtocolMessage) []byte {
	pmbin, err := proto.Marshal(message)
	assert.NoError(t, err)
	sign, err := s.Sign(pmbin)
	assert.NoError(t, err)
	message.Metadata.MsgSign = sign
	finbin, err := proto.Marshal(message)
	assert.NoError(t, err)
	return finbin
}

func TestNeighborhood_Relay(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()
	net.addRandomPeers(20)

	signer := newTestSigner(t)
	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			AuthPubKey:    signer.PublicKey().Bytes(),
			Timestamp:     time.Now().Unix(),
			ClientVersion: config.ClientVersion,
		},
		Payload: []byte("LOL"),
	}

	signed := signedMessage(t, signer, pm)
	//n.Broadcast([]byte("LOL"))
	var msg service.Message = TestMessage{signed}
	net.pcountwg.Add(1)
	net.inbox <- msg
	onRelease(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
}

func TestNeighborhood_Broadcast(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()
	net.addRandomPeers(20)

	net.msgwg.Add(20)
	n.Broadcast([]byte("LOL"), "")
	onRelease(t, net.msgwg)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Relay2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	signer := newTestSigner(t)
	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			AuthPubKey:    signer.PublicKey().Bytes(),
			Timestamp:     time.Now().Unix(),
			ClientVersion: config.ClientVersion,
		},
		Payload: []byte("LOL"),
	}

	signed := signedMessage(t, signer, pm)
	var msg service.Message = TestMessage{signed}
	net.pcountwg.Add(1)
	net.inbox <- msg
	onRelease(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())

	net.msgwg.Add(20)
	net.addRandomPeers(20)
	net.inbox <- msg
	onRelease(t, net.msgwg)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Broadcast2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	msgB := []byte("LOL")
	net.addRandomPeers(1)
	net.msgwg.Add(1)
	n.Broadcast(msgB, "") // dosent matter
	onRelease(t, net.msgwg)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 1, net.totalMessageSent())

	net.msgwg.Add(20)
	net.addRandomPeers(20)
	var msg service.Message = TestMessage{net.lastMsg}
	net.inbox <- msg
	onRelease(t, net.msgwg)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 21, net.totalMessageSent())
}

func TestNeighborhood_Broadcast3(t *testing.T) {
	// todo : Fix this test, because the first message is broadcasted `Broadcast` attaches metadata to it with the current authoring timestamp
	// to test that the the next message doesn't get processed by the protocol we must create an exact copy of the message produced at `Broadcast`
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	net.addRandomPeers(20)

	msgB := []byte("LOL")
	net.msgwg.Add(20)
	n.Broadcast(msgB, "")
	onRelease(t, net.msgwg)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())

	var msg service.Message = TestMessage{net.lastMsg}
	net.inbox <- msg
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Relay3(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	var msg service.Message = TestMessage{newTestSignedMessageData(t, newTestSigner(t))}
	net.pcountwg.Add(1)
	net.inbox <- msg
	onRelease(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())

	net.addRandomPeers(20)
	net.msgwg.Add(20)
	net.inbox <- msg
	onRelease(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())

	net.addRandomPeers(1)
	net.msgwg.Add(1)
	net.inbox <- msg
	onRelease(t, net.msgwg)

	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 21, net.totalMessageSent())
}

func TestNeighborhood_Start(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))

	// before Start
	net.addRandomPeers(20)
	assert.Equal(t, 0, n.peersCount())

	n.Start()

	net.addRandomPeers(20)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 20, n.peersCount())
}

func TestNeighborhood_Close(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))

	n.Start()
	net.addRandomPeers(20)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 20, n.peersCount())

	n.Close()
	net.addRandomPeers(20)
	assert.Equal(t, 20, n.peersCount())
}

func TestNeighborhood_Disconnect(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))

	n.Start()
	_, pub1, _ := crypto.GenerateKeyPair()
	n.addPeer(pub1)
	_, pub2, _ := crypto.GenerateKeyPair()
	n.addPeer(pub2)
	assert.Equal(t, 2, n.peersCount())

	msg := newTestSignedMessageData(t, newTestSigner(t))

	net.pcountwg.Add(1)
	net.msgwg.Add(2)
	net.inbox <- TestMessage{msg}
	onRelease(t, net.pcountwg)
	onRelease(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 2, net.totalMessageSent())

	msg2 := newTestSignedMessageData(t, newTestSigner(t))

	n.removePeer(pub1)
	net.pcountwg.Add(1)
	net.msgwg.Add(1)
	net.inbox <- TestMessage{msg2}
	onRelease(t, net.pcountwg)
	onRelease(t, net.msgwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 3, net.totalMessageSent())

	n.addPeer(pub1)
	net.msgwg.Add(1)
	net.inbox <- TestMessage{msg2}
	onRelease(t, net.msgwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 4, net.totalMessageSent())
}
