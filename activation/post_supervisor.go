package activation

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/spacemeshos/post/config"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/events"
)

// DefaultPostServiceConfig returns the default config for post service. These are intended for testing.
func DefaultPostServiceConfig() PostSupervisorConfig {
	cfg := PostSupervisorConfig{
		PostServiceCmd:  "/bin/service",
		DataDir:         config.DefaultDataDir,
		NodeAddress:     "http://127.0.0.1:9093",
		PowDifficulty:   config.DefaultConfig().PowDifficulty,
		PostServiceMode: "light",
	}

	return cfg
}

// MainnetPostServiceConfig returns the default config for mainnet.
func MainnetPostServiceConfig() PostSupervisorConfig {
	cfg := PostSupervisorConfig{
		PostServiceCmd:  "/bin/service",
		DataDir:         config.DefaultDataDir,
		NodeAddress:     "http://127.0.0.1:9093",
		PowDifficulty:   config.MainnetConfig().PowDifficulty,
		PostServiceMode: "fast",
	}

	return cfg
}

type PostSupervisorConfig struct {
	PostServiceCmd string `mapstructure:"post-opts-post-service"`

	DataDir         string `mapstructure:"post-opts-datadir"`
	NodeAddress     string `mapstructure:"post-opts-node-address"`
	PostServiceMode string `mapstructure:"post-opts-post-service-mode"`

	PowDifficulty PowDifficulty `mapstructure:"post-opts-pow-difficulty"`
	K1            uint32        `mapstructure:"post-opts-k1"`
	K2            uint32        `mapstructure:"post-opts-k2"`
	K3            uint32        `mapstructure:"post-opts-k3"`

	N uint `mapstructure:"post-opts-n"`
	R uint `mapstructure:"post-opts-r"`
	P uint `mapstructure:"post-opts-p"`
}

// PostSupervisor manages a local post service.
type PostSupervisor struct {
	logger *zap.Logger

	pid atomic.Int64 // pid of the running post service, only for tests.

	stop context.CancelFunc // stops the running command.
	eg   errgroup.Group     // eg manages post service goroutines.
}

// NewPostSupervisor returns a new post service.
func NewPostSupervisor(logger *zap.Logger, opts PostSupervisorConfig) (*PostSupervisor, error) {
	ctx, stop := context.WithCancel(context.Background())

	ps := &PostSupervisor{
		logger: logger,
		stop:   stop,
	}
	ps.eg.Go(func() error { return ps.runCmd(ctx, opts) })
	return ps, nil
}

func (ps *PostSupervisor) Close() error {
	ps.stop()
	ps.eg.Wait()
	return nil
}

// captureCmdOutput returns a function that reads from the given pipe and logs the output.
// it returns when the pipe is closed.
func (ps *PostSupervisor) captureCmdOutput(pipe io.ReadCloser) func() error {
	return func() error {
		buf := bufio.NewReader(pipe)
		for {
			line, err := buf.ReadString('\n')
			line = strings.TrimRight(line, "\r\n") // remove line delimiters at end of input
			switch err {
			case nil:
				ps.logger.Info(line)
			case io.EOF:
				ps.logger.Info(line)
				return nil
			default:
				ps.logger.Info(line)
				ps.logger.Warn("read from post service pipe", zap.Error(err))
				return nil
			}
		}
	}
}

func (ps *PostSupervisor) runCmd(ctx context.Context, opts PostSupervisorConfig) error {
	for {
		args := []string{
			"--dir", opts.DataDir,
			"--address", opts.NodeAddress,
			"--pow-difficulty", opts.PowDifficulty.String(),
			"--randomx-mode", opts.PostServiceMode,
		}
		if opts.K1 != 0 {
			args = append(args, "--k1", strconv.FormatUint(uint64(opts.K1), 10))
		}
		if opts.K2 != 0 {
			args = append(args, "--k2", strconv.FormatUint(uint64(opts.K2), 10))
		}
		if opts.K3 != 0 {
			args = append(args, "--k3", strconv.FormatUint(uint64(opts.K3), 10))
		}
		if opts.N != 0 {
			args = append(args, "-n", strconv.FormatUint(uint64(opts.N), 10))
		}
		if opts.R != 0 {
			args = append(args, "-r", strconv.FormatUint(uint64(opts.R), 10))
		}
		if opts.P != 0 {
			args = append(args, "-p", strconv.FormatUint(uint64(opts.P), 10))
		}

		cmd := exec.CommandContext(
			ctx,
			opts.PostServiceCmd,
			args...,
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
		ps.pid.Store(int64(cmd.Process.Pid))
		ps.logger.Info("post service started", zap.Int("pid", cmd.Process.Pid), zap.String("cmd", cmd.String()))
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
		eg.Wait()
	}
}
