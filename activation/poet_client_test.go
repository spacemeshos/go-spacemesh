package activation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spacemeshos/poet/config"
	rpcapi "github.com/spacemeshos/poet/release/proto/go/rpc/api/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func Test_HTTPPoetClient_ParsesURL(t *testing.T) {
	cfg := config.DefaultConfig()

	t.Run("add http if missing", func(t *testing.T) {
		client, err := NewHTTPPoetClient("bla", PoetConfig{
			PhaseShift: cfg.Service.PhaseShift,
			CycleGap:   cfg.Service.CycleGap,
		})
		require.NoError(t, err)
		require.Equal(t, "http://bla", client.baseURL.String())
	})

	t.Run("do not change scheme if present", func(t *testing.T) {
		client, err := NewHTTPPoetClient("https://bla", PoetConfig{
			PhaseShift: cfg.Service.PhaseShift,
			CycleGap:   cfg.Service.CycleGap,
		})
		require.NoError(t, err)
		require.Equal(t, "https://bla", client.baseURL.String())
	})
}

func Test_HTTPPoetClient_Submit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusOK)

		resp, err := protojson.Marshal(&rpcapi.SubmitResponse{
			Hash: make([]byte, 32),
		})
		require.NoError(t, err)

		w.Write(resp)
		require.Equal(t, "/v1/submit", r.URL.Path)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	client, err := NewHTTPPoetClient(ts.URL, PoetConfig{
		PhaseShift: cfg.Service.PhaseShift,
		CycleGap:   cfg.Service.CycleGap,
	}, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	_, err = client.Submit(context.Background(), nil, nil)
	require.NoError(t, err)
}

func Test_HTTPPoetClient_Proof(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)

		w.WriteHeader(http.StatusOK)
		resp, err := protojson.Marshal(&rpcapi.ProofResponse{})
		require.NoError(t, err)

		w.Write(resp)
		require.Equal(t, "/v1/proofs/1", r.URL.Path)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	client, err := NewHTTPPoetClient(ts.URL, PoetConfig{
		PhaseShift: cfg.Service.PhaseShift,
		CycleGap:   cfg.Service.CycleGap,
	}, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	_, err = client.Proof(context.Background(), "1")
	require.NoError(t, err)
}

func Test_HTTPPoetClient_Start(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)

		w.WriteHeader(http.StatusOK)
		resp, err := protojson.Marshal(&rpcapi.StartResponse{})
		require.NoError(t, err)
		w.Write(resp)

		require.Equal(t, "/v1/start", r.URL.Path)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	client, err := NewHTTPPoetClient(ts.URL, PoetConfig{
		PhaseShift: cfg.Service.PhaseShift,
		CycleGap:   cfg.Service.CycleGap,
	}, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	err = client.Start(context.Background(), []string{})
	require.NoError(t, err)
}

func Test_HTTPPoetClient_PoetServiceID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)

		w.WriteHeader(http.StatusOK)
		resp, err := protojson.Marshal(&rpcapi.InfoResponse{})
		require.NoError(t, err)
		w.Write(resp)

		require.Equal(t, "/v1/info", r.URL.Path)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	client, err := NewHTTPPoetClient(ts.URL, PoetConfig{
		PhaseShift: cfg.Service.PhaseShift,
		CycleGap:   cfg.Service.CycleGap,
	}, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	_, err = client.PoetServiceID(context.Background())
	require.NoError(t, err)
}
