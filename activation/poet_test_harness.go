package activation

import (
	"context"
	"net/url"
	"time"

	"github.com/spacemeshos/poet/server"
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

	cfg, err := server.SetupConfig(cfg)
	if err != nil {
		return nil, err
	}

	poet, err := server.New(ctx, *cfg)
	if err != nil {
		return nil, err
	}

	return &HTTPPoetTestHarness{
		Service: poet,
	}, nil
}
