package activation

import (
	"context"
	"net/url"
	"time"

	"github.com/spacemeshos/poet/config"
	"github.com/spacemeshos/poet/server"
)

// HTTPPoetTestHarness utilizes a local self-contained poet server instance
// targeted by an HTTP client. It is intended to be used in tests only.
type HTTPPoetTestHarness struct {
	*HTTPPoetClient
	Service *server.Server
}

type HTTPPoetOpt func(*config.Config)

func WithGenesis(genesis time.Time) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.Genesis = genesis.Format(time.RFC3339)
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

	// NewHTTPPoetClient takes an URL as connection string. Server speaks HTTP.
	url := &url.URL{
		Scheme: "http",
		Host:   poet.GrpcRestProxyAddr().String(),
	}

	client, err := NewHTTPPoetClient(url.String(), PoetConfig{
		PhaseShift:  cfg.Service.PhaseShift,
		CycleGap:    cfg.Service.CycleGap,
		GracePeriod: cfg.Service.CycleGap / 2,
	})
	if err != nil {
		return nil, err
	}

	// TODO: query for the REST address to allow dynamic port allocation.
	// It needs changes in poet.
	return &HTTPPoetTestHarness{
		HTTPPoetClient: client,
		Service:        poet,
	}, nil
}
