package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/require"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

var testUDPAddr = func() *net.UDPAddr {
	port := int(rand.Int31n(48127) + 1024)
	return &net.UDPAddr{IP: IPv4LoopbackAddress, Port: port}
}

type mockCon struct {
	local      net.Addr
	readCount  int32
	readResult struct {
		buf  []byte
		n    int
		addr net.Addr
		err  error
	}

	releaseCount chan struct{}

	writeResult struct {
		n   int
		err error
	}
}

func (mc *mockCon) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	if mc.releaseCount != nil {
		<-mc.releaseCount
	}
	copy(p, mc.readResult.buf)
	atomic.AddInt32(&mc.readCount, 1)
	return mc.readResult.n, mc.readResult.addr, mc.readResult.err
}

func (mc *mockCon) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return mc.writeResult.n, mc.readResult.err
}

func (mc *mockCon) Close() error {
	return nil
}

func (mc *mockCon) LocalAddr() net.Addr {
	return mc.local
}

func (mc *mockCon) SetDeadline(t time.Time) error {
	return nil
}
func (mc *mockCon) SetReadDeadline(t time.Time) error {
	return nil
}

func (mc *mockCon) SetWriteDeadline(t time.Time) error {
	return nil
}

const testMsg = "TEST"

func TestUDPNet_Sanity(t *testing.T) {
	local, localinfo := node.GenerateTestNode(t)
	udpAddr := &net.UDPAddr{IP: net.IPv4zero, Port: int(localinfo.DiscoveryPort)}
	udpnet, err := NewUDPNet(config.DefaultConfig(), local, log.NewDefault("TEST_"+t.Name()))
	require.NoError(t, err)
	require.NotNil(t, udpnet)

	mockconn := &mockCon{local: udpAddr}

	other, otherinfo := node.GenerateTestNode(t)
	addr2 := &net.UDPAddr{IP: otherinfo.IP, Port: int(otherinfo.DiscoveryPort)}

	session := createSession(other.PrivateKey(), local.PublicKey())

	require.NoError(t, err)

	mockconn.releaseCount = make(chan struct{})

	mockconn.readResult = struct {
		buf  []byte
		n    int
		addr net.Addr
		err  error
	}{buf: []byte(testMsg), n: len([]byte(testMsg)), addr: addr2, err: nil}

	go udpnet.listenToUDPNetworkMessages(mockconn)

	mockconn.releaseCount <- struct{}{}

	sealed := session.SealMessage([]byte(testMsg))
	final := p2pcrypto.PrependPubkey(sealed, other.PublicKey())

	mockconn.readResult = struct {
		buf  []byte
		n    int
		addr net.Addr
		err  error
	}{buf: final, n: len(final), addr: addr2, err: nil}

	mockconn.releaseCount <- struct{}{}

	i := 0
	for msg := range udpnet.IncomingMessages() {
		require.Equal(t, msg.Conn.RemoteAddr(), addr2)
		require.Equal(t, msg.Conn.RemotePublicKey(), other.PublicKey())
		require.Equal(t, msg.Message, final)
		i++
		if i == 1 {
			udpnet.Shutdown()
			return
		}
	}
}

func TestUDPNet_Dial(t *testing.T) {

}

type udpConnMock struct {
	CreatedFunc          func() time.Time
	CloseFunc            func() error
	PushIncomingFunc     func(b []byte) error
	SetDeadlineFunc      func(t time.Time) error
	SetReadDeadlineFunc  func(t time.Time) error
	SetWriteDeadlineFunc func(t time.Time) error
	LocalAddrFunc        func() net.Addr
	RemoteAddrFunc       func() net.Addr
	ReadFunc             func(b []byte) (int, error)
	WriteFunc            func(b []byte) (int, error)
	WriteToUDPFunc       func(final []byte, addr *net.UDPAddr) (int, error)
	ReadFromFunc         func(p []byte) (n int, addr net.Addr, err error)
	WriteToFunc          func(p []byte, addr net.Addr) (n int, err error)
}

func (ucw *udpConnMock) WriteToUDP(final []byte, addr *net.UDPAddr) (int, error) {
	panic("implement me")
}

func (ucw *udpConnMock) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	if ucw.ReadFromFunc != nil {
		return ucw.ReadFromFunc(p)
	}
	return 0, nil, errors.New("not impl")
}

func (ucw *udpConnMock) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	panic("implement me")
}

func (ucw *udpConnMock) PushIncoming(b []byte) error {
	if ucw.PushIncomingFunc != nil {
		return ucw.PushIncomingFunc(b)
	}
	return nil
}

func (ucw *udpConnMock) SetDeadline(t time.Time) error {
	if ucw.SetDeadlineFunc != nil {
		return ucw.SetDeadline(t)
	}
	return nil
}

func (ucw *udpConnMock) SetReadDeadline(t time.Time) error {
	if ucw.SetReadDeadlineFunc != nil {
		return ucw.SetReadDeadlineFunc(t)
	}
	return nil
}

func (ucw *udpConnMock) SetWriteDeadline(t time.Time) error {
	if ucw.SetWriteDeadlineFunc != nil {
		return ucw.SetWriteDeadlineFunc(t)
	}
	return nil
}

func (ucw *udpConnMock) Created() time.Time {
	if ucw.CreatedFunc != nil {
		return ucw.CreatedFunc()
	}
	return time.Now()
}

func (ucw *udpConnMock) LocalAddr() net.Addr {
	if ucw.LocalAddrFunc != nil {
		return ucw.LocalAddrFunc()
	}
	return nil
}

func (ucw *udpConnMock) RemoteAddr() net.Addr {
	if ucw.RemoteAddrFunc != nil {
		return ucw.RemoteAddrFunc()
	}
	return nil
}

func (ucw *udpConnMock) Read(b []byte) (int, error) {
	if ucw.ReadFunc != nil {
		return ucw.ReadFunc(b)
	}
	return 0, errors.New("not impl")
}
func (ucw *udpConnMock) Write(b []byte) (int, error) {
	if ucw.WriteFunc != nil {
		return ucw.WriteFunc(b)
	}
	return 0, errors.New("not impl")
}

func (ucw *udpConnMock) Close() error {
	if ucw.CloseFunc != nil {
		return ucw.CloseFunc()
	}
	return nil
}

func TestUDPNet_Cache(t *testing.T) {
	localnode, _ := node.NewNodeIdentity()
	n, err := NewUDPNet(config.DefaultConfig(), localnode, log.NewDefault(t.Name()))
	require.NoError(t, err)
	require.NotNil(t, n)
	addr2 := testUDPAddr()
	n.addConn(addr2, &udpConnMock{CreatedFunc: func() time.Time {
		return time.Now()
	}})
	require.Len(t, n.incomingConn, 1)

	for i := 1; i < maxUDPConn-1; i++ {
		addrx := testUDPAddr()
		_, ok := n.incomingConn[addrx.String()]
		for ok {
			addrx = testUDPAddr()
			_, ok = n.incomingConn[addrx.String()]
		}
		n.addConn(addrx, &udpConnMock{CreatedFunc: func() time.Time {
			return time.Now()
		}})
	}

	require.Len(t, n.incomingConn, maxUDPConn-1)

	addrx := testUDPAddr()
	_, ok := n.incomingConn[addrx.String()]
	for ok {
		addrx = testUDPAddr()
		_, ok = n.incomingConn[addrx.String()]
	}

	n.addConn(addrx, &udpConnMock{CreatedFunc: func() time.Time {
		return time.Now().Add(-maxUDPLife - 1*time.Second)
	}})

	require.Len(t, n.incomingConn, maxUDPConn)

	addrx2 := testUDPAddr()
	_, ok2 := n.incomingConn[addrx2.String()]
	for ok2 {
		addrx2 = testUDPAddr()
		_, ok = n.incomingConn[addrx2.String()]
	}
	n.addConn(addrx2, &udpConnMock{CreatedFunc: func() time.Time {
		return time.Now()
	}})

	require.Len(t, n.incomingConn, maxUDPConn)

	i := 0
	for k := range n.incomingConn {
		delete(n.incomingConn, k)
		i++
		if i == 2 {
			break
		}
	}

	closed := false

	somecon := &udpConnMock{CreatedFunc: func() time.Time {
		return time.Now().Add(-maxUDPLife - 5*time.Second)
	}, CloseFunc: func() error {
		closed = true
		return nil
	}}

	addrxx := testUDPAddr()
	_, okk := n.incomingConn[addrxx.String()]
	for okk {
		addrxx = testUDPAddr()
		_, okk = n.incomingConn[addrxx.String()]
	}

	n.addConn(addrxx, somecon)

	c, err := n.getConn(addrxx)
	require.Error(t, err)
	require.Nil(t, c)
	require.True(t, closed)

}

func TestUDPNet_Cache2(t *testing.T) {
	// This test fails by panic before latest fix to udp.go. it crashed the testnet.
	// instead
	localnode, _ := node.NewNodeIdentity()
	n, err := NewUDPNet(config.DefaultConfig(), localnode, log.NewDefault(t.Name()))
	require.NoError(t, err)
	require.NotNil(t, n)

	newCon := func() *udpConnMock {
		tm := time.Now()
		closeval := false
		ucm := &udpConnMock{
			CreatedFunc: func() time.Time {
				return tm
			},
			CloseFunc: func() error {
				if closeval {
					panic("CLOSE TWICE")
				}
				closeval = true
				return nil
			},
			PushIncomingFunc:     nil,
			SetDeadlineFunc:      nil,
			SetReadDeadlineFunc:  nil,
			SetWriteDeadlineFunc: nil,
			LocalAddrFunc:        nil,
			RemoteAddrFunc:       nil,
			ReadFunc:             nil,
			WriteFunc:            nil,
		}
		return ucm
	}

	createAndRunConn := func() {
		pk := p2pcrypto.NewRandomPubkey()
		ns := NewSessionMock(pk)
		conn := newCon()
		addr := testUDPAddr()
		_, ok := n.incomingConn[addr.String()]
		for ok {
			addr = testUDPAddr()
			_, ok = n.incomingConn[addr.String()]
		}
		conn.RemoteAddrFunc = func() net.Addr {
			return addr
		}
		mconn := newMsgConnection(conn, n, pk, ns, n.config.MsgSizeLimit, n.config.DialTimeout, n.logger)
		n.addConn(addr, conn)
		go mconn.beginEventProcessing()
	}

	for i := 0; i < maxUDPConn; i++ {
		createAndRunConn()
	}

	require.Len(t, n.incomingConn, maxUDPConn)
	createAndRunConn()

	// Make sure one connection was evicted and replaced
	require.Len(t, n.incomingConn, maxUDPConn)
}
