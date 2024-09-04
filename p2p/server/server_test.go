package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/p2p/peerinfo"
)

type hostWrapper struct {
	host.Host
	pi peerinfo.PeerInfo
}

var _ Host = &hostWrapper{}

func (hw *hostWrapper) PeerInfo() peerinfo.PeerInfo {
	return hw.pi
}

func wrapHost(t *testing.T, h host.Host) Host {
	pt := peerinfo.NewPeerInfoTracker()
	pt.Start(h.Network())
	t.Cleanup(pt.Stop)
	return &hostWrapper{Host: h, pi: pt}
}

func TestServer(t *testing.T) {
	const limit = 1024

	mesh, err := mocknet.FullMeshConnected(4)
	require.NoError(t, err)
	proto := "test"
	request := []byte("test request")
	testErr := errors.New("test error")

	handler := func(ctx context.Context, msg []byte) ([]byte, error) {
		peerID, found := ContextPeerID(ctx)
		require.True(t, found)
		return append(msg, []byte(peerID)...), nil
	}
	errhandler := func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, testErr
	}
	opts := []Opt{
		WithTimeout(100 * time.Millisecond),
		WithLog(zaptest.NewLogger(t)),
		WithMetrics(),
	}
	client := New(
		wrapHost(t, mesh.Hosts()[0]),
		proto,
		WrapHandler(handler),
		append(opts, WithRequestSizeLimit(2*limit))...,
	)
	srv1 := New(
		wrapHost(t, mesh.Hosts()[1]),
		proto,
		WrapHandler(handler),
		append(opts, WithRequestSizeLimit(limit))...,
	)
	srv2 := New(
		wrapHost(t, mesh.Hosts()[2]),
		proto,
		WrapHandler(errhandler),
		append(opts, WithRequestSizeLimit(limit))...,
	)
	srv3 := New(
		wrapHost(t, mesh.Hosts()[3]),
		proto,
		WrapHandler(handler),
		append(opts, WithRequestSizeLimit(limit))...,
	)
	ctx, cancel := context.WithCancel(context.Background())
	noPeerID, found := ContextPeerID(ctx)
	require.Equal(t, peer.ID(""), noPeerID)
	require.False(t, found)
	var eg errgroup.Group
	eg.Go(func() error {
		return srv1.Run(ctx)
	})
	eg.Go(func() error {
		return srv2.Run(ctx)
	})
	eg.Go(func() error {
		return srv3.Run(ctx)
	})
	require.Eventually(t, func() bool {
		for _, h := range mesh.Hosts()[1:] {
			if len(h.Mux().Protocols()) == 0 {
				return false
			}
		}
		return true
	}, time.Second, 10*time.Millisecond)
	t.Cleanup(func() {
		cancel()
		eg.Wait()
	})

	t.Run("ReceiveMessage", func(t *testing.T) {
		n := srv1.NumAcceptedRequests()
		srvID := mesh.Hosts()[1].ID()
		response, err := client.Request(ctx, srvID, request)
		require.NoError(t, err)
		expResponse := append(request, []byte(mesh.Hosts()[0].ID())...)
		require.Equal(t, expResponse, response)
		srvConns := mesh.Hosts()[1].Network().ConnsToPeer(mesh.Hosts()[0].ID())
		require.NotEmpty(t, srvConns)
		require.Equal(t, n+1, srv1.NumAcceptedRequests())

		clientInfo := client.peerInfo().EnsurePeerInfo(srvID)
		require.Equal(t, 1, clientInfo.ClientStats.SuccessCount())
		require.Zero(t, clientInfo.ClientStats.FailureCount())

		serverInfo := srv1.peerInfo().EnsurePeerInfo(mesh.Hosts()[0].ID())
		require.Eventually(t, func() bool {
			return serverInfo.ServerStats.SuccessCount() == 1
		}, 10*time.Second, 10*time.Millisecond)
		require.Zero(t, serverInfo.ServerStats.FailureCount())
	})
	t.Run("ReceiveNoPeerInfo", func(t *testing.T) {
		n := srv1.NumAcceptedRequests()
		srvID := mesh.Hosts()[3].ID()
		response, err := client.Request(ctx, srvID, request)
		require.NoError(t, err)
		expResponse := append(request, []byte(mesh.Hosts()[0].ID())...)
		require.Equal(t, expResponse, response)
		srvConns := mesh.Hosts()[3].Network().ConnsToPeer(mesh.Hosts()[0].ID())
		require.NotEmpty(t, srvConns)
		require.Equal(t, n+1, srv1.NumAcceptedRequests())
	})
	t.Run("ReceiveError", func(t *testing.T) {
		n := srv1.NumAcceptedRequests()
		srvID := mesh.Hosts()[2].ID()
		_, err := client.Request(ctx, srvID, request)
		var srvErr *ServerError
		require.ErrorAs(t, err, &srvErr)
		require.ErrorContains(t, err, "peer error")
		require.ErrorContains(t, err, testErr.Error())
		require.Equal(t, n+1, srv1.NumAcceptedRequests())

		clientInfo := client.peerInfo().EnsurePeerInfo(srvID)
		require.Zero(t, clientInfo.ClientStats.SuccessCount())
		require.Equal(t, 1, clientInfo.ClientStats.FailureCount())

		serverInfo := srv2.peerInfo().EnsurePeerInfo(mesh.Hosts()[0].ID())
		require.Eventually(t, func() bool {
			return serverInfo.ServerStats.FailureCount() == 1
		}, 10*time.Second, 10*time.Millisecond)
		require.Zero(t, serverInfo.ServerStats.SuccessCount())
	})
	t.Run("DialError", func(t *testing.T) {
		_, err := client.Request(ctx, mesh.Hosts()[2].ID(), request)
		require.Error(t, err)
	})
	t.Run("NotConnected", func(t *testing.T) {
		_, err := client.Request(ctx, "unknown", request)
		require.ErrorIs(t, err, ErrNotConnected)
	})
	t.Run("limit overflow", func(t *testing.T) {
		_, err := client.Request(
			ctx,
			mesh.Hosts()[2].ID(),
			make([]byte, limit+1),
		)
		require.Error(t, err)
	})
}

func Test_Queued(t *testing.T) {
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	var (
		queueSize = 10
		proto     = "test"
		stop      = make(chan struct{})
		wg        sync.WaitGroup
	)

	wg.Add(queueSize)
	client := New(wrapHost(t, mesh.Hosts()[0]), proto, nil)
	srv := New(
		wrapHost(t, mesh.Hosts()[1]),
		proto,
		WrapHandler(func(_ context.Context, msg []byte) ([]byte, error) {
			wg.Done()
			<-stop
			return msg, nil
		}),
		WithQueueSize(queueSize),
	)
	var (
		eg          errgroup.Group
		ctx, cancel = context.WithCancel(context.Background())
	)
	defer cancel()
	eg.Go(func() error {
		return srv.Run(ctx)
	})
	t.Cleanup(func() {
		assert.NoError(t, eg.Wait())
	})
	var reqEq errgroup.Group
	for i := 0; i < queueSize; i++ { // fill the queue with requests
		reqEq.Go(func() error {
			resp, err := client.Request(ctx, mesh.Hosts()[1].ID(), []byte("ping"))
			require.NoError(t, err)
			require.Equal(t, []byte("ping"), resp)
			return nil
		})
	}
	wg.Wait()

	for i := 0; i < queueSize; i++ { // queue is full, requests fail
		_, err := client.Request(ctx, mesh.Hosts()[1].ID(), []byte("ping"))
		require.Error(t, err)
	}

	close(stop)
	require.NoError(t, reqEq.Wait())
}

func Test_RequestInterval(t *testing.T) {
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	var (
		maxReq     = 10
		maxReqTime = time.Minute
		proto      = "test"
	)

	client := New(wrapHost(t, mesh.Hosts()[0]), proto, nil)
	srv := New(
		wrapHost(t, mesh.Hosts()[1]),
		proto,
		WrapHandler(func(_ context.Context, msg []byte) ([]byte, error) {
			return msg, nil
		}),
		WithRequestsPerInterval(maxReq, maxReqTime),
	)
	var (
		eg          errgroup.Group
		ctx, cancel = context.WithCancel(context.Background())
	)
	defer cancel()
	eg.Go(func() error {
		return srv.Run(ctx)
	})
	t.Cleanup(func() {
		assert.NoError(t, eg.Wait())
	})

	start := time.Now()
	for i := 0; i < maxReq; i++ { // fill the interval with requests (bursts up to maxReq are allowed)
		resp, err := client.Request(ctx, mesh.Hosts()[1].ID(), []byte("ping"))
		require.NoError(t, err)
		require.Equal(t, []byte("ping"), resp)
	}

	// new request will be delayed by the interval
	resp, err := client.Request(context.Background(), mesh.Hosts()[1].ID(), []byte("ping"))
	require.NoError(t, err)
	require.Equal(t, []byte("ping"), resp)

	interval := maxReqTime / time.Duration(maxReq)
	require.GreaterOrEqual(t, time.Since(start), interval)
}

func FuzzResponseConsistency(f *testing.F) {
	tester.FuzzConsistency[Response](f)
}

func FuzzResponseSafety(f *testing.F) {
	tester.FuzzSafety[Response](f)
}
