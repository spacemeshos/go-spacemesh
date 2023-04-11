package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func query(t *testing.T, ctx context.Context) []byte {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:%d", port), nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return got
}

func TestServer(t *testing.T) {
	t.Cleanup(launchServer(t))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		var content string
		if strings.HasSuffix(r.URL.String(), "/blocks/782685") {
			content = bitcoinResponse2
		} else {
			content = bitcoinResponse1
		}
		_, err := w.Write([]byte(content))
		require.NoError(t, err)
	}))
	defer ts.Close()

	fs := afero.NewMemMapFs()
	g := NewGenerator(
		ts.URL,
		fmt.Sprintf("%s:%d", target, grpcPort),
		WithLogger(logtest.New(t)),
		WithFilesystem(fs),
		WithHttpClient(ts.Client()),
	)

	srv := NewServer(fs, g, false, port, logtest.New(t))
	np := &NetworkParam{
		Genesis:      time.Now(),
		LyrsPerEpoch: 2,
		LyrDuration:  time.Second,
		Offset:       1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	srv.Start(ctx, ch, np)

	require.Eventually(t, func() bool {
		_, err := fs.Stat(PersistedFilename())
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)
	require.Empty(t, ch)

	data := query(t, ctx)
	verifyUpdate(t, data, types.EpochID(2), expectedBeacon, activeSetSize)
	cancel()
	srv.Stop(ctx)
}
