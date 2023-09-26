package activation

import (
	"errors"
	"fmt"
	"io"
	"os"
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
		PostServiceCmd:  "./service",
		DataDir:         config.DefaultDataDir,
		NodeAddress:     "127.0.0.1:9093",
		PowDifficulty:   config.DefaultConfig().PowDifficulty,
		PostServiceMode: "light",
	}

	return cfg
}

// MainnetPostServiceConfig returns the default config for mainnet.
func MainnetPostServiceConfig() PostServiceConfig {
	cfg := PostServiceConfig{
		PostServiceCmd:  "./service",
		DataDir:         config.DefaultDataDir,
		NodeAddress:     "127.0.0.1:9093",
		PowDifficulty:   config.DefaultConfig().PowDifficulty,
		PostServiceMode: "fast",
	}

	return cfg
}

type PostServiceConfig struct {
	PostServiceCmd string `mapstructure:"post-opts-post-service"`

	DataDir         string        `mapstructure:"post-opts-datadir"`
	NodeAddress     string        `mapstructure:"post-opts-node-address"`
	PowDifficulty   PowDifficulty `mapstructure:"post-opts-pow-difficulty"`
	PostServiceMode string        `mapstructure:"post-opts-post-service-mode"` // TODO(mafa): consider enum
}

// PostService manages a local post service.
type PostService struct {
	logger *zap.Logger

	cmdMtx sync.Mutex     // protects concurrent access to cmd.
	cmd    *exec.Cmd      // cmd holds the post service command.
	eg     errgroup.Group // eg manages post service goroutines.
}

// NewPostService returns a new post service.
func NewPostService(logger *zap.Logger, opts PostServiceConfig) (*PostService, error) {
	cmd := exec.Command(opts.PostServiceCmd,
		"--dir", opts.DataDir,
		"--address", opts.NodeAddress,
		"--pow-difficulty", opts.PowDifficulty.String(),
		"--randomx-mode", opts.PostServiceMode,
	)
	cmd.Dir = filepath.Dir(opts.PostServiceCmd)
	pipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("setup stderr pipe for post service: %w", err)
	}

	ps := &PostService{
		logger: logger,
		cmd:    cmd,
	}
	ps.eg.Go(ps.captureCmdOutput(pipe))
	if err := ps.cmd.Start(); err != nil {
		pipe.Close()
		return nil, fmt.Errorf("start post service: %w", err)
	}
	ps.eg.Go(ps.monitorCmd(opts))
	return ps, nil
}

func (ps *PostService) Close() error {
	ps.cmdMtx.Lock()
	defer ps.cmdMtx.Unlock()

	err := ps.cmd.Process.Kill()
	switch {
	case err == nil:
	case errors.Is(err, os.ErrProcessDone):
	default:
		return fmt.Errorf("kill post service: %w", err)
	}

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

func (ps *PostService) monitorCmd(opts PostServiceConfig) func() error {
	return func() error {
		events.EmitPostServiceStarted()
		ps.cmd.Wait()
		events.EmitPostServiceStopped()
		if !ps.cmdMtx.TryLock() {
			// command is closing down, no need to restart.
			return nil
		}
		defer ps.cmdMtx.Unlock()

		ps.logger.Warn("post service crashed, restarting")
		ps.cmd = exec.Command(opts.PostServiceCmd,
			"--dir", opts.DataDir,
			"--address", opts.NodeAddress,
			"--pow-difficulty", opts.PowDifficulty.String(),
			"--randomx-mode", opts.PostServiceMode,
		)
		ps.cmd.Dir = filepath.Dir(opts.PostServiceCmd)
		pipe, err := ps.cmd.StderrPipe()
		if err != nil {
			ps.logger.Error("setup stderr pipe for post service", zap.Error(err))
			return nil
		}
		ps.eg.Go(ps.captureCmdOutput(pipe))
		if err := ps.cmd.Start(); err != nil {
			pipe.Close()
			ps.logger.Error("restart post service", zap.Error(err))
			return nil
		}
		ps.eg.Go(ps.monitorCmd(opts))
		return nil
	}
}
