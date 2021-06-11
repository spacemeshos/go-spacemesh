package net

import (
	"context"
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
	udpnet, err := NewUDPNet(context.TODO(), config.DefaultConfig(), local, log.NewDefault("TEST_"+t.Name()))
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

	go udpnet.listenToUDPNetworkMessages(context.TODO(), mockconn)

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
	udpnet.Shutdown()
}

func TestUDPNet_Dial(t *testing.T) {}

type msgConnMock struct {
	CloseFunc func() error
}

func (mcm msgConnMock) beginEventProcessing(context.Context) {
	panic("not implemented")
}

func (mcm msgConnMock) String() string {
	panic("not implemented")
}

func (mcm msgConnMock) ID() string {
	panic("not implemented")
}

func (mcm msgConnMock) RemotePublicKey() p2pcrypto.PublicKey {
	panic("not implemented")
}

func (mcm msgConnMock) SetRemotePublicKey(p2pcrypto.PublicKey) {
	panic("not implemented")
}

func (mcm msgConnMock) Created() time.Time {
	panic("not implemented")
}

func (mcm msgConnMock) RemoteAddr() net.Addr {
	panic("not implemented")
}

func (mcm msgConnMock) Session() NetworkSession {
	panic("not implemented")
}

func (mcm msgConnMock) SetSession(NetworkSession) {
	panic("not implemented")
}

func (mcm msgConnMock) Send(context.Context, []byte) error {
	panic("not implemented")
}

func (mcm msgConnMock) Close() error {
	if mcm.CloseFunc != nil {
		return mcm.CloseFunc()
	}
	return nil
}

func (mcm msgConnMock) Closed() bool {
	panic("not implemented")
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
	n, err := NewUDPNet(context.TODO(), config.DefaultConfig(), localnode, log.NewDefault(t.Name()))
	require.NoError(t, err)
	require.NotNil(t, n)
	addr2 := testUDPAddr()
	n.addConn(addr2, &udpConnMock{CreatedFunc: func() time.Time {
		return time.Now()
	}}, &MsgConnection{stopSending: make(chan struct{})})
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
		}}, &MsgConnection{stopSending: make(chan struct{})})
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
	}}, &MsgConnection{stopSending: make(chan struct{})})

	require.Len(t, n.incomingConn, maxUDPConn)

	addrx2 := testUDPAddr()
	_, ok2 := n.incomingConn[addrx2.String()]
	for ok2 {
		addrx2 = testUDPAddr()
		_, ok2 = n.incomingConn[addrx2.String()]
	}
	n.addConn(addrx2, &udpConnMock{CreatedFunc: func() time.Time {
		return time.Now()
	}}, &MsgConnection{stopSending: make(chan struct{})})

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

	n.addConn(addrxx, somecon, &MsgConnection{stopSending: make(chan struct{})})

	c, err := n.getConn(addrxx)
	require.Error(t, err)
	require.Nil(t, c)
	require.True(t, closed)
	n.Shutdown()
}

func TestUDPNet_Cache2(t *testing.T) {
	// This test fails by panic before latest fix to udp.go. it crashed the testnet.
	// instead we now give up on "Closing" from within the "connection" when there's an error since
	// we provide the errors from the level above anyway, so closing should be performed there.
	localnode, _ := node.NewNodeIdentity()
	n, err := NewUDPNet(context.TODO(), config.DefaultConfig(), localnode, log.NewDefault(t.Name()))
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
				require.False(t, closeval, "attempt to close closed channel on connection eviction")
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

	createAndRunConn := func(fs func(mock *udpConnMock), ft func(mock *msgConnMock)) {
		conn := newCon()
		if fs != nil {
			fs(conn)
		}
		addr := testUDPAddr()
		_, ok := n.incomingConn[addr.String()]
		for ok {
			addr = testUDPAddr()
			_, ok = n.incomingConn[addr.String()]
		}
		conn.RemoteAddrFunc = func() net.Addr {
			return addr
		}
		mconn := &msgConnMock{}
		if ft != nil {
			ft(mconn)
		}
		n.addConn(addr, conn, mconn)
	}

	for i := 0; i < maxUDPConn; i++ {
		createAndRunConn(nil, nil)
	}

	require.Len(t, n.incomingConn, maxUDPConn)
	createAndRunConn(nil, nil)

	// Make sure one connection was evicted and replaced
	require.Len(t, n.incomingConn, maxUDPConn)

	// This gets into the closing code path and panics if Close is called twice
	createAndRunConn(nil, nil)

	// Make sure close is called on both conn objects at eviction
	closed := 0
	closed2 := 0
	createAndRunConn(func(mock *udpConnMock) {
		mock.CreatedFunc = func() time.Time {
			return time.Now().Add(-maxUDPLife * 2)
		}

		require.True(t, time.Since(mock.Created()) > maxUDPLife)

		mock.CloseFunc = func() error {
			closed++
			return nil
		}
	}, func(mock *msgConnMock) {
		mock.CloseFunc = func() error {
			closed2++
			return nil
		}
	})

	// this gets into the closing code path of older than 24 hours
	createAndRunConn(nil, nil)
	require.Equal(t, 1, closed)
	require.Equal(t, 1, closed2)
	n.Shutdown()
}

func Test_UDPIncomingConnClose(t *testing.T) {
	localnode, _ := node.NewNodeIdentity()
	remotenode, _ := node.NewNodeIdentity()
	n, err := NewUDPNet(context.TODO(), config.DefaultConfig(), localnode, log.NewDefault(t.Name()+"_local"))
	require.NoError(t, err)
	require.NotNil(t, n)

	remoten, rerr := NewUDPNet(context.TODO(), config.DefaultConfig(), remotenode, log.NewDefault(t.Name()+"_remote"))
	require.NoError(t, rerr)
	require.NotNil(t, remoten)

	ns := remoten.cache.GetOrCreate(localnode.PublicKey())
	data := []byte("random message")
	sealed := ns.SealMessage(data)
	final := p2pcrypto.PrependPubkey(sealed, n.local.PublicKey())

	addr := testUDPAddr()

	donech := make(chan struct{})

	lis := &udpConnMock{
		ReadFromFunc: func(p []byte) (int, net.Addr, error) {
			copy(p, final)
			donech <- struct{}{}
			return len(final), addr, nil
		},
		CloseFunc: func() error {
			t.Fatal("Closed the listener")
			return nil
		},
	}
	n.conn = lis

	go n.listenToUDPNetworkMessages(context.TODO(), lis)
	<-donech
	<-donech
	conn := n.incomingConn[addr.String()]
	conn.Close()
	time.Sleep(1 * time.Second)
}
