package net

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/require"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

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
	udpAddr := &net.UDPAddr{net.IPv4zero, int(localinfo.DiscoveryPort), ""}
	udpnet, err := NewUDPNet(config.DefaultConfig(), local, udpAddr, log.NewDefault("TEST_"+t.Name()))
	require.NoError(t, err)
	require.NotNil(t, udpnet)

	mockconn := &mockCon{local: udpAddr}

	other, otherinfo := node.GenerateTestNode(t)
	addr2 := &net.UDPAddr{otherinfo.IP, int(otherinfo.DiscoveryPort), ""}

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
		require.Equal(t, msg.FromAddr, addr2)
		require.Equal(t, msg.From, other.PublicKey())
		require.Equal(t, msg.Message, []byte(testMsg))
		i++
		if i == 1 {
			udpnet.Shutdown()
			return
		}
	}
}
