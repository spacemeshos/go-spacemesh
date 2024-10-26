package main

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

//go:embed checkpointdata.json
var checkpointdata string

func query(tb testing.TB, ctx context.Context, update string) []byte {
	return queryUrl(tb, ctx, fmt.Sprintf("http://localhost:%d/%s", port, update))
}

func queryCheckpoint(tb testing.TB, ctx context.Context) []byte {
	return queryUrl(tb, ctx, fmt.Sprintf("http://localhost:%d/checkpoint", port))
}

func queryUrl(tb testing.TB, ctx context.Context, url string) []byte {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(tb, err)
	resp, err := (&http.Client{}).Do(req)
	require.NoError(tb, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(tb, err)
	return got
}

func updateCheckpoint(tb testing.TB, ctx context.Context, data string) {
	endpoint := fmt.Sprintf("http://localhost:%d/updateCheckpoint", port)
	formData := url.Values{"checkpoint": []string{data}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(formData.Encode()))
	require.NoError(tb, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{}).Do(req)
	require.NoError(tb, err)
	resp.Body.Close()
}

func TestServer(t *testing.T) {
	db := statesql.InMemoryTest(t)
	cfg, cleanup := launchServer(t, db)
	t.Cleanup(cleanup)

	fs := afero.NewMemMapFs()
	g := NewGenerator(
		"",
		cfg.PublicListener,
		WithLogger(zaptest.NewLogger(t)),
		WithFilesystem(fs),
	)

	epochs := []types.EpochID{types.EpochID(4), types.EpochID(5)}
	srv := NewServer(g, false, port,
		WithSrvFilesystem(fs),
		WithSrvLogger(zaptest.NewLogger(t)),
		WithBootstrapEpochs(epochs),
	)
	np := &NetworkParam{
		Genesis:      time.Now(),
		LyrsPerEpoch: 2,
		LyrDuration:  100 * time.Millisecond,
		Offset:       1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	srv.Start(ctx, ch, np)

	for _, epoch := range epochs {
		createAtxs(t, db, epoch-1, types.RandomActiveSet(activeSetSize))
		fname := PersistedFilename(epoch, bootstrap.SuffixBootstrap)
		require.Eventually(t, func() bool {
			_, err := fs.Stat(fname)
			return err == nil
		}, 5*time.Second, 100*time.Millisecond)
		require.Empty(t, ch)

		data := query(t, ctx, bootstrap.UpdateName(epoch, bootstrap.SuffixBootstrap))
		verifyUpdate(t, data, epoch, hex.EncodeToString(epochBeacon(epoch).Bytes()), activeSetSize)
		require.NoError(t, fs.Remove(fname))
	}

	got := queryCheckpoint(t, ctx)
	require.Empty(t, got)

	updateCheckpoint(t, ctx, checkpointdata)
	got = queryCheckpoint(t, ctx)
	require.Equal(t, checkpointdata, string(got))

	cancel()
	srv.Stop(ctx)
}
