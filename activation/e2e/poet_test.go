package activation_test

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/spacemeshos/poet/server"
	"github.com/spacemeshos/poet/shared"
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
	c, err := NewHTTPPoetTestHarness(ctx, poetDir)
	r.NoError(err)
	r.NotNil(c)

	eg.Go(func() error {
		err := c.Service.Start(ctx)
		return errors.Join(err, c.Service.Close())
	})

	client, err := activation.NewHTTPPoetClient(
		types.PoetServer{Address: c.RestURL().String()},
		activation.DefaultPoetConfig(),
		activation.WithLogger(zaptest.NewLogger(t)),
	)
	require.NoError(t, err)

	resp, err := client.PowParams(context.Background())
	r.NoError(err)

	signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("prefix")))
	require.NoError(t, err)
	ch := types.RandomHash()

	nonce, err := shared.FindSubmitPowNonce(
		context.Background(),
		resp.Challenge,
		ch.Bytes(),
		signer.NodeID().Bytes(),
		uint(resp.Difficulty),
	)
	r.NoError(err)

	signature := signer.Sign(signing.POET, ch.Bytes())
	prefix := bytes.Join([][]byte{signer.Prefix(), {byte(signing.POET)}}, nil)

	poetRound, err := client.Submit(
		context.Background(),
		time.Time{},
		prefix,
		ch.Bytes(),
		signature,
		signer.NodeID(),
		activation.PoetPoW{
			Nonce:  nonce,
			Params: *resp,
		},
	)
	r.NoError(err)
	r.NotNil(poetRound)
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
		types.PoetServer{Address: c.RestURL().String()},
		activation.DefaultPoetConfig(),
		activation.WithLogger(zaptest.NewLogger(t)),
	)
	require.NoError(t, err)

	resp, err := client.PowParams(context.Background())
	r.NoError(err)

	signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("prefix")))
	require.NoError(t, err)
	ch := types.RandomHash()

	nonce, err := shared.FindSubmitPowNonce(
		context.Background(),
		resp.Challenge,
		ch.Bytes(),
		signer.NodeID().Bytes(),
		uint(resp.Difficulty),
	)
	r.NoError(err)

	signature := signer.Sign(signing.POET, ch.Bytes())
	prefix := bytes.Join([][]byte{signer.Prefix(), {byte(signing.POET)}}, nil)

	_, err = client.Submit(
		context.Background(),
		time.Now(),
		prefix,
		ch.Bytes(),
		signature,
		signer.NodeID(),
		activation.PoetPoW{
			Nonce:  nonce,
			Params: *resp,
		},
	)
	r.ErrorIs(err, activation.ErrInvalidRequest)
}
