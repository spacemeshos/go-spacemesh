package activation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spacemeshos/post/initialization"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/events"
)

// DefaultPostServiceConfig returns the default config for post service.
func DefaultPostServiceConfig() PostSupervisorConfig {
	path, err := os.Executable()
	if err != nil {
		panic(err)
	}

	return PostSupervisorConfig{
		PostServiceCmd: filepath.Join(filepath.Dir(path), DefaultPostServiceName),
		NodeAddress:    "http://127.0.0.1:9093",
		MaxRetries:     10,
	}
}

// DefaultTestPostServiceConfig returns the default config for post service in tests.
func DefaultTestPostServiceConfig() PostSupervisorConfig {
	path, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		panic(err)
	}

	return PostSupervisorConfig{
		PostServiceCmd: filepath.Join(filepath.Dir(string(path)), "build", DefaultPostServiceName),
		NodeAddress:    "http://127.0.0.1:9093",
		MaxRetries:     10,
	}
}

type PostSupervisorConfig struct {
	PostServiceCmd string
	NodeAddress    string
	MaxRetries     int

	CACert string
	Cert   string
	Key    string
}

// PostSupervisor manages a local post service.
type PostSupervisor struct {
	logger *zap.Logger

	cmdCfg      PostSupervisorConfig
	postCfg     PostConfig
	provingOpts PostProvingOpts

	postSetupProvider postSetupProvider

	pid atomic.Int64 // pid of the running post service, only for tests.

	mtx  sync.Mutex         // protects fields below
	eg   errgroup.Group     // eg manages post service goroutines.
	stop context.CancelFunc // stops the running command.
}

// NewPostSupervisor returns a new post service.
func NewPostSupervisor(
	logger *zap.Logger,
	cmdCfg PostSupervisorConfig,
	postCfg PostConfig,
	provingOpts PostProvingOpts,
	postSetupProvider postSetupProvider,
) (*PostSupervisor, error) {
	if _, err := os.Stat(cmdCfg.PostServiceCmd); err != nil {
		return nil, fmt.Errorf("post service binary not found: %s", cmdCfg.PostServiceCmd)
	}

	return &PostSupervisor{
		logger:      logger,
		cmdCfg:      cmdCfg,
		postCfg:     postCfg,
		provingOpts: provingOpts,

		postSetupProvider: postSetupProvider,
	}, nil
}

func (ps *PostSupervisor) Config() PostConfig {
	return ps.postCfg
}

// Providers returns a list of available compute providers for Post setup.
func (*PostSupervisor) Providers() ([]PostSetupProvider, error) {
	providers, err := initialization.OpenCLProviders()
	if err != nil {
		return nil, err
	}

	providersAlias := make([]PostSetupProvider, len(providers))
	for i, p := range providers {
		providersAlias[i] = PostSetupProvider(p)
	}

	return providersAlias, nil
}

// Benchmark runs a short benchmarking session for a given provider to evaluate its performance.
func (*PostSupervisor) Benchmark(p PostSetupProvider) (int, error) {
	score, err := initialization.Benchmark(initialization.Provider(p))
	if err != nil {
		return score, fmt.Errorf("benchmark GPU: %w", err)
	}

	return score, nil
}

func (ps *PostSupervisor) Status() *PostSetupStatus {
	return ps.postSetupProvider.Status()
}

func (ps *PostSupervisor) Start(opts PostSetupOpts) error {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	if ps.stop != nil {
		return fmt.Errorf("post service already started")
	}

	// TODO(mafa): verify that opts don't delete existing files

	ps.eg = errgroup.Group{} // reset errgroup to allow restarts.
	ctx, stop := context.WithCancel(context.Background())
	ps.stop = stop
	ps.eg.Go(func() error {
		// If it returns any error other than context.Canceled
		// (which is how we signal it to stop) then we shutdown.
		err := ps.postSetupProvider.PrepareInitializer(ctx, opts)
		switch {
		case errors.Is(err, context.Canceled):
			return nil
		case err != nil:
			ps.logger.Fatal("preparing POST initializer failed", zap.Error(err))
			return err
		}

		err = ps.postSetupProvider.StartSession(ctx)
		switch {
		case errors.Is(err, context.Canceled):
			return nil
		case err != nil:
			ps.logger.Fatal("initialization failed", zap.Error(err))
			return err
		}

		return ps.runCmd(ctx, ps.cmdCfg, ps.postCfg, opts, ps.provingOpts)
	})
	return nil
}

// Stop stops the post service.
func (ps *PostSupervisor) Stop(deleteFiles bool) error {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	if ps.stop == nil {
		return nil
	}

	ps.stop()
	ps.stop = nil
	ps.pid.Store(0)
	err := ps.eg.Wait()
	switch {
	case err == nil || errors.Is(err, context.Canceled):
		if !deleteFiles {
			return nil
		}

		if err := ps.postSetupProvider.Reset(); err != nil {
			ps.logger.Error("failed to delete post files", zap.Error(err))
			return err
		}
		return nil
	default:
		return fmt.Errorf("stop post supervisor: %w", err)
	}
}

// captureCmdOutput returns a function that reads from the given pipe and logs the output.
// it returns when the pipe is closed.
func (ps *PostSupervisor) captureCmdOutput(pipe io.ReadCloser) func() error {
	return func() error {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			line = strings.TrimRight(line, "\r\n") // remove line delimiters at end of input
			ps.logger.Info(line)
		}
		return nil
	}
}

func (ps *PostSupervisor) runCmd(
	ctx context.Context,
	cmdCfg PostSupervisorConfig,
	postCfg PostConfig,
	postOpts PostSetupOpts,
	provingOpts PostProvingOpts,
) error {
	args := []string{
		"--address", cmdCfg.NodeAddress,

		"--min-num-units", strconv.FormatUint(uint64(postCfg.MinNumUnits), 10),
		"--max-num-units", strconv.FormatUint(uint64(postCfg.MaxNumUnits), 10),
		"--labels-per-unit", strconv.FormatUint(postCfg.LabelsPerUnit, 10),
		"--k1", strconv.FormatUint(uint64(postCfg.K1), 10),
		"--k2", strconv.FormatUint(uint64(postCfg.K2), 10),
		"--pow-difficulty", postCfg.PowDifficulty.String(),

		"--dir", postOpts.DataDir,
		"-n", strconv.FormatUint(uint64(postOpts.Scrypt.N), 10),
		"-r", strconv.FormatUint(uint64(postOpts.Scrypt.R), 10),
		"-p", strconv.FormatUint(uint64(postOpts.Scrypt.P), 10),

		"--threads", strconv.FormatUint(uint64(provingOpts.Threads), 10),
		"--nonces", strconv.FormatUint(uint64(provingOpts.Nonces), 10),
		"--randomx-mode", provingOpts.RandomXMode.String(),

		"--watch-pid", strconv.Itoa(os.Getpid()),
	}
	if cmdCfg.MaxRetries > 0 {
		args = append(args, "--max-retries", strconv.Itoa(cmdCfg.MaxRetries))
	}
	if cmdCfg.CACert != "" {
		args = append(args, "--ca-cert", cmdCfg.CACert)
	}
	if cmdCfg.Cert != "" {
		args = append(args, "--cert", cmdCfg.Cert)
	}
	if cmdCfg.Key != "" {
		args = append(args, "--key", cmdCfg.Key)
	}

	cmd := exec.CommandContext(
		ctx,
		cmdCfg.PostServiceCmd,
		args...,
	)
	pipe, err := cmd.StderrPipe()
	if err != nil {
		ps.logger.Error("setup stderr pipe for post service", zap.Error(err))
		return nil
	}

	var eg errgroup.Group
	eg.Go(ps.captureCmdOutput(pipe))
	if err := cmd.Start(); err != nil {
		pipe.Close()
		ps.logger.Error("start post service", zap.Error(err))
		return nil
	}
	ps.logger.Info("post service started", zap.Int("pid", cmd.Process.Pid), zap.String("cmd", cmd.String()))
	ps.pid.Store(int64(cmd.Process.Pid))
	events.EmitPostServiceStarted()
	err = cmd.Wait()
	if ctx.Err() != nil {
		events.EmitPostServiceStopped()
		if err := eg.Wait(); err != nil {
			ps.logger.Warn("output reading goroutine failed", zap.Error(err))
		}
		return nil
	}
	eg.Wait()
	ps.logger.Fatal("post service exited", zap.Error(err))
	return nil
}
