package activation

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

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
