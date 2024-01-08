package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/spacemeshos/go-scale/tester"
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
	errch := make(chan error, 1)
	respch := make(chan []byte, 1)

	handler := Handler(func(_ context.Context, msg []byte) ([]byte, error) {
		return msg, nil
	})
	errhandler := Handler(func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, testErr
	})
	opts := []Opt{
		WithTimeout(100 * time.Millisecond),
		WithLog(logtest.New(t)),
	}
	client := New(mesh.Hosts()[0], proto, handler, append(opts, WithRequestSizeLimit(2*limit))...)
	srv1 := New(mesh.Hosts()[1], proto, handler, append(opts, WithRequestSizeLimit(limit))...)
	srv2 := New(mesh.Hosts()[2], proto, errhandler, append(opts, WithRequestSizeLimit(limit))...)
	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		return srv1.Run(ctx)
	})
	eg.Go(func() error {
		return srv2.Run(ctx)
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
	respHandler := func(msg []byte) {
		select {
		case <-ctx.Done():
		case respch <- msg:
		}
	}
	respErrHandler := func(err error) {
		select {
		case <-ctx.Done():
		case errch <- err:
		}
	}
	t.Run("ReceiveMessage", func(t *testing.T) {
		require.NoError(
			t,
			client.Request(ctx, mesh.Hosts()[1].ID(), request, respHandler, respErrHandler),
		)
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for message response")
		case response := <-respch:
			require.Equal(t, request, response)
			require.NotEmpty(t, mesh.Hosts()[2].Network().ConnsToPeer(mesh.Hosts()[0].ID()))
		}
	})
	t.Run("ReceiveError", func(t *testing.T) {
		require.NoError(
			t,
			client.Request(ctx, mesh.Hosts()[2].ID(), request, respHandler, respErrHandler),
		)
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for error response")
		case err := <-errch:
			require.Equal(t, testErr, err)
		}
	})
	t.Run("DialError", func(t *testing.T) {
		require.NoError(
			t,
			client.Request(ctx, mesh.Hosts()[3].ID(), request, respHandler, respErrHandler),
		)
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for dial error")
		case err := <-errch:
			require.Error(t, err)
		}
	})
	t.Run("NotConnected", func(t *testing.T) {
		require.ErrorIs(
			t,
			client.Request(ctx, "unknown", request, respHandler, respErrHandler),
			ErrNotConnected,
		)
	})
	t.Run("limit overflow", func(t *testing.T) {
		require.NoError(
			t,
			client.Request(
				ctx,
				mesh.Hosts()[2].ID(),
				make([]byte, limit+1),
				respHandler,
				respErrHandler,
			),
		)
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for error response")
		case err := <-errch:
			require.Error(t, err)
		}
	})
}

func TestInteractive(t *testing.T) {
	const limit = 1024

	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)
	proto := "itest"
	errch := make(chan error, 1)
	respch := make(chan []string, 1)

	handler := InteractiveHandler(func(ctx context.Context, i Interactor) (time.Duration, error) {
		hi, err := i.Receive()
		if err != nil {
			return 0, err
		}
		if string(hi) != "<hi>" {
			return 0, fmt.Errorf("server: bad greeting message: %q", hi)
		}
		if err := i.Send([]byte("<hi>")); err != nil {
			return 0, err
		}

		var b bytes.Buffer
		for {
			msg, err := i.Receive()
			if err != nil {
				return 0, err
			}
			m := string(msg)
			if m == "<end>" {
				break
			} else if m == "<hi>" {
				retErr := errors.New("duplicate <hi>")
				if err := i.SendError(retErr); err != nil {
					return 0, err
				}
				return 0, retErr
			} else {
				b.WriteString("+" + m)
			}
		}

		for _, s := range []string{b.String(), "foo", "bar", "baz", "<end>"} {
			if err := i.Send([]byte(s)); err != nil {
				return 0, err
			}
		}

		return 0, nil
	})
	opts := []Opt{
		WithTimeout(100 * time.Millisecond),
		WithLog(logtest.New(t)),
	}
	client := New(mesh.Hosts()[0], proto, handler, append(opts, WithRequestSizeLimit(2*limit))...)
	srv1 := New(mesh.Hosts()[1], proto, handler, append(opts, WithRequestSizeLimit(limit))...)
	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		return srv1.Run(ctx)
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
	makeRespHandler := func(strs ...string) InteractiveHandler {
		return func(ctx context.Context, i Interactor) (time.Duration, error) {
			hi, err := i.Receive()
			if err != nil {
				return 0, err
			}
			if string(hi) != "<hi>" {
				return 0, fmt.Errorf("client: bad greeting message: %q", hi)
			}

			for _, s := range append(strs, "<end>") {
				if err := i.Send([]byte(s)); err != nil {
					return 0, err
				}
			}

			var strs []string
			for {
				msg, err := i.Receive()
				if err != nil {
					return 0, err
				}
				m := string(msg)
				switch m {
				case "<end>":
					respch <- strs
					return 0, nil
				case "<hi>":
					return 0, errors.New("duplicate <hi>")
				default:
					strs = append(strs, m)
				}
			}
		}
	}
	respErrHandler := func(err error) {
		select {
		case <-ctx.Done():
		case errch <- err:
		}
	}
	initReq := []byte("<hi>")

	t.Run("ReceiveMessage", func(t *testing.T) {
		respHandler := makeRespHandler("abc", "def", "ghi")
		require.NoError(
			t,
			client.InteractiveRequest(ctx, mesh.Hosts()[1].ID(), initReq, respHandler, respErrHandler),
		)
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for interaction to finish")
		case strs := <-respch:
			require.Equal(t, []string{"+abc+def+ghi", "foo", "bar", "baz"}, strs)
		case err := <-errch:
			require.Fail(t, "unexpected error", "%v", err)
		}
	})

	t.Run("ReceiveError", func(t *testing.T) {
		respHandler := makeRespHandler("abc", "def", "<hi>")
		require.NoError(
			t,
			client.InteractiveRequest(ctx, mesh.Hosts()[1].ID(), initReq, respHandler, respErrHandler),
		)
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for error response")
		case <-respch:
			require.FailNow(t, "got unexpected response")
		case err := <-errch:
			require.ErrorContains(t, err, "peer reported an error: duplicate <hi>")
		}
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
		Handler(func(_ context.Context, msg []byte) ([]byte, error) {
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
		eg.Wait()
	})
	for i := 0; i < total; i++ {
		require.NoError(t, client.Request(ctx, mesh.Hosts()[1].ID(), []byte("ping"),
			func(b []byte) {
				success.Add(1)
				wait <- struct{}{}
			}, func(err error) {
				failure.Add(1)
				wait <- struct{}{}
			},
		))
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
