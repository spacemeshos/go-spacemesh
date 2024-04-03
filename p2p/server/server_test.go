package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestServer(t *testing.T) {
	const limit = 1024

	mesh, err := mocknet.FullMeshConnected(4)
	require.NoError(t, err)
	proto := "test"
	request := []byte("test request")
	testErr := errors.New("test error")

	handler := func(_ context.Context, msg []byte) ([]byte, error) {
		return msg, nil
	}
	errhandler := func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, testErr
	}
	opts := []Opt{
		WithTimeout(100 * time.Millisecond),
		WithLog(logtest.New(t)),
		WithMetrics(),
	}
	client := New(mesh.Hosts()[0], proto, WrapHandler(handler), append(opts, WithRequestSizeLimit(2*limit))...)
	srv1 := New(mesh.Hosts()[1], proto, WrapHandler(handler), append(opts, WithRequestSizeLimit(limit))...)
	srv2 := New(mesh.Hosts()[2], proto, WrapHandler(errhandler), append(opts, WithRequestSizeLimit(limit))...)
	srv3 := New(mesh.Hosts()[3], "otherproto", WrapHandler(errhandler), append(opts, WithRequestSizeLimit(limit))...)
	ctx, cancel := context.WithCancel(context.Background())
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
		response, err := client.Request(ctx, mesh.Hosts()[1].ID(), request)
		require.NoError(t, err)
		require.Equal(t, request, response)
		require.NotEmpty(t, mesh.Hosts()[2].Network().ConnsToPeer(mesh.Hosts()[0].ID()))
		require.Equal(t, n+1, srv1.NumAcceptedRequests())
	})
	t.Run("ReceiveError", func(t *testing.T) {
		n := srv1.NumAcceptedRequests()
		_, err := client.Request(ctx, mesh.Hosts()[2].ID(), request)
		require.ErrorIs(t, err, &ServerError{})
		require.ErrorContains(t, err, "peer error")
		require.ErrorContains(t, err, testErr.Error())
		require.Equal(t, n+1, srv1.NumAcceptedRequests())
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

func TestQueued(t *testing.T) {
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	var (
		total            = 100
		proto            = "test"
		success, failure atomic.Int64
		wait             = make(chan struct{}, total)
	)

	client := New(mesh.Hosts()[0], proto, nil)
	srv := New(
		mesh.Hosts()[1],
		proto,
		WrapHandler(func(_ context.Context, msg []byte) ([]byte, error) {
			return msg, nil
		}),
		WithQueueSize(total/4),
		WithRequestsPerInterval(50, time.Second),
		WithMetrics(),
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
	for i := 0; i < total; i++ {
		eg.Go(func() error {
			if _, err := client.Request(ctx, mesh.Hosts()[1].ID(), []byte("ping")); err != nil {
				failure.Add(1)
			} else {
				success.Add(1)
			}
			wait <- struct{}{}
			return nil
		})
	}
	for i := 0; i < total; i++ {
		<-wait
	}
	require.NotZero(t, failure.Load())
	require.Greater(t, int(success.Load()), total/2)
	t.Log(success.Load())
}

func FuzzResponseConsistency(f *testing.F) {
	tester.FuzzConsistency[Response](f)
}

func FuzzResponseSafety(f *testing.F) {
	tester.FuzzSafety[Response](f)
}
