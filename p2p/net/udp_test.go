package net

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
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

	writeResult struct {
		n   int
		err error
	}
}

func (mc *mockCon) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
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
	local := &node.LocalNode{Node: node.GenerateRandomNodeData()}
	udpnet, err := NewUDPNet(config.DefaultConfig(), local, log.New("", "", ""))
	require.NoError(t, err)
	require.NotNil(t, udpnet)

	addr, err := net.ResolveUDPAddr("udp", local.Address())
	require.NoError(t, err)
	mockconn := &mockCon{local: addr}

	other := node.GenerateRandomNodeData()
	addr2, err := net.ResolveUDPAddr("udp", other.Address())
	require.NoError(t, err)

	mockconn.readResult = struct {
		buf  []byte
		n    int
		addr net.Addr
		err  error
	}{buf: []byte(testMsg), n: len([]byte(testMsg)), addr: addr2, err: nil}

	go udpnet.listenToUDPNetworkMessages(mockconn)

	i := 0
	for msg := range udpnet.IncomingMessages() {
		require.Equal(t, msg.FromAddr, addr2)
		require.Equal(t, msg.Message, []byte(testMsg))
		i++
		if i == 10 {
			udpnet.Shutdown()
			return
		}
	}
}
