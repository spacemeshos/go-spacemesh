package activation

import (
	"context"
	"net/url"
	"time"

	"github.com/spacemeshos/poet/config"
	"github.com/spacemeshos/poet/server"
	"github.com/spacemeshos/poet/service"
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

type HTTPPoetOpt func(*config.Config)

func WithGenesis(genesis time.Time) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.Genesis = service.Genesis(genesis)
	}
}

func WithEpochDuration(epoch time.Duration) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.EpochDuration = epoch
	}
}

func WithPhaseShift(phase time.Duration) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.PhaseShift = phase
	}
}

func WithCycleGap(gap time.Duration) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.CycleGap = gap
	}
}

// NewHTTPPoetTestHarness returns a new instance of HTTPPoetHarness.
func NewHTTPPoetTestHarness(ctx context.Context, poetdir string, opts ...HTTPPoetOpt) (*HTTPPoetTestHarness, error) {
	cfg := config.DefaultConfig()
	cfg.PoetDir = poetdir
	cfg.RawRESTListener = "localhost:0"
	cfg.RawRPCListener = "localhost:0"

	for _, opt := range opts {
		opt(cfg)
	}

	cfg, err := config.SetupConfig(cfg)
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
