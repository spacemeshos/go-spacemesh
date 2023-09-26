package activation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func initPost(tb *testing.T, log *zap.Logger, dir string) {
	tb.Helper()

	cfg := DefaultPostConfig()

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	id := sig.NodeID()

	opts := DefaultPostSetupOpts()
	opts.DataDir = dir
	opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	goldenATXID := types.ATXID{2, 3, 4}

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
	provingOpts := DefaultPostProvingOpts()
	provingOpts.Flags = config.RecommendedPowFlags()
	mgr, err := NewPostSetupManager(id, cfg, log.Named("post mgr"), cdb, goldenATXID, provingOpts)
	require.NoError(tb, err)

	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)

	var eg errgroup.Group
	lastStatus := &PostSetupStatus{}
	eg.Go(func() error {
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				status := mgr.Status()
				require.GreaterOrEqual(tb, status.NumLabelsWritten, lastStatus.NumLabelsWritten)

				if status.NumLabelsWritten == uint64(opts.NumUnits)*cfg.LabelsPerUnit {
					return nil
				}
				require.Equal(tb, PostSetupStateInProgress, status.State)
			}
		}
	})

	// Create data.
	require.NoError(tb, mgr.PrepareInitializer(context.Background(), opts))
	require.NoError(tb, mgr.StartSession(context.Background()))
	require.NoError(tb, eg.Wait())
	require.Equal(tb, PostSetupStateComplete, mgr.Status().State)
}

func Test_PostService_StartsServiceCmd(t *testing.T) {
	log := zaptest.NewLogger(t)

	postDir := t.TempDir()
	initPost(t, log.Named("post init"), postDir)

	path, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(t, err)

	opts := PostServiceConfig{
		PostServiceCmd:  filepath.Join(filepath.Dir(string(path)), "build", "service"),
		DataDir:         postDir,
		NodeAddress:     "http://127.0.0.1:12345", // node isn't listening, but also not relevant for test
		PowDifficulty:   DefaultPostConfig().PowDifficulty,
		PostServiceMode: "light",
	}

	ps, err := NewPostService(log.Named("post service"), opts)
	require.NoError(t, err)
	require.NotNil(t, ps)
	t.Cleanup(func() { assert.NoError(t, ps.Close()) })

	pid := ps.cmd.Process.Pid
	process, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.NotNil(t, process)

	if runtime.GOOS != "windows" {
		require.NoError(t, process.Signal(syscall.Signal(0))) // check if process is running
	}

	require.NoError(t, ps.Close())

	if runtime.GOOS != "windows" {
		require.Error(t, process.Signal(syscall.Signal(0))) // check if process is closed
	}
}

func Test_PostService_RestartsOnCrash(t *testing.T) {
	log := zaptest.NewLogger(t)

	postDir := t.TempDir()
	initPost(t, log.Named("post init"), postDir)

	path, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(t, err)

	opts := PostServiceConfig{
		PostServiceCmd:  filepath.Join(filepath.Dir(string(path)), "build", "service"),
		DataDir:         postDir,
		NodeAddress:     "http://127.0.0.1:12345", // node isn't listening, but also not relevant for test
		PowDifficulty:   DefaultPostConfig().PowDifficulty,
		PostServiceMode: "light",
	}

	ps, err := NewPostService(log.Named("post service"), opts)
	require.NoError(t, err)
	require.NotNil(t, ps)
	t.Cleanup(func() { assert.NoError(t, ps.Close()) })

	ps.cmd.Process.Kill()

	time.Sleep(500 * time.Millisecond)

	ps.cmdMtx.Lock()
	pid := ps.cmd.Process.Pid
	ps.cmdMtx.Unlock()
	process, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.NotNil(t, process)

	if runtime.GOOS != "windows" {
		require.NoError(t, process.Signal(syscall.Signal(0))) // check if process is running
	}

	require.NoError(t, ps.Close())
}
