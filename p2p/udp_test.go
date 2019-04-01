package p2p

import (
	"errors"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	net2 "net"
	"testing"
	"time"
)

const test_str = "regTest"

type mockUDPNetwork struct {
	startCalled bool
	startResult error

	shutdownCalled bool

	sendCalled bool
	sendResult error

	inc chan net.UDPMessageEvent
}

func (mun *mockUDPNetwork) Start() error {
	mun.startCalled = true
	return mun.startResult
}

func (mun *mockUDPNetwork) Shutdown() {
	mun.shutdownCalled = true
}

func (mun *mockUDPNetwork) IncomingMessages() chan net.UDPMessageEvent {
	return mun.inc
}

func (mun *mockUDPNetwork) Send(to node.Node, data []byte) error {
	mun.sendCalled = true
	return mun.sendResult
}

func TestNewUDPMux(t *testing.T) {
	nd := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	mockdht := &dht.MockDHT{}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, mockdht, udpMock, log.New("test", "", ""))
	require.NotNil(t, m)
}

func TestUDPMux_RegisterDirectProtocolWithChannel(t *testing.T) {
	nd := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	mockdht := &dht.MockDHT{}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, mockdht, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)
	c2, ok := m.messages[test_str]
	require.True(t, ok)
	require.NotNil(t, c2)
	require.Equal(t, c, c2)
}

func TestUDPMux_Start(t *testing.T) {
	nd := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	mockdht := &dht.MockDHT{}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, mockdht, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	err := m.Start()
	require.NoError(t, err)
	require.True(t, udpMock.startCalled)

	udpMock.startCalled = false
	udpMock.startResult = errors.New("Error start")
	m = NewUDPMux(nd, mockdht, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	err = m.Start()
	require.True(t, udpMock.startCalled)
	require.Error(t, err)
}

func TestUDPMux_ProcessDirectProtocolMessage(t *testing.T) {
	nd := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	mockdht := &dht.MockDHT{}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, mockdht, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)

	data := service.DataBytes{[]byte(test_str)}
	nod := node.GenerateRandomNodeData()
	addr, err := net2.ResolveUDPAddr("udp", nod.Address())
	require.NoError(t, err)
	err = m.ProcessDirectProtocolMessage(nod.PublicKey(), test_str, data, service.P2PMetadata{addr})
	require.Error(t, err) // no protocol
	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)
	err = m.ProcessDirectProtocolMessage(nod.PublicKey(), test_str, data, service.P2PMetadata{addr})
	require.NoError(t, err)
	select {
	case msg := <-c:
		require.Equal(t, msg.Bytes(), data.Bytes())
	default:
		t.Fatal("failed")
	}
}

func TestUDPMux_sendMessageImpl(t *testing.T) {
	nd := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	mockdht := &dht.MockDHT{}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, mockdht, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	data := service.DataBytes{[]byte(test_str)}
	sendto := node.GenerateRandomNodeData()

	var lookupcalled bool
	mockdht.InternalLookupFunc = func(dhtid node.DhtID) []node.Node {
		lookupcalled = true
		return []node.Node{}
	}

	err := m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.Error(t, err)
	require.True(t, lookupcalled)
	require.False(t, udpMock.sendCalled)

	lookupcalled = false
	mockdht.InternalLookupFunc = func(dhtid node.DhtID) []node.Node {
		lookupcalled = true
		return []node.Node{sendto}
	}

	udpMock.sendResult = errors.New("err send")

	err = m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.Error(t, err)
	require.True(t, lookupcalled)
	require.True(t, udpMock.sendCalled)

	udpMock.sendResult = nil
	udpMock.startCalled = false

	err = m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.NoError(t, err)
	require.True(t, lookupcalled)
	require.True(t, udpMock.sendCalled)

}

func TestUDPMux_ProcessUDP(t *testing.T) {
	nd := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	mockdht := &dht.MockDHT{}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, mockdht, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	data := service.DataBytes{[]byte(test_str)}
	msg := &pb.ProtocolMessage{}

	gotfrom := node.GenerateRandomNodeData()

	msg.Metadata = NewProtocolMessageMetadata(gotfrom.PublicKey(), test_str)
	msg.Data = &pb.ProtocolMessage_Payload{Payload: data.Bytes()}

	msgbuf, err := proto.Marshal(msg)
	require.NoError(t, err)

	addr, _ := net2.ResolveUDPAddr("udp", gotfrom.Address())

	err = m.processUDPMessage(addr, msgbuf)

	require.Error(t, err) // no protocol

	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)
	err = m.processUDPMessage(addr, msgbuf)
	require.NoError(t, err) // no protocol

	select {
	case msg := <-c:
		require.Equal(t, msg.Metadata().FromAddress, addr)
		require.Equal(t, msg.Sender(), gotfrom.PublicKey())
		require.Equal(t, msg.Bytes(), data.Bytes())
	default:
		t.Fatal("didn't get msg")
	}

}

func Test_RoundTrip(t *testing.T) {
	nd := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	mockdht := &dht.MockDHT{}
	udpnet, err := net.NewUDPNet(config.DefaultConfig(), nd, log.New("", "", ""))
	require.NoError(t, err)
	m := NewUDPMux(nd, mockdht, udpnet, log.New(test_str, "", ""))
	require.NotNil(t, m)

	nd2 := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	mockdht2 := &dht.MockDHT{}
	udpnet2, err := net.NewUDPNet(config.DefaultConfig(), nd2, log.New("", "", ""))
	require.NoError(t, err)
	m2 := NewUDPMux(nd2, mockdht2, udpnet2, log.New(test_str+"2", "", ""))

	require.NotNil(t, m2)

	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)
	c2 := make(chan service.DirectMessage, 1)
	m2.RegisterDirectProtocolWithChannel(test_str, c2)

	mockdht.InternalLookupFunc = func(dhtid node.DhtID) []node.Node {
		return []node.Node{nd2.Node}
	}

	err = m.Start()
	require.NoError(t, err)
	err = m2.Start()
	require.NoError(t, err)

	err = m.SendMessage(nd2.PublicKey(), test_str, []byte(test_str))

	require.NoError(t, err)
	tm := time.NewTimer(time.Second)

	select {
	case msg := <-c2:
		require.Equal(t, msg.Metadata().FromAddress.String(), nd.Address())
		require.Equal(t, msg.Sender(), nd.PublicKey())
		require.Equal(t, msg.Bytes(), []byte(test_str))
	case <-tm.C:
		t.Fatal("message timeout")

	}
}
