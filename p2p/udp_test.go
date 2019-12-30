package p2p

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
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

func (mun *mockUDPNetwork) Send(to *node.NodeInfo, data []byte) error {
	mun.sendCalled = true
	return mun.sendResult
}

func TestNewUDPMux(t *testing.T) {
	nd := &node.LocalNode{NodeInfo: node.GenerateRandomNodeData()}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, log.New("test", "", ""))
	require.NotNil(t, m)
}

func TestUDPMux_RegisterDirectProtocolWithChannel(t *testing.T) {
	nd := &node.LocalNode{NodeInfo: node.GenerateRandomNodeData()}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)
	c2, ok := m.messages[test_str]
	require.True(t, ok)
	require.NotNil(t, c2)
	require.Equal(t, c, c2)
}

func TestUDPMux_Start(t *testing.T) {
	nd := &node.LocalNode{NodeInfo: node.GenerateRandomNodeData()}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	err := m.Start()
	require.NoError(t, err)
}

func TestUDPMux_ProcessDirectProtocolMessage(t *testing.T) {
	nd := &node.LocalNode{NodeInfo: node.GenerateRandomNodeData()}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)

	data := service.DataBytes{[]byte(test_str)}
	nod := node.GenerateRandomNodeData()
	addr := &net2.UDPAddr{nod.IP, int(nod.DiscoveryPort), ""} //net2.ResolveUDPAddr("udp", nod.Address())
	err := m.ProcessDirectProtocolMessage(nod.PublicKey(), test_str, data, service.P2PMetadata{addr})
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
	nd := &node.LocalNode{NodeInfo: node.GenerateRandomNodeData()}
	udpMock := &mockUDPNetwork{}
	sendto := node.GenerateRandomNodeData()

	var lookupcalled bool
	f := func(key p2pcrypto.PublicKey) (*node.NodeInfo, error) {
		lookupcalled = true
		return nil, errors.New("nonode")
	}

	m := NewUDPMux(nd, f, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	data := service.DataBytes{[]byte(test_str)}

	err := m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.Error(t, err)
	require.True(t, lookupcalled)
	require.False(t, udpMock.sendCalled)

	lookupcalled = false
	f = func(key p2pcrypto.PublicKey) (*node.NodeInfo, error) {
		lookupcalled = true
		return sendto, nil
	}
	m.lookuper = f

	udpMock.sendResult = errors.New("err send")

	err = m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.Error(t, err)
	require.True(t, lookupcalled)
	require.True(t, udpMock.sendCalled)

	lookupcalled = false
	udpMock.sendResult = nil
	udpMock.startCalled = false

	err = m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.NoError(t, err)
	require.True(t, lookupcalled)
	require.True(t, udpMock.sendCalled)

}

func TestUDPMux_ProcessUDP(t *testing.T) {
	nd := &node.LocalNode{NodeInfo: node.GenerateRandomNodeData()}
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, log.New(test_str, "", ""))
	require.NotNil(t, m)
	data := service.DataBytes{[]byte(test_str)}
	msg := &ProtocolMessage{}

	gotfrom := node.GenerateRandomNodeData()

	msg.Metadata = &ProtocolMessageMetadata{NextProtocol: test_str, ClientVersion: config.ClientVersion, Timestamp: time.Now().Unix(),
		NetworkID: int32(nd.NetworkID())}
	//pb.NewUDPProtocolMessageMetadata(gotfrom.PublicKey(), int8(nd.NetworkID()), test_str)
	msg.Payload = &Payload{Payload: data.Bytes()}

	msgbuf, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	addr := &net2.UDPAddr{gotfrom.IP, int(gotfrom.DiscoveryPort), ""}

	err = m.processUDPMessage(gotfrom.PublicKey(), addr, msgbuf)

	require.Error(t, err) // no protocol

	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)
	err = m.processUDPMessage(gotfrom.PublicKey(), addr, msgbuf)
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
	nd, _ := node.GenerateTestNode(t)
	udpnet, err := net.NewUDPNet(config.DefaultConfig(), nd, log.New("", "", ""))
	require.NoError(t, err)

	m := NewUDPMux(nd, nil, udpnet, log.New(test_str, "", ""))
	require.NotNil(t, m)

	nd2, _ := node.GenerateTestNode(t)
	udpnet2, err := net.NewUDPNet(config.DefaultConfig(), nd2, log.New("", "", ""))
	require.NoError(t, err)

	m2 := NewUDPMux(nd2, nil, udpnet2, log.New(test_str+"2", "", ""))
	require.NotNil(t, m2)

	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)

	c2 := make(chan service.DirectMessage, 1)
	m2.RegisterDirectProtocolWithChannel(test_str, c2)

	m.lookuper = func(key p2pcrypto.PublicKey) (*node.NodeInfo, error) {
		return nil, errors.New("nonode")
	}
	require.NoError(t, udpnet.Start())
	err = m.Start()
	require.NoError(t, err)
	defer m.Shutdown()
	require.NoError(t, udpnet2.Start())
	err = m2.Start()
	require.NoError(t, err)
	defer m2.Shutdown()

	// use loopback IP for node 2
	nd2.NodeInfo.IP = net2.IP{127, 0, 0, 1}

	m.lookuper = func(key p2pcrypto.PublicKey) (*node.NodeInfo, error) {
		return nd2.NodeInfo, nil
	}

	err = m.SendMessage(nd2.PublicKey(), test_str, []byte(test_str))

	require.NoError(t, err)
	tm := time.NewTimer(time.Second)

	select {
	case msg := <-c2:
		require.Equal(t, msg.Sender(), nd.PublicKey())
		require.Equal(t, msg.Bytes(), []byte(test_str))
	case <-tm.C:
		t.Fatal("message timeout")

	}
}
