package activation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	rpcapi "github.com/spacemeshos/poet/release/proto/go/rpc/api/v1"
	"github.com/spacemeshos/poet/server"
	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_HTTPPoetClient_ParsesURL(t *testing.T) {
	cfg := server.DefaultRoundConfig()

	t.Run("add http if missing", func(t *testing.T) {
		t.Parallel()
		client, err := NewHTTPPoetClient(types.PoetServer{Address: "bla"}, PoetConfig{
			PhaseShift: cfg.PhaseShift,
			CycleGap:   cfg.CycleGap,
		})
		require.NoError(t, err)
		require.Equal(t, "http://bla", client.baseURL.String())
	})

	t.Run("do not change scheme if present", func(t *testing.T) {
		t.Parallel()
		client, err := NewHTTPPoetClient(types.PoetServer{Address: "https://bla"}, PoetConfig{
			PhaseShift: cfg.PhaseShift,
			CycleGap:   cfg.CycleGap,
		})
		require.NoError(t, err)
		require.Equal(t, "https://bla", client.baseURL.String())
	})
}

func Test_HTTPPoetClient_Submit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/submit", func(w http.ResponseWriter, r *http.Request) {
		resp, err := protojson.Marshal(&rpcapi.SubmitResponse{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(resp)
	})
	ts := httptest.NewServer(mux)
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

func Test_HTTPPoetClient_SubmitTillCtxCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tries := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/submit", func(w http.ResponseWriter, r *http.Request) {
		tries += 1
		if tries == 3 {
			cancel()
		}
		http.Error(w, "some_error", http.StatusInternalServerError)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	cfg := server.DefaultRoundConfig()
	client, err := NewHTTPPoetClient(types.PoetServer{Address: ts.URL}, PoetConfig{
		PhaseShift:        cfg.PhaseShift,
		CycleGap:          cfg.CycleGap,
		MaxRequestRetries: 1,
	}, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)
	_, err = client.Submit(
		ctx,
		time.Time{},
		nil,
		nil,
		types.EmptyEdSignature,
		types.NodeID{},
		PoetAuth{},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 3, tries)
}

func Test_HTTPPoetClient_Address(t *testing.T) {
	t.Run("with scheme", func(t *testing.T) {
		t.Parallel()
		client, err := NewHTTPPoetClient(types.PoetServer{Address: "https://poet-address"}, PoetConfig{})
		require.NoError(t, err)

		require.Equal(t, "https://poet-address", client.Address())
	})
	t.Run("appends 'http://' scheme if not present", func(t *testing.T) {
		t.Parallel()
		client, err := NewHTTPPoetClient(types.PoetServer{Address: "poet-address"}, PoetConfig{})
		require.NoError(t, err)

		require.Equal(t, "http://poet-address", client.Address())
	})
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/proofs/1", func(w http.ResponseWriter, r *http.Request) {
		resp, err := protojson.Marshal(&rpcapi.ProofResponse{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(resp)
	})
	ts := httptest.NewServer(mux)
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
	var proofsCalled atomic.Uint64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/proofs/", func(w http.ResponseWriter, r *http.Request) {
		proofsCalled.Add(1)
		resp, err := protojson.Marshal(&rpcapi.ProofResponse{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(resp)
	})
	ts := httptest.NewServer(mux)
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
	cfg := PoetConfig{
		RequestTimeout: time.Millisecond * 100,
		PhaseShift:     10 * time.Second,
	}
	client := NewMockPoetClient(gomock.NewController(t))
	// first call on info returns the expected value
	client.EXPECT().Info(gomock.Any()).Return(&types.PoetInfo{
		PhaseShift: cfg.PhaseShift,
	}, nil)
	poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t))

	// any additional call on Info will block
	client.EXPECT().Proof(gomock.Any(), "1").DoAndReturn(
		func(ctx context.Context, _ string) (*types.PoetProofMessage, []types.Hash32, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		},
	).AnyTimes()

	start := time.Now()
	var eg errgroup.Group
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

	cfg := PoetConfig{RequestTimeout: time.Millisecond * 100}

	t.Run("poet supports certificate", func(t *testing.T) {
		certifierAddress := &url.URL{Scheme: "http", Host: "certifier"}
		certifierPubKey := []byte("certifier-pubkey")

		infoResp, err := protojson.Marshal(&rpcapi.InfoResponse{
			ServicePubkey: []byte("pubkey"),
			Certifier: &rpcapi.InfoResponse_Cerifier{
				Url:    certifierAddress.String(),
				Pubkey: certifierPubKey,
			},
		})
		require.NoError(t, err)

		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) { w.Write(infoResp) })
		ts := httptest.NewServer(mux)
		defer ts.Close()

		server := types.PoetServer{
			Address: ts.URL,
			Pubkey:  types.NewBase64Enc([]byte("pubkey")),
		}

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
	})

	t.Run("poet does not support certificate", func(t *testing.T) {
		infoResp, err := protojson.Marshal(&rpcapi.InfoResponse{
			ServicePubkey: []byte("pubkey"),
		})
		require.NoError(t, err)

		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) { w.Write(infoResp) })
		ts := httptest.NewServer(mux)
		defer ts.Close()

		server := types.PoetServer{
			Address: ts.URL,
			Pubkey:  types.NewBase64Enc([]byte("pubkey")),
		}

		ctrl := gomock.NewController(t)
		mCertifier := NewMockcertifierService(ctrl)

		client, err := NewHTTPPoetClient(server, cfg, withCustomHttpClient(ts.Client()))
		require.NoError(t, err)

		poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t), WithCertifier(mCertifier))
		_, err = poet.Certify(context.Background(), sig.NodeID())
		require.ErrorIs(t, err, ErrCertificatesNotSupported)
	})
}

func TestPoetClient_ObtainsCertOnSubmit(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	certifierAddress := &url.URL{Scheme: "http", Host: "certifier"}
	certifierPubKey := []byte("certifier-pubkey")
	mux := http.NewServeMux()
	infoResp, err := protojson.Marshal(&rpcapi.InfoResponse{
		ServicePubkey: []byte("pubkey"),
		Certifier: &rpcapi.InfoResponse_Cerifier{
			Url:    certifierAddress.String(),
			Pubkey: certifierPubKey,
		},
	})
	require.NoError(t, err)
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) { w.Write(infoResp) })

	submitResp, err := protojson.Marshal(&rpcapi.SubmitResponse{})
	require.NoError(t, err)
	mux.HandleFunc("POST /v1/submit", func(w http.ResponseWriter, r *http.Request) { w.Write(submitResp) })
	ts := httptest.NewServer(mux)
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

func TestCheckCertifierPublickeyHint(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	certifierAddress := &url.URL{Scheme: "http", Host: "certifier"}
	certifierPubKey := []byte("certifier-pubkey")
	infoResp, err := protojson.Marshal(&rpcapi.InfoResponse{
		ServicePubkey: []byte("pubkey"),
		Certifier: &rpcapi.InfoResponse_Cerifier{
			Url:    certifierAddress.String(),
			Pubkey: certifierPubKey,
		},
	})
	require.NoError(t, err)

	submitResp, err := protojson.Marshal(&rpcapi.SubmitResponse{})
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) { w.Write(infoResp) })

	publicKeyHint := certifierPubKey[:shared.CertPubkeyHintSize]
	mux.HandleFunc("POST /v1/submit", func(w http.ResponseWriter, r *http.Request) {
		rawReq, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return
		}

		req := &rpcapi.SubmitRequest{}
		if err := protojson.Unmarshal(rawReq, req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if !bytes.Equal(publicKeyHint, req.CertificatePubkeyHint) {
			http.Error(w, shared.ErrCertExpired.Error(), http.StatusUnauthorized)
			return
		}

		w.Write(submitResp)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	server := types.PoetServer{
		Address: ts.URL,
		Pubkey:  types.NewBase64Enc([]byte("pubkey")),
	}
	cfg := PoetConfig{RequestTimeout: time.Millisecond * 100}
	client, err := NewHTTPPoetClient(server, cfg, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	cert := certifier.PoetCert{Data: []byte("abc")}
	t.Run("public key hint is valid", func(t *testing.T) {
		_, err = client.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID(),
			PoetAuth{
				PoetCert:   &cert,
				CertPubKey: publicKeyHint,
			})
		require.NoError(t, err)
	})

	t.Run("no public key hint", func(t *testing.T) {
		_, err = client.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID(),
			PoetAuth{
				PoetCert: &cert,
			})
		require.NoError(t, err)
	})

	t.Run("public key hint is invalid", func(t *testing.T) {
		_, err = client.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID(),
			PoetAuth{
				PoetCert:   &cert,
				CertPubKey: []byte{1, 2, 3, 4, 5},
			})
		require.ErrorContains(t, err, shared.ErrCertExpired.Error())
	})
}

func TestPoetClient_RecertifiesOnAuthFailure(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	certifierAddress := &url.URL{Scheme: "http", Host: "certifier"}
	certifierPubKey := []byte("certifier-pubkey")
	submitCount := 0
	certs := make(chan []byte, 2)

	mux := http.NewServeMux()
	infoResp, err := protojson.Marshal(&rpcapi.InfoResponse{
		ServicePubkey: []byte("pubkey"),
		Certifier: &rpcapi.InfoResponse_Cerifier{
			Url:    certifierAddress.String(),
			Pubkey: certifierPubKey,
		},
	})
	require.NoError(t, err)
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) { w.Write(infoResp) })

	submitResp, err := protojson.Marshal(&rpcapi.SubmitResponse{})
	require.NoError(t, err)
	mux.HandleFunc("POST /v1/submit", func(w http.ResponseWriter, r *http.Request) {
		req := rpcapi.SubmitRequest{}
		body, _ := io.ReadAll(r.Body)
		protojson.Unmarshal(body, &req)
		certs <- req.Certificate.Data
		if submitCount == 0 {
			w.WriteHeader(http.StatusUnauthorized)
		} else {
			w.Write(submitResp)
		}
		submitCount++
	})
	ts := httptest.NewServer(mux)
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
		mCertifier.EXPECT().DeleteCertificate(sig.NodeID(), certifierPubKey),
		mCertifier.EXPECT().
			Certificate(gomock.Any(), sig.NodeID(), certifierAddress, certifierPubKey).
			Return(&certifier.PoetCert{Data: []byte("second")}, nil),
	)

	client, err := NewHTTPPoetClient(server, cfg, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)
	poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t), WithCertifier(mCertifier))

	_, err = poet.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, 2, submitCount)
	require.EqualValues(t, "first", <-certs)
	require.EqualValues(t, "second", <-certs)
}

func TestPoetClient_FallbacksToPowWhenCannotRecertify(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	certifierAddress := &url.URL{Scheme: "http", Host: "certifier"}
	certifierPubKey := []byte("certifier-pubkey")

	mux := http.NewServeMux()
	infoResp, err := protojson.Marshal(&rpcapi.InfoResponse{
		ServicePubkey: []byte("pubkey"),
		Certifier: &rpcapi.InfoResponse_Cerifier{
			Url:    certifierAddress.String(),
			Pubkey: certifierPubKey,
		},
	})
	require.NoError(t, err)
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) { w.Write(infoResp) })

	powChallenge := []byte("challenge")
	powResp, err := protojson.Marshal(&rpcapi.PowParamsResponse{PowParams: &rpcapi.PowParams{Challenge: powChallenge}})
	require.NoError(t, err)
	mux.HandleFunc("GET /v1/pow_params", func(w http.ResponseWriter, r *http.Request) { w.Write(powResp) })

	submitResp, err := protojson.Marshal(&rpcapi.SubmitResponse{})
	require.NoError(t, err)
	submitCount := 0
	mux.HandleFunc("POST /v1/submit", func(w http.ResponseWriter, r *http.Request) {
		req := rpcapi.SubmitRequest{}
		body, _ := io.ReadAll(r.Body)
		protojson.Unmarshal(body, &req)

		switch {
		case submitCount == 0:
			w.WriteHeader(http.StatusUnauthorized)
		case submitCount == 1 && req.Certificate == nil && bytes.Equal(req.PowParams.Challenge, powChallenge):
			w.Write(submitResp)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
		submitCount++
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	server := types.PoetServer{
		Address: ts.URL,
		Pubkey:  types.NewBase64Enc([]byte("pubkey")),
	}
	cfg := PoetConfig{InfoCacheTTL: time.Hour}

	ctrl := gomock.NewController(t)
	mCertifier := NewMockcertifierService(ctrl)
	gomock.InOrder(
		mCertifier.EXPECT().
			Certificate(gomock.Any(), sig.NodeID(), certifierAddress, certifierPubKey).
			Return(&certifier.PoetCert{Data: []byte("first")}, nil),
		mCertifier.EXPECT().DeleteCertificate(sig.NodeID(), certifierPubKey),
		mCertifier.EXPECT().
			Certificate(gomock.Any(), sig.NodeID(), certifierAddress, certifierPubKey).
			Return(nil, errors.New("cannot recertify")),
	)

	client, err := NewHTTPPoetClient(server, cfg, withCustomHttpClient(ts.Client()))
	require.NoError(t, err)

	poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t), WithCertifier(mCertifier))

	_, err = poet.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, 2, submitCount)
}

func TestPoetService_CachesCertifierInfo(t *testing.T) {
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
			cfg.InfoCacheTTL = tc.ttl
			db, err := NewPoetDb(statesql.InMemory(), zaptest.NewLogger(t))
			require.NoError(t, err)

			url := &url.URL{Host: "certifier.hello"}
			pubkey := []byte("pubkey")
			poetInfoResp := &types.PoetInfo{Certifier: &types.CertifierInfo{Url: url, Pubkey: pubkey}}

			client := NewMockPoetClient(gomock.NewController(t))
			client.EXPECT().Address().Return("some_addr").AnyTimes()
			client.EXPECT().Info(gomock.Any()).Return(poetInfoResp, nil)

			poet := NewPoetServiceWithClient(db, client, cfg, zaptest.NewLogger(t))

			if tc.ttl == 0 {
				client.EXPECT().Info(gomock.Any()).Times(5).Return(poetInfoResp, nil)
			}

			for range 5 {
				info, err := poet.getInfo(context.Background())
				require.NoError(t, err)
				require.Equal(t, url, info.Certifier.Url)
				require.Equal(t, pubkey, info.Certifier.Pubkey)
			}
		})
	}
}

func TestPoetService_CachesPowParams(t *testing.T) {
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

			client.EXPECT().Info(gomock.Any()).Return(&types.PoetInfo{}, nil)
			client.EXPECT().Address().Return("some_address").AnyTimes()

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

func TestPoetService_FetchPoetPhaseShift(t *testing.T) {
	t.Parallel()
	const phaseShift = time.Second

	t.Run("poet service created: expected and fetched phase shift are matching",
		func(t *testing.T) {
			cfg := DefaultPoetConfig()
			cfg.PhaseShift = phaseShift

			client := NewMockPoetClient(gomock.NewController(t))
			client.EXPECT().Address().Return("some_addr").AnyTimes()
			client.EXPECT().Info(gomock.Any()).Return(&types.PoetInfo{
				PhaseShift: phaseShift,
			}, nil)

			NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t))
		})

	t.Run("poet service created: phase shift is not fetched",
		func(t *testing.T) {
			cfg := DefaultPoetConfig()
			cfg.PhaseShift = phaseShift

			client := NewMockPoetClient(gomock.NewController(t))
			client.EXPECT().Address().Return("some_addr").AnyTimes()
			client.EXPECT().Info(gomock.Any()).Return(nil, errors.New("some error"))

			NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t))
		})

	t.Run("poet service creation failed: expected and fetched phase shift are not matching",
		func(t *testing.T) {
			cfg := DefaultPoetConfig()
			cfg.PhaseShift = phaseShift

			client := NewMockPoetClient(gomock.NewController(t))
			client.EXPECT().Address().Return("some_addr").AnyTimes()
			client.EXPECT().Info(gomock.Any()).Return(&types.PoetInfo{
				PhaseShift: phaseShift * 2,
			}, nil)

			log := zaptest.NewLogger(t).WithOptions(zap.WithFatalHook(calledFatal(t)))
			NewPoetServiceWithClient(nil, client, cfg, log)
		})

	t.Run("fetch phase shift before submitting challenge: success",
		func(t *testing.T) {
			cfg := DefaultPoetConfig()
			cfg.PhaseShift = phaseShift

			client := NewMockPoetClient(gomock.NewController(t))
			client.EXPECT().Address().Return("some_addr").AnyTimes()
			client.EXPECT().Info(gomock.Any()).Return(nil, errors.New("some error"))

			poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t))
			sig, err := signing.NewEdSigner()
			require.NoError(t, err)

			client.EXPECT().Info(gomock.Any()).Return(&types.PoetInfo{PhaseShift: phaseShift}, nil)
			client.EXPECT().PowParams(gomock.Any()).Return(&PoetPowParams{}, nil)
			client.EXPECT().
				Submit(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
					gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&types.PoetRound{}, nil)

			_, err = poet.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID())
			require.NoError(t, err)
		})

	t.Run("fetch phase shift before submitting challenge: failed to fetch poet info",
		func(t *testing.T) {
			cfg := DefaultPoetConfig()
			cfg.PhaseShift = phaseShift

			client := NewMockPoetClient(gomock.NewController(t))
			client.EXPECT().Address().Return("some_addr").AnyTimes()
			client.EXPECT().Info(gomock.Any()).Return(nil, errors.New("some error"))

			poet := NewPoetServiceWithClient(nil, client, cfg, zaptest.NewLogger(t))
			sig, err := signing.NewEdSigner()
			require.NoError(t, err)

			expectedErr := errors.New("some error")
			client.EXPECT().Info(gomock.Any()).Return(nil, expectedErr)

			_, err = poet.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID())
			require.ErrorIs(t, err, expectedErr)
		})

	t.Run("fetch phase shift before submitting challenge: fetched and expected phase shift do not match",
		func(t *testing.T) {
			cfg := DefaultPoetConfig()
			cfg.PhaseShift = phaseShift

			client := NewMockPoetClient(gomock.NewController(t))
			client.EXPECT().Address().Return("some_addr").AnyTimes()
			client.EXPECT().Info(gomock.Any()).Return(nil, errors.New("some error"))

			log := zaptest.NewLogger(t).WithOptions(zap.WithFatalHook(calledFatal(t)))
			poet := NewPoetServiceWithClient(nil, client, cfg, log)
			sig, err := signing.NewEdSigner()
			require.NoError(t, err)

			client.EXPECT().Info(gomock.Any()).Return(&types.PoetInfo{
				PhaseShift: phaseShift * 2,
			}, nil)

			poet.Submit(context.Background(), time.Time{}, nil, nil, types.RandomEdSignature(), sig.NodeID())
		})
}
