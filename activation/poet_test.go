package activation

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestHTTPPoet(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()
	r := require.New(t)

	var eg errgroup.Group

	poetDir := t.TempDir()
	t.Cleanup(func() { r.NoError(eg.Wait()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewHTTPPoetTestHarness(ctx, poetDir)
	r.NoError(err)
	r.NotNil(c)

	eg.Go(func() error {
		return c.Service.Start(ctx)
	})

	signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("prefix")))
	require.NoError(t, err)
	ch := types.RandomHash()

	signature := signer.Sign(signing.POET, ch.Bytes())
	prefix := bytes.Join([][]byte{signer.Prefix(), {byte(signing.POET)}}, nil)

	poetRound, err := c.Submit(context.Background(), prefix, ch.Bytes(), signature, signer.NodeID(), PoetPoW{})
	r.NoError(err)
	r.NotNil(poetRound)
}

func TestCheckRetry(t *testing.T) {
	t.Parallel()
	t.Run("doesn't retry on context cancellation.", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		retry, err := checkRetry(ctx, nil, nil)
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, retry)
	})
	t.Run("doesn't retry on unrecoverable error.", func(t *testing.T) {
		t.Parallel()
		retry, err := checkRetry(context.Background(), nil, &url.Error{Err: errors.New("unsupported protocol scheme")})
		require.NoError(t, err)
		require.False(t, retry)
	})
	t.Run("retries on 404 (not found).", func(t *testing.T) {
		t.Parallel()
		retry, err := checkRetry(context.Background(), &http.Response{StatusCode: http.StatusNotFound}, nil)
		require.NoError(t, err)
		require.True(t, retry)
	})
}
