package activation

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
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
		PoetAuth{},
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

	client, err := NewHTTPPoetClient(server, DefaultPoetConfig(), withCustomHttpClient(ts.Client()))
	require.NoError(t, err)
	poet := NewPoetServiceWithClient(db, client, DefaultPoetConfig(), zaptest.NewLogger(t))

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

func TestPoetClient_QueryProofTimeout(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer ts.Close()
	defer close(block)

	server := types.PoetServer{
		Address: ts.URL,
		Pubkey:  types.NewBase64Enc([]byte("pubkey")),
	}
	cfg := PoetConfig{
		RequestTimeout: time.Millisecond * 100,
	}
	client, err := NewHTTPPoetClient(server, cfg, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)
	poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t))

	start := time.Now()
	eg := errgroup.Group{}
	for range 50 {
		eg.Go(func() error {
			_, _, err := poet.Proof(context.Background(), "1")
			require.ErrorIs(t, err, context.DeadlineExceeded)
			return nil
		})
	}
	eg.Wait()
	require.WithinDuration(t, start.Add(cfg.RequestTimeout), time.Now(), time.Millisecond*300)
}

func TestPoetClient_Certify(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	certifierAddress := &url.URL{Scheme: "http", Host: "certifier"}
	certifierPubKey := []byte("certifier-pubkey")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/info":
			resp, err := protojson.Marshal(&rpcapi.InfoResponse{
				ServicePubkey: []byte("pubkey"),
				Certifier: &rpcapi.InfoResponse_Cerifier{
					Url:    certifierAddress.String(),
					Pubkey: certifierPubKey,
				},
			})
			require.NoError(t, err)
			w.Write(resp)
		}
	}))
	defer ts.Close()

	server := types.PoetServer{
		Address: ts.URL,
		Pubkey:  types.NewBase64Enc([]byte("pubkey")),
	}
	cfg := PoetConfig{RequestTimeout: time.Millisecond * 100}
	cert := certifier.PoetCert{Data: []byte("abc")}
	ctrl := gomock.NewController(t)
	mCertifier := NewMockcertifierService(ctrl)
	mCertifier.EXPECT().
		Certificate(gomock.Any(), sig.NodeID(), certifierAddress, certifierPubKey).
		Return(&cert, nil)

	client, err := NewHTTPPoetClient(server, cfg, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)
	poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t), WithCertifier(mCertifier))

	got, err := poet.Certify(context.Background(), sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, cert, *got)
}

func TestPoetClient_ObtainsCertOnSubmit(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	certifierAddress := &url.URL{Scheme: "http", Host: "certifier"}
	certifierPubKey := []byte("certifier-pubkey")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/info":
			resp, err := protojson.Marshal(&rpcapi.InfoResponse{
				ServicePubkey: []byte("pubkey"),
				Certifier: &rpcapi.InfoResponse_Cerifier{
					Url:    certifierAddress.String(),
					Pubkey: certifierPubKey,
				},
			})
			require.NoError(t, err)
			w.Write(resp)
		case "/v1/submit":
			resp, err := protojson.Marshal(&rpcapi.SubmitResponse{})
			require.NoError(t, err)
			w.Write(resp)
		}
	}))

	defer ts.Close()

	server := types.PoetServer{
		Address: ts.URL,
		Pubkey:  types.NewBase64Enc([]byte("pubkey")),
	}
	cfg := PoetConfig{RequestTimeout: time.Millisecond * 100}
	cert := certifier.PoetCert{Data: []byte("abc")}
	ctrl := gomock.NewController(t)
	mCertifier := NewMockcertifierService(ctrl)
	mCertifier.EXPECT().
		Certificate(gomock.Any(), sig.NodeID(), certifierAddress, certifierPubKey).
		Return(&cert, nil)

	client, err := NewHTTPPoetClient(server, cfg, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)
	poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t), WithCertifier(mCertifier))

	_, err = poet.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID())
	require.NoError(t, err)
}

func TestPoetClient_RecertifiesOnAuthFailure(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	certifierAddress := &url.URL{Scheme: "http", Host: "certifier"}
	certifierPubKey := []byte("certifier-pubkey")
	submitCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/info":
			resp, err := protojson.Marshal(&rpcapi.InfoResponse{
				ServicePubkey: []byte("pubkey"),
				Certifier: &rpcapi.InfoResponse_Cerifier{
					Url:    certifierAddress.String(),
					Pubkey: certifierPubKey,
				},
			})
			require.NoError(t, err)
			w.Write(resp)
		case "/v1/submit":
			req := rpcapi.SubmitRequest{}
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.NoError(t, protojson.Unmarshal(body, &req))
			if submitCount == 0 {
				require.EqualValues(t, "first", req.Certificate.Data)
				w.WriteHeader(http.StatusUnauthorized)
			} else {
				require.EqualValues(t, "second", req.Certificate.Data)
				resp, err := protojson.Marshal(&rpcapi.SubmitResponse{})
				require.NoError(t, err)
				w.Write(resp)
			}
			submitCount++
		}
	}))

	defer ts.Close()

	server := types.PoetServer{
		Address: ts.URL,
		Pubkey:  types.NewBase64Enc([]byte("pubkey")),
	}
	cfg := PoetConfig{RequestTimeout: time.Millisecond * 100}

	ctrl := gomock.NewController(t)
	mCertifier := NewMockcertifierService(ctrl)
	gomock.InOrder(
		mCertifier.EXPECT().
			Certificate(gomock.Any(), sig.NodeID(), certifierAddress, certifierPubKey).
			Return(&certifier.PoetCert{Data: []byte("first")}, nil),
		mCertifier.EXPECT().
			Recertify(gomock.Any(), sig.NodeID(), certifierAddress, certifierPubKey).
			Return(&certifier.PoetCert{Data: []byte("second")}, nil),
	)

	client, err := NewHTTPPoetClient(server, cfg, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)
	poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t), WithCertifier(mCertifier))

	_, err = poet.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, 2, submitCount)
}

func TestPoetService_CachesCertifierInfo(t *testing.T) {
	t.Parallel()
	type test struct {
		name string
		ttl  time.Duration
	}
	for _, tc := range []test{
		{name: "cache enabled", ttl: time.Hour},
		{name: "cache disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultPoetConfig()
			cfg.CertifierInfoCacheTTL = tc.ttl
			client := NewMockPoetClient(gomock.NewController(t))
			db := NewPoetDb(sql.InMemory(), zaptest.NewLogger(t))
			poet := NewPoetServiceWithClient(db, client, cfg, zaptest.NewLogger(t))
			url := &url.URL{Host: "certifier.hello"}
			pubkey := []byte("pubkey")
			exp := client.EXPECT().CertifierInfo(gomock.Any()).Return(url, pubkey, nil)
			if tc.ttl == 0 {
				exp.Times(5)
			}
			for range 5 {
				gotUrl, gotPubkey, err := poet.getCertifierInfo(context.Background())
				require.NoError(t, err)
				require.Equal(t, url, gotUrl)
				require.Equal(t, pubkey, gotPubkey)
			}
		})
	}
}

func TestPoetService_CachesPowParams(t *testing.T) {
	t.Parallel()
	type test struct {
		name string
		ttl  time.Duration
	}
	for _, tc := range []test{
		{name: "cache enabled", ttl: time.Hour},
		{name: "cache disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultPoetConfig()
			cfg.PowParamsCacheTTL = tc.ttl
			client := NewMockPoetClient(gomock.NewController(t))
			poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t))

			params := PoetPowParams{
				Challenge:  types.RandomBytes(10),
				Difficulty: 8,
			}
			exp := client.EXPECT().PowParams(gomock.Any()).Return(&params, nil)
			if tc.ttl == 0 {
				exp.Times(5)
			}
			for range 5 {
				got, err := poet.powParams(context.Background())
				require.NoError(t, err)
				require.Equal(t, params, *got)
			}
		})
	}
}
