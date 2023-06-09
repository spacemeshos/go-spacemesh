package main

import (
	"bytes"
	"context"
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

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

const checkpointdata = `{
"version":"https://spacemesh.io/checkpoint.schema.json.1.0",
"data":{
  "id":"snapshot-15-restore-18",
  "restore":18,
  "atxs":[{
    "id":"d1ef13c8deb8970c19af780f6ce8bbdabfad368afe219ed052fde6766e121cbb",
    "epoch":3,
    "commitmentAtx":"c146f83c2b0f670c7e34e30699536a60b6d5eb13d8f0b63adb6084872c4f3b8d",
    "vrfNonce":144,
    "numUnits":2,
    "baseTickHeight":12401,
    "tickCount":6183,
    "publicKey":"0655283aa44b67e7dcbde46be857334033f5b9af79ec269f45e8e57e7913ed21",
    "sequence":2,
    "coinbase":"000000003100000000000000000000000000000000000000"
  },{
    "id":"fcaac088afd1872cf2b4ab8f1e4c1702b3f184c4c85ead0911f6917222324cf6",
    "epoch":2,
    "commitmentAtx":"c146f83c2b0f670c7e34e30699536a60b6d5eb13d8f0b63adb6084872c4f3b8d",
    "vrfNonce":144,
    "numUnits":2,
    "baseTickHeight":6202,
    "tickCount":6199,
    "publicKey":"0655283aa44b67e7dcbde46be857334033f5b9af79ec269f45e8e57e7913ed21",
    "sequence":1,
    "coinbase":"000000003100000000000000000000000000000000000000"
  }],
  "accounts":[{
    "address":"00000000073af7bec018e8d2e379fa47df6a9fa07a6a8344",
    "balance":100000000000000000,
    "nonce":0,
    "template":"",
    "state":""
  },{
    "address":"000000000dc43c7311d28e000130edbcffd6dc230fd1542e",
    "balance":99999999999863378,
    "nonce":2,
    "template":"000000000000000000000000000000000000000000000001",
    "state":""
  }]}
}
`

func query(t *testing.T, ctx context.Context, update string) []byte {
	return queryUrl(t, ctx, fmt.Sprintf("http://localhost:%d/%s", port, update))
}

func queryCheckpoint(t *testing.T, ctx context.Context) []byte {
	return queryUrl(t, ctx, fmt.Sprintf("http://localhost:%d/checkpoint", port))
}

func queryUrl(t *testing.T, ctx context.Context, url string) []byte {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return got
}

func updateCheckpoint(t *testing.T, ctx context.Context, data []byte) {
	endpoint := fmt.Sprintf("http://localhost:%d/updateCheckpoint", port)
	formData := url.Values{"checkpoint": []string{string(data)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(formData.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
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

	epochs := []types.EpochID{types.EpochID(4), types.EpochID(5)}
	srv := NewServer(g, false, port,
		WithSrvFilesystem(fs),
		WithSrvLogger(logtest.New(t)),
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
		fname := PersistedFilename(epoch, bootstrap.SuffixBoostrap)
		require.Eventually(t, func() bool {
			_, err := fs.Stat(fname)
			return err == nil
		}, 5*time.Second, 100*time.Millisecond)
		require.Empty(t, ch)

		data := query(t, ctx, bootstrap.UpdateName(epoch, bootstrap.SuffixBoostrap))
		verifyUpdate(t, data, epoch, hex.EncodeToString(epochBeacon(epoch).Bytes()), activeSetSize)
		require.NoError(t, fs.Remove(fname))
	}

	got := queryCheckpoint(t, ctx)
	require.Empty(t, got)

	chdata := []byte(checkpointdata)
	updateCheckpoint(t, ctx, chdata)
	got = queryCheckpoint(t, ctx)
	require.True(t, bytes.Equal(chdata, got))

	cancel()
	srv.Stop(ctx)
}
