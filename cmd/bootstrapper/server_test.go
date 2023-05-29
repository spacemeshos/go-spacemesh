package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
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
	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return got
}

func TestServer(t *testing.T) {
	t.Cleanup(launchServer(t))

	fs := afero.NewMemMapFs()
	g := NewGenerator(
		"",
		fmt.Sprintf("%s:%d", target, grpcPort),
		WithLogger(logtest.New(t)),
		WithFilesystem(fs),
	)

	epoch := types.EpochID(4)
	srv := NewServer(g, false, port,
		WithSrvFilesystem(fs),
		WithSrvLogger(logtest.New(t)),
		WithBootstrapEpoch(epoch),
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

	require.Eventually(t, func() bool {
		_, err := fs.Stat(PersistedFilename())
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)
	require.Empty(t, ch)

	data := query(t, ctx)
	verifyUpdate(t, data, epoch, hex.EncodeToString(epochBeacon(epoch).Bytes()), activeSetSize)
	cancel()
	srv.Stop(ctx)
}
