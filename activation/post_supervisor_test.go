package activation

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func Test_PostSupervisor_ErrorOnMissingBinary(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	cmdCfg.PostServiceCmd = "missing"
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, postOpts, provingOpts)
	require.ErrorContains(t, err, "post service binary not found")
	require.Nil(t, ps)
}

func Test_PostSupervisor_StopWithoutStart(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, postOpts, provingOpts)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Stop())
}

func Test_PostSupervisor_StartsServiceCmd(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, postOpts, provingOpts)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Start())
	t.Cleanup(func() { assert.NoError(t, ps.Stop()) })

	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	pid := int(ps.pid.Load())
	process, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.NotNil(t, process)

	if runtime.GOOS != "windows" {
		require.NoError(t, process.Signal(syscall.Signal(0))) // check if process is running
	}

	require.NoError(t, ps.Stop())

	if runtime.GOOS != "windows" {
		require.Error(t, process.Signal(syscall.Signal(0))) // check if process is closed
	}
}

func Test_PostSupervisor_Restart_Possible(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, postOpts, provingOpts)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Start())
	t.Cleanup(func() { assert.NoError(t, ps.Stop()) })
	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	require.NoError(t, ps.Stop())
	require.Eventually(t, func() bool { return (ps.pid.Load() == 0) }, 5*time.Second, 100*time.Millisecond)

	require.NoError(t, ps.Start())
	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	require.NoError(t, ps.Stop())
	require.Eventually(t, func() bool { return (ps.pid.Load() == 0) }, 5*time.Second, 100*time.Millisecond)
}

func Test_PostSupervisor_RestartsOnCrash(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, postOpts, provingOpts)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Start())
	t.Cleanup(func() { assert.NoError(t, ps.Stop()) })

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
	require.NoError(t, ps.Stop())
}
