package server

import (
	"context"
	"errors"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"
)

func TestServer(t *testing.T) {
	const limit = 1024
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mesh, err := mocknet.FullMeshConnected(4)
	require.NoError(t, err)
	proto := "test"
	request := []byte("test request")
	testErr := errors.New("test error")
	errch := make(chan error, 1)
	respch := make(chan []byte, 1)

	handler := func(_ context.Context, msg []byte) ([]byte, error) {
		return msg, nil
	}
	errhandler := func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, testErr
	}
	opts := []Opt{
		WithTimeout(100 * time.Millisecond),
		WithContext(ctx),
		WithRequestSizeLimit(limit),
	}
	client := New(mesh.Hosts()[0], proto, handler, opts...)
	_ = New(mesh.Hosts()[1], proto, handler, opts...)
	_ = New(mesh.Hosts()[2], proto, errhandler, opts...)

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
		require.NoError(t, client.Request(ctx, mesh.Hosts()[1].ID(), request, respHandler, respErrHandler))
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for message response")
		case response := <-respch:
			require.Equal(t, request, response)
			require.NotEmpty(t, mesh.Hosts()[2].Network().ConnsToPeer(mesh.Hosts()[0].ID()))
		}
	})
	t.Run("ReceiveError", func(t *testing.T) {
		require.NoError(t, client.Request(ctx, mesh.Hosts()[2].ID(), request, respHandler, respErrHandler))
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for error response")
		case err := <-errch:
			require.Equal(t, testErr, err)
		}
	})
	t.Run("DialError", func(t *testing.T) {
		require.NoError(t, client.Request(ctx, mesh.Hosts()[3].ID(), request, respHandler, respErrHandler))
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for dial error")
		case err := <-errch:
			require.Error(t, err)
		}
	})
	t.Run("NotConnected", func(t *testing.T) {
		require.ErrorIs(t, client.Request(ctx, "unknown", request, respHandler, respErrHandler), ErrNotConnected)
	})
	t.Run("limit overflow", func(t *testing.T) {
		require.NoError(t, client.Request(ctx, mesh.Hosts()[2].ID(), make([]byte, limit+1), respHandler, respErrHandler))
		select {
		case <-time.After(time.Second):
			require.FailNow(t, "timed out while waiting for error response")
		case err := <-errch:
			require.Error(t, err)
		}
	})
}

func FuzzResponseConsistency(f *testing.F) {
	tester.FuzzConsistency[Response](f)
}

func FuzzResponseSafety(f *testing.F) {
	tester.FuzzSafety[Response](f)
}
