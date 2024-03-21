package activation_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/spacemeshos/poet/registration"
	poetShared "github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func TestCertification(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	cfg := activation.DefaultPostConfig()
	cdb := datastore.NewCachedDB(sql.InMemory(), log.NewFromLog(logger))

	syncer := activation.NewMocksyncer(gomock.NewController(t))
	synced := make(chan struct{})
	close(synced)
	syncer.EXPECT().RegisterForATXSynced().AnyTimes().Return(synced)

	validator := activation.NewMocknipostValidator(gomock.NewController(t))
	validator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes()
	validator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	mgr, err := activation.NewPostSetupManager(cfg, logger, cdb, types.ATXID{2, 3, 4}, syncer, validator)
	require.NoError(t, err)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	initPost(t, mgr, opts, sig.NodeID())

	svc := grpcserver.NewPostService(logger)
	grpcCfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)
	t.Cleanup(launchPostSupervisor(t, logger, mgr, sig, grpcCfg, opts))

	var postClient activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		postClient, err = svc.Client(sig.NodeID())
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")
	post, info, err := postClient.Proof(context.Background(), shared.ZeroChallenge)
	require.NoError(t, err)

	poets := []activation.PoetClient{}
	// Spawn certifier and 2 poets using it
	pubKey, addr := spawnTestCertifier(t, cfg, verifying.WithLabelScryptParams(opts.Scrypt))
	certifierCfg := &registration.CertifierConfig{
		URL:    "http://" + addr.String(),
		PubKey: registration.Base64Enc(pubKey),
	}

	for i := 0; i < 2; i++ {
		poet := spawnPoet(t, WithCertifier(certifierCfg))
		address := poet.RestURL().String()
		client, err := activation.NewHTTPPoetClient(types.PoetServer{Address: address}, activation.DefaultPoetConfig())
		require.NoError(t, err)
		poets = append(poets, client)
	}

	// Spawn another certifier and 1 poet using it
	pubKey, addr = spawnTestCertifier(t, cfg, verifying.WithLabelScryptParams(opts.Scrypt))
	certifierCfg = &registration.CertifierConfig{
		URL:    "http://" + addr.String(),
		PubKey: registration.Base64Enc(pubKey),
	}

	poet := spawnPoet(t, WithCertifier(certifierCfg))
	address := poet.RestURL().String()
	client, err := activation.NewHTTPPoetClient(types.PoetServer{Address: address}, activation.DefaultPoetConfig())
	require.NoError(t, err)
	poets = append(poets, client)

	// poet not using certifier
	poet = spawnPoet(t)
	address = poet.RestURL().String()
	client, err = activation.NewHTTPPoetClient(types.PoetServer{Address: address}, activation.DefaultPoetConfig())
	require.NoError(t, err)
	poets = append(poets, client)

	certifierClient := activation.NewCertifierClient(zaptest.NewLogger(t), post, info, shared.ZeroChallenge)
	certifier := activation.NewCertifier(localsql.InMemory(), zaptest.NewLogger(t), certifierClient)
	certs := certifier.CertifyAll(context.Background(), poets)
	require.Len(t, certs, 3)
	require.Contains(t, certs, poets[0].Address())
	require.Contains(t, certs, poets[1].Address())
	require.Contains(t, certs, poets[2].Address())
}

// A testCertifier for use in tests.
// Will verify any certificate valid POST proofs.
type testCertifier struct {
	privKey      ed25519.PrivateKey
	postVerifier activation.PostVerifier
	opts         []verifying.OptionFunc
	cfg          activation.PostConfig
}

func (c *testCertifier) certify(w http.ResponseWriter, r *http.Request) {
	var req activation.CertifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest)
		return
	}

	// Verify the POST proof.
	proof := &shared.Proof{
		Nonce:   req.Proof.Nonce,
		Indices: req.Proof.Indices,
		Pow:     req.Proof.Pow,
	}
	metadata := &shared.ProofMetadata{
		NodeId:          req.Metadata.NodeId,
		CommitmentAtxId: req.Metadata.CommitmentAtxId,
		Challenge:       req.Metadata.Challenge,
		NumUnits:        req.Metadata.NumUnits,
		LabelsPerUnit:   c.cfg.LabelsPerUnit,
	}
	if err := c.postVerifier.Verify(context.Background(), proof, metadata, c.opts...); err != nil {
		http.Error(w, fmt.Sprintf("verifying POST: %v", err), http.StatusBadRequest)
		return
	}

	certData, err := poetShared.EncodeCert(&poetShared.Cert{Pubkey: req.Metadata.NodeId})
	if err != nil {
		panic(fmt.Sprintf("encoding cert: %v", err))
	}

	resp := activation.CertifyResponse{
		Certificate: certData,
		Signature:   ed25519.Sign(c.privKey, certData),
		PubKey:      c.privKey.Public().(ed25519.PublicKey),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
		return
	}
}

func spawnTestCertifier(
	t *testing.T,
	cfg activation.PostConfig,
	opts ...verifying.OptionFunc,
) (ed25519.PublicKey, net.Addr) {
	t.Helper()

	pub, private, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	postVerifier, err := activation.NewPostVerifier(
		cfg,
		zaptest.NewLogger(t),
	)
	require.NoError(t, err)
	var eg errgroup.Group
	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	eg.Go(func() error {
		certifier := &testCertifier{
			privKey:      private,
			postVerifier: postVerifier,
			opts:         opts,
			cfg:          cfg,
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/certify", certifier.certify)
		http.Serve(l, mux)
		return nil
	})
	t.Cleanup(func() {
		require.NoError(t, l.Close())
		require.NoError(t, eg.Wait())
	})

	return pub, l.Addr()
}
