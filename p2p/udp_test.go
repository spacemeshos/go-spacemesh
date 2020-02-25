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

	DialFunc func(address net2.Addr, remotepk p2pcrypto.PublicKey) (net.Connection, error)

	inc chan net.IncomingMessageEvent
}

func (mun *mockUDPNetwork) Start() error {
	mun.startCalled = true
	return mun.startResult
}

func (mun *mockUDPNetwork) Shutdown() {
	mun.shutdownCalled = true
}

func (mun *mockUDPNetwork) IncomingMessages() chan net.IncomingMessageEvent {
	return mun.inc
}

func (mun *mockUDPNetwork) Dial(address net2.Addr, remotepk p2pcrypto.PublicKey) (net.Connection, error) {
	if mun.DialFunc != nil {
		return mun.DialFunc(address, remotepk)
	}
	return nil, errors.New("not impl")
}

func (mun *mockUDPNetwork) SubscribeClosingConnections(func(err net.ConnectionWithErr)) {

}

func (mun *mockUDPNetwork) SubscribeOnNewRemoteConnections(func(event net.NewConnectionEvent)) {

}

func TestNewUDPMux(t *testing.T) {
	nd, _ := node.NewNodeIdentity()
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, 1, log.New("test", "", ""))
	require.NotNil(t, m)
}

func TestUDPMux_RegisterDirectProtocolWithChannel(t *testing.T) {
	nd, _ := node.NewNodeIdentity()
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, 1, log.New(test_str, "", ""))
	require.NotNil(t, m)
	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)
	c2, ok := m.messages[test_str]
	require.True(t, ok)
	require.NotNil(t, c2)
	require.Equal(t, c, c2)
}

func TestUDPMux_Start(t *testing.T) {
	nd, _ := node.NewNodeIdentity()
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, 1, log.New(test_str, "", ""))
	require.NotNil(t, m)
	err := m.Start()
	require.NoError(t, err)
}

func TestUDPMux_ProcessDirectProtocolMessage(t *testing.T) {
	nd, _ := node.NewNodeIdentity()
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, 1, log.New(test_str, "", ""))
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
	nd, _ := node.NewNodeIdentity()
	udpMock := &mockUDPNetwork{}
	sendto := node.GenerateRandomNodeData()

	var lookupcalled bool
	f := func(key p2pcrypto.PublicKey) (*node.NodeInfo, error) {
		lookupcalled = true
		return nil, errors.New("nonode")
	}

	m := NewUDPMux(nd, f, udpMock, 1, log.New(test_str, "", ""))
	require.NotNil(t, m)
	data := service.DataBytes{[]byte(test_str)}

	err := m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.Error(t, err)
	require.True(t, lookupcalled)

	lookupcalled = false
	f = func(key p2pcrypto.PublicKey) (*node.NodeInfo, error) {
		lookupcalled = true
		return sendto, nil
	}
	m.lookuper = f

	udpMock.DialFunc = func(address net2.Addr, remotepk p2pcrypto.PublicKey) (connection net.Connection, err error) {
		c := net.NewConnectionMock(remotepk)
		c.Addr = address
		return c, nil
	}

	err = m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.Equal(t, ErrNoSession, err)
	require.True(t, lookupcalled)

	lookupcalled = false
	senderr := errors.New("err send")
	m.cpool.CloseConnection(sendto.PublicKey())

	udpMock.DialFunc = func(address net2.Addr, remotepk p2pcrypto.PublicKey) (connection net.Connection, err error) {
		c := net.NewConnectionMock(remotepk)
		c.Addr = address
		c.SetSession(net.NewSessionMock(remotepk))
		c.SetSendResult(senderr)
		return c, nil
	}

	err = m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.Equal(t, senderr, err)
	require.True(t, lookupcalled)

	m.cpool.CloseConnection(sendto.PublicKey())

	udpMock.DialFunc = func(address net2.Addr, remotepk p2pcrypto.PublicKey) (connection net.Connection, err error) {
		c := net.NewConnectionMock(remotepk)
		c.Addr = address
		c.SetSession(net.NewSessionMock(remotepk))
		c.SetSendResult(nil)
		return c, nil
	}

	err = m.sendMessageImpl(sendto.PublicKey(), test_str, data)

	require.NoError(t, err)
	require.True(t, lookupcalled)
}

func TestUDPMux_ProcessUDP(t *testing.T) {
	nd, _ := node.NewNodeIdentity()
	udpMock := &mockUDPNetwork{}
	m := NewUDPMux(nd, nil, udpMock, 1, log.New(test_str, "", ""))
	require.NotNil(t, m)
	data := service.DataBytes{[]byte(test_str)}
	msg := &ProtocolMessage{}

	gotfrom := node.GenerateRandomNodeData()

	msg.Metadata = &ProtocolMessageMetadata{NextProtocol: test_str, ClientVersion: config.ClientVersion, Timestamp: time.Now().Unix(),
		NetworkID: int32(1)}
	//pb.NewUDPProtocolMessageMetadata(gotfrom.PublicKey(), int8(nd.NetworkID()), test_str)
	msg.Payload = &Payload{Payload: data.Bytes()}

	themsgbuf, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	msgbuf := p2pcrypto.PrependPubkey(themsgbuf, gotfrom.PublicKey())

	addr := &net2.UDPAddr{gotfrom.IP, int(gotfrom.DiscoveryPort), ""}

	connmock := net.NewConnectionMock(gotfrom.PublicKey())
	connmock.Addr = addr
	err = m.processUDPMessage(net.IncomingMessageEvent{connmock, msgbuf})

	require.Error(t, err) // no protocol

	c := make(chan service.DirectMessage, 1)
	m.RegisterDirectProtocolWithChannel(test_str, c)
	err = m.processUDPMessage(net.IncomingMessageEvent{connmock, msgbuf})

	require.Equal(t, err, ErrNoSession)

	session := net.NewSessionMock(gotfrom.PublicKey())
	connmock.SetSession(session)
	err = m.processUDPMessage(net.IncomingMessageEvent{connmock, msgbuf})

	require.Equal(t, err, ErrFailDecrypt)

	session.OpenMessageFunc = func(boxedMessage []byte) (bytes []byte, err error) {
		return boxedMessage, nil
	}

	err = m.processUDPMessage(net.IncomingMessageEvent{connmock, msgbuf})

	require.NoError(t, err)

	select {
	case msg := <-c:
		require.Equal(t, addr, msg.Metadata().FromAddress)
		require.Equal(t, gotfrom.PublicKey(), msg.Sender())
		require.Equal(t, data.Bytes(), msg.Bytes())
	default:
		t.Fatal("didn't get msg")
	}

}

func Test_RoundTrip(t *testing.T) {
	nd, ndinfo := node.GenerateTestNode(t)
	udpaddr := &net2.UDPAddr{ndinfo.IP, int(ndinfo.DiscoveryPort), ""}
	udpnet, err := net.NewUDPNet(config.DefaultConfig(), nd, udpaddr, log.New("", "", ""))
	require.NoError(t, err)

	m := NewUDPMux(nd, nil, udpnet, 0, log.New(test_str, "", ""))
	require.NotNil(t, m)

	nd2, ndinfo2 := node.GenerateTestNode(t)
	udpaddr2 := &net2.UDPAddr{ndinfo2.IP, int(ndinfo2.DiscoveryPort), ""}
	udpnet2, err := net.NewUDPNet(config.DefaultConfig(), nd2, udpaddr2, log.New("", "", ""))
	require.NoError(t, err)

	m2 := NewUDPMux(nd2, nil, udpnet2, 0, log.New(test_str+"2", "", ""))
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
	ndinfo2.IP = net2.IP{127, 0, 0, 1}

	m.lookuper = func(key p2pcrypto.PublicKey) (*node.NodeInfo, error) {
		return ndinfo2, nil
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
