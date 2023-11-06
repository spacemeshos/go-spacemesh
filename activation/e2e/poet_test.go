package activation_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/spacemeshos/poet/registration"
	"github.com/spacemeshos/poet/server"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// HTTPPoetTestHarness utilizes a local self-contained poet server instance
// targeted by an HTTP client. It is intended to be used in tests only.
type HTTPPoetTestHarness struct {
	Service *server.Server
}

func (h *HTTPPoetTestHarness) RestURL() *url.URL {
	return &url.URL{
		Scheme: "http",
		Host:   h.Service.GrpcRestProxyAddr().String(),
	}
}

type HTTPPoetOpt func(*server.Config)

func WithGenesis(genesis time.Time) HTTPPoetOpt {
	return func(cfg *server.Config) {
		cfg.Genesis = server.Genesis(genesis)
	}
}

func WithEpochDuration(epoch time.Duration) HTTPPoetOpt {
	return func(cfg *server.Config) {
		cfg.Round.EpochDuration = epoch
	}
}

func WithPhaseShift(phase time.Duration) HTTPPoetOpt {
	return func(cfg *server.Config) {
		cfg.Round.PhaseShift = phase
	}
}

func WithCycleGap(gap time.Duration) HTTPPoetOpt {
	return func(cfg *server.Config) {
		cfg.Round.CycleGap = gap
	}
}

func WithCertifier(certifier *registration.CertifierConfig) HTTPPoetOpt {
	return func(cfg *server.Config) {
		cfg.Registration.Certifier = certifier
	}
}

// NewHTTPPoetTestHarness returns a new instance of HTTPPoetHarness.
func NewHTTPPoetTestHarness(ctx context.Context, poetdir string, opts ...HTTPPoetOpt) (*HTTPPoetTestHarness, error) {
	cfg := server.DefaultConfig()
	cfg.PoetDir = poetdir
	cfg.RawRESTListener = "localhost:0"
	cfg.RawRPCListener = "localhost:0"

	for _, opt := range opts {
		opt(cfg)
	}

	server.SetupConfig(cfg)

	poet, err := server.New(ctx, *cfg)
	if err != nil {
		return nil, err
	}

	return &HTTPPoetTestHarness{
		Service: poet,
	}, nil
}

func TestHTTPPoet(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var eg errgroup.Group
	poetDir := t.TempDir()
	t.Cleanup(func() { r.NoError(eg.Wait()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	certPubKey, certPrivKey, err := ed25519.GenerateKey(nil)
	r.NoError(err)

	c, err := NewHTTPPoetTestHarness(ctx, poetDir, WithCertifier(&registration.CertifierConfig{
		PubKey: registration.Base64Enc(certPubKey),
	}))
	r.NoError(err)
	r.NotNil(c)

	eg.Go(func() error {
		err := c.Service.Start(ctx)
		return errors.Join(err, c.Service.Close())
	})

	client, err := activation.NewHTTPPoetClient(
		c.RestURL().String(),
		activation.DefaultPoetConfig(),
		activation.WithLogger(zaptest.NewLogger(t)),
	)
	require.NoError(t, err)

	signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("prefix")))
	require.NoError(t, err)
	ch := types.RandomHash()

	signature := signer.Sign(signing.POET, ch.Bytes())
	prefix := bytes.Join([][]byte{signer.Prefix(), {byte(signing.POET)}}, nil)

	t.Run("submit with cert", func(t *testing.T) {
		poetRound, err := client.Submit(
			context.Background(),
			time.Time{},
			prefix,
			ch.Bytes(),
			signature,
			signer.NodeID(),
			activation.PoetAuth{
				PoetCert: &activation.PoetCert{Signature: ed25519.Sign(certPrivKey, signer.NodeID().Bytes())},
			},
		)
		require.NoError(t, err)
		require.NotNil(t, poetRound)
	})
	t.Run("return proper error code on rejected cert", func(t *testing.T) {
		_, err := client.Submit(
			context.Background(),
			time.Time{},
			prefix,
			ch.Bytes(),
			signature,
			signer.NodeID(),
			activation.PoetAuth{PoetCert: &activation.PoetCert{Signature: []byte("oops")}},
		)
		require.ErrorIs(t, err, activation.ErrUnathorized)
	})
}

func TestSubmitTooLate(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var eg errgroup.Group
	poetDir := t.TempDir()
	t.Cleanup(func() { r.NoError(eg.Wait()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewHTTPPoetTestHarness(ctx, poetDir)
	r.NoError(err)
	r.NotNil(c)

	eg.Go(func() error {
		err := c.Service.Start(ctx)
		return errors.Join(err, c.Service.Close())
	})

	client, err := activation.NewHTTPPoetClient(
		c.RestURL().String(),
		activation.DefaultPoetConfig(),
		activation.WithLogger(zaptest.NewLogger(t)),
	)
	require.NoError(t, err)

	signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("prefix")))
	require.NoError(t, err)
	ch := types.RandomHash()

	signature := signer.Sign(signing.POET, ch.Bytes())
	prefix := bytes.Join([][]byte{signer.Prefix(), {byte(signing.POET)}}, nil)

	_, err = client.Submit(
		context.Background(),
		time.Now(),
		prefix,
		ch.Bytes(),
		signature,
		signer.NodeID(),
		activation.PoetAuth{},
	)
	r.ErrorIs(err, activation.ErrInvalidRequest)
}

func TestCertifierInfo(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var eg errgroup.Group
	poetDir := t.TempDir()
	t.Cleanup(func() { r.NoError(eg.Wait()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewHTTPPoetTestHarness(ctx, poetDir, WithCertifier(&registration.CertifierConfig{
		URL:    "http://localhost:8080",
		PubKey: []byte("pubkey"),
	}))
	r.NoError(err)
	r.NotNil(c)

	eg.Go(func() error {
		err := c.Service.Start(ctx)
		return errors.Join(err, c.Service.Close())
	})

	client, err := activation.NewHTTPPoetClient(
		c.RestURL().String(),
		activation.DefaultPoetConfig(),
		activation.WithLogger(zaptest.NewLogger(t)),
	)
	require.NoError(t, err)

	info, err := client.CertifierInfo(context.Background())
	r.NoError(err)
	r.Equal("http://localhost:8080", info.URL.String())
	r.Equal([]byte("pubkey"), info.PubKey)
}

func TestNoCertifierInfo(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var eg errgroup.Group
	poetDir := t.TempDir()
	t.Cleanup(func() { r.NoError(eg.Wait()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewHTTPPoetTestHarness(ctx, poetDir)
	r.NoError(err)
	r.NotNil(c)

	eg.Go(func() error {
		err := c.Service.Start(ctx)
		return errors.Join(err, c.Service.Close())
	})

	client, err := activation.NewHTTPPoetClient(
		c.RestURL().String(),
		activation.DefaultPoetConfig(),
		activation.WithLogger(zaptest.NewLogger(t)),
	)
	require.NoError(t, err)

	_, err = client.CertifierInfo(context.Background())
	r.ErrorContains(err, "poet doesn't support certifier")
}
