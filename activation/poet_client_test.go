package activation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	rpcapi "github.com/spacemeshos/poet/release/proto/go/rpc/api/v1"
	"github.com/spacemeshos/poet/server"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Test_HTTPPoetClient_ParsesURL(t *testing.T) {
	cfg := server.DefaultRoundConfig()

	t.Run("add http if missing", func(t *testing.T) {
		client, err := NewHTTPPoetClient(types.PoetServer{Address: "bla"}, PoetConfig{
			PhaseShift: cfg.PhaseShift,
			CycleGap:   cfg.CycleGap,
		})
		require.NoError(t, err)
		require.Equal(t, "http://bla", client.baseURL.String())
	})

	t.Run("do not change scheme if present", func(t *testing.T) {
		client, err := NewHTTPPoetClient(types.PoetServer{Address: "https://bla"}, PoetConfig{
			PhaseShift: cfg.PhaseShift,
			CycleGap:   cfg.CycleGap,
		})
		require.NoError(t, err)
		require.Equal(t, "https://bla", client.baseURL.String())
	})
}

func Test_HTTPPoetClient_Submit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusOK)

		resp, err := protojson.Marshal(&rpcapi.SubmitResponse{})
		require.NoError(t, err)

		w.Write(resp)
		require.Equal(t, "/v1/submit", r.URL.Path)
	}))
	defer ts.Close()

	cfg := server.DefaultRoundConfig()
	client, err := NewHTTPPoetClient(types.PoetServer{Address: ts.URL}, PoetConfig{
		PhaseShift: cfg.PhaseShift,
		CycleGap:   cfg.CycleGap,
	}, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	_, err = client.Submit(
		context.Background(),
		time.Time{},
		nil,
		nil,
		types.EmptyEdSignature,
		types.NodeID{},
		PoetPoW{},
	)
	require.NoError(t, err)
}

func Test_HTTPPoetClient_Address(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)

		w.WriteHeader(http.StatusOK)
		resp, err := protojson.Marshal(&rpcapi.InfoResponse{})
		require.NoError(t, err)
		w.Write(resp)

		require.Equal(t, "/v1/info", r.URL.Path)
	}))
	defer ts.Close()

	cfg := server.DefaultRoundConfig()
	client, err := NewHTTPPoetClient(types.PoetServer{Address: ts.URL}, PoetConfig{
		PhaseShift: cfg.PhaseShift,
		CycleGap:   cfg.CycleGap,
	}, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	require.Equal(t, ts.URL, client.Address())
}

func Test_HTTPPoetClient_Address_Mainnet(t *testing.T) {
	poetCfg := server.DefaultRoundConfig()

	poETServers := []string{
		"https://mainnet-poet-0.spacemesh.network",
		"https://mainnet-poet-1.spacemesh.network",
		"https://poet-110.spacemesh.network",
		"https://poet-111.spacemesh.network",
	}

	for _, url := range poETServers {
		t.Run(url, func(t *testing.T) {
			client, err := NewHTTPPoetClient(types.PoetServer{Address: url}, PoetConfig{
				PhaseShift: poetCfg.PhaseShift,
				CycleGap:   poetCfg.CycleGap,
			})
			require.NoError(t, err)
			require.Equal(t, url, client.Address())
		})
	}
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

	cfg := server.DefaultRoundConfig()
	client, err := NewHTTPPoetClient(types.PoetServer{Address: ts.URL}, PoetConfig{
		PhaseShift: cfg.PhaseShift,
		CycleGap:   cfg.CycleGap,
	}, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	_, _, err = client.Proof(context.Background(), "1")
	require.NoError(t, err)
}

func TestPoetClient_CachesProof(t *testing.T) {
	t.Parallel()

	var proofsCalled atomic.Uint64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proofsCalled.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.Path, "/v1/proofs/")

		resp, err := protojson.Marshal(&rpcapi.ProofResponse{
			Proof: &rpcapi.PoetProof{},
		})
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		w.Write(resp)
	}))
	defer ts.Close()

	server := types.PoetServer{
		Address: ts.URL,
		Pubkey:  types.NewBase64Enc([]byte("pubkey")),
	}
	ctx := context.Background()
	db := NewMockpoetDbAPI(gomock.NewController(t))
	db.EXPECT().ValidateAndStore(ctx, gomock.Any())
	db.EXPECT().ProofForRound(server.Pubkey.Bytes(), "1").Times(19)

	poet, err := newPoetClient(db, server, DefaultPoetConfig(), zaptest.NewLogger(t))
	require.NoError(t, err)
	poet.client.client.HTTPClient = ts.Client()

	eg := errgroup.Group{}
	for range 20 {
		eg.Go(func() error {
			_, _, err := poet.Proof(ctx, "1")
			return err
		})
	}
	require.NoError(t, eg.Wait())
	require.Equal(t, uint64(1), proofsCalled.Load())

	db.EXPECT().ValidateAndStore(ctx, gomock.Any())
	_, _, err = poet.Proof(ctx, "2")
	require.NoError(t, err)
	require.Equal(t, uint64(2), proofsCalled.Load())
}
