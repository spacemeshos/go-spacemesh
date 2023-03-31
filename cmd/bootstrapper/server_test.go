package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

const (
	update1 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "id": 1,
    "epochs": [
      {
        "epoch": 0,
        "beacon": "6fe7c971"
      },
      {
        "epoch": 1,
        "beacon": "6fe7c971",
		"activeSet": [
		  "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210",
		  "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"]
      }
    ]
  }
}
`

	update2 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "id": 2,
    "epochs": [
      {
        "epoch": 1,
        "beacon": "6fe7c971",
        "activeSet": [
		  "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210",
		  "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"]
      },
      {
        "epoch": 2,
        "beacon": "f70cf90b",
        "activeSet": [
		  "0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
		  "23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
      }
    ]
  }
}
`

	update3 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "id": 3,
    "epochs": [
      {
        "epoch": 2,
        "beacon": "f70cf90b",
        "activeSet": [
          "0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
          "23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
      },
      {
        "epoch": 3,
        "beacon": "9ef76b65",
        "activeSet": [
          "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64",
          "e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd",
          "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"]
      }
    ]
  }
}
`
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
	fs := afero.NewMemMapFs()
	srv := NewServer(fs, port, logtest.New(t))
	require.NoError(t, srv.Start())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, afero.WriteFile(fs, epochFile(1), []byte(update1), 0o400))
	require.NoError(t, afero.WriteFile(fs, epochFile(2), []byte(update2), 0o400))
	require.NoError(t, afero.WriteFile(fs, epochFile(3), []byte(update3), 0o400))

	require.Equal(t, []byte(update3), query(t, ctx))

	// change file content on disk we should still get the cached data
	require.NoError(t, afero.WriteFile(fs, epochFile(3), []byte(update1), 0o400))
	require.Equal(t, []byte(update3), query(t, ctx))

	// but removing epoch 3 file will cause the latest update to be epoch 2
	require.NoError(t, fs.Remove(epochFile(3)))
	require.Equal(t, []byte(update2), query(t, ctx))
	require.NoError(t, srv.Stop(ctx))
}
