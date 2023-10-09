package activation

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func Test_PostSupervisor_ErrOnMissingBinary(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := PostSupervisorConfig{
		PostServiceCmd: filepath.Join(t.TempDir(), "service"),
		NodeAddress:    "http://127.0.0.1:12345", // node isn't listening and not relevant for test
	}
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, postOpts, provingOpts)
	require.ErrorContains(t, err, "post service binary not found")
	require.Nil(t, ps)
}

func Test_PostSupervisor_StartsServiceCmd(t *testing.T) {
	log := zaptest.NewLogger(t)

	path, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(t, err)

	cmdCfg := PostSupervisorConfig{
		PostServiceCmd: filepath.Join(filepath.Dir(string(path)), "build", "service"),
		NodeAddress:    "http://127.0.0.1:12345", // node isn't listening and not relevant for test
	}
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, postOpts, provingOpts)
	require.NoError(t, err)
	require.NotNil(t, ps)
	t.Cleanup(func() { assert.NoError(t, ps.Close()) })

	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	pid := int(ps.pid.Load())
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

func Test_PostSupervisor_RestartsOnCrash(t *testing.T) {
	log := zaptest.NewLogger(t)

	path, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(t, err)

	cmdCfg := PostSupervisorConfig{
		PostServiceCmd: filepath.Join(filepath.Dir(string(path)), "build", "service"),
		NodeAddress:    "http://127.0.0.1:12345", // node isn't listening and not relevant for test
	}
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, postOpts, provingOpts)
	require.NoError(t, err)
	require.NotNil(t, ps)
	t.Cleanup(func() { assert.NoError(t, ps.Close()) })

	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	oldPid := int(ps.pid.Load())
	process, err := os.FindProcess(oldPid)
	require.NoError(t, err)
	require.NotNil(t, process)
	require.NoError(t, process.Kill())

	require.Eventually(t, func() bool { return (ps.pid.Load() != int64(oldPid)) }, 5*time.Second, 100*time.Millisecond)

	pid := int(ps.pid.Load())
	process, err = os.FindProcess(pid)
	require.NoError(t, err)
	require.NotNil(t, process)

	require.NotEqual(t, oldPid, pid)
	require.NoError(t, ps.Close())
}
