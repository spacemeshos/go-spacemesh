package activation

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/spacemeshos/post/config"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/events"
)

// DefaultPostServiceConfig returns the default config for post service. These are intended for testing.
func DefaultPostServiceConfig() PostServiceConfig {
	cfg := PostServiceConfig{
		PostServiceCmd:  "/bin/service",
		DataDir:         config.DefaultDataDir,
		NodeAddress:     "http://127.0.0.1:9093",
		PowDifficulty:   config.DefaultConfig().PowDifficulty,
		PostServiceMode: "light",
	}

	return cfg
}

// MainnetPostServiceConfig returns the default config for mainnet.
func MainnetPostServiceConfig() PostServiceConfig {
	cfg := PostServiceConfig{
		PostServiceCmd:  "/bin/service",
		DataDir:         config.DefaultDataDir,
		NodeAddress:     "http://127.0.0.1:9093",
		PowDifficulty:   config.MainnetConfig().PowDifficulty,
		PostServiceMode: "fast",
	}

	return cfg
}

type PostServiceConfig struct {
	PostServiceCmd string `mapstructure:"post-opts-post-service"`

	DataDir         string        `mapstructure:"post-opts-datadir"`
	NodeAddress     string        `mapstructure:"post-opts-node-address"`
	PowDifficulty   PowDifficulty `mapstructure:"post-opts-pow-difficulty"`
	PostServiceMode string        `mapstructure:"post-opts-post-service-mode"`
}

// PostService manages a local post service.
type PostService struct {
	logger *zap.Logger

	pidMtx sync.RWMutex
	pid    int // pid of the running post service.

	stop context.CancelFunc // stops the running command.
	eg   errgroup.Group     // eg manages post service goroutines.
}

// NewPostService returns a new post service.
func NewPostService(logger *zap.Logger, opts PostServiceConfig) (*PostService, error) {
	ctx, stop := context.WithCancel(context.Background())

	ps := &PostService{
		logger: logger,
		stop:   stop,
	}
	ps.eg.Go(func() error { return ps.runCmd(ctx, opts) })
	return ps, nil
}

func (ps *PostService) Close() error {
	ps.stop()
	ps.eg.Wait()
	return nil
}

// captureCmdOutput returns a function that reads from the given pipe and logs the output.
// it returns when the pipe is closed.
func (ps *PostService) captureCmdOutput(pipe io.ReadCloser) func() error {
	return func() error {
		for {
			buf := make([]byte, 1024)
			n, err := pipe.Read(buf)
			switch err {
			case nil:
			case io.EOF:
				return nil
			default:
				ps.logger.Warn("read from post service pipe", zap.Error(err))
				return nil
			}

			ps.logger.Debug(string(buf[:n]))
		}
	}
}

func (ps *PostService) runCmd(ctx context.Context, opts PostServiceConfig) error {
	for {
		cmd := exec.CommandContext(
			ctx,
			opts.PostServiceCmd,
			"--dir", opts.DataDir,
			"--address", opts.NodeAddress,
			"--pow-difficulty", opts.PowDifficulty.String(),
			"--randomx-mode", opts.PostServiceMode,
		)
		cmd.Dir = filepath.Dir(opts.PostServiceCmd)
		pipe, err := cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("setup stderr pipe for post service: %w", err)
		}

		var eg errgroup.Group
		eg.Go(ps.captureCmdOutput(pipe))
		if cmd.Start(); err != nil {
			pipe.Close()
			return fmt.Errorf("start post service: %w", err)
		}
		ps.pidMtx.Lock()
		ps.pid = cmd.Process.Pid
		ps.pidMtx.Unlock()
		events.EmitPostServiceStarted()
		err = cmd.Wait()
		if err := ctx.Err(); err != nil {
			events.EmitPostServiceStopped()
			if err := eg.Wait(); err != nil {
				ps.logger.Warn("output reading goroutine failed", zap.Error(err))
			}
			return ctx.Err()
		}
		ps.logger.Warn("post service exited", zap.Error(err))
		if err := eg.Wait(); err != nil {
			ps.logger.Warn("output reading goroutine failed", zap.Error(err))
		}
	}
}

func (ps *PostService) Pid() int {
	ps.pidMtx.RLock()
	defer ps.pidMtx.RUnlock()
	return ps.pid
}
