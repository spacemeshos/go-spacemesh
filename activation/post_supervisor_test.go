package activation

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func Test_PostSupervisor_ErrorOnMissingBinary(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	cmdCfg.PostServiceCmd = "missing"
	postCfg := DefaultPostConfig()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, nil, nil)
	require.ErrorContains(t, err, "post service binary not found")
	require.Nil(t, ps)
}

func Test_PostSupervisor_StopWithoutStart(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	provingOpts := DefaultPostProvingOpts()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Stop(false))
}

func Test_PostSupervisor_Start_FailPrepare(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	mgr := NewMockpostSetupProvider(gomock.NewController(t))
	testErr := errors.New("test error")
	mgr.EXPECT().PrepareInitializer(postOpts).Return(testErr)

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, mgr, nil)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.ErrorIs(t, ps.Start(postOpts), testErr)
}

type fatalHook struct {
	called bool
}

func (f *fatalHook) OnWrite(*zapcore.CheckedEntry, []zapcore.Field) {
	f.called = true
}

func calledFatal(tb testing.TB) zapcore.CheckWriteHook {
	hook := &fatalHook{}
	tb.Cleanup(func() { assert.True(tb, hook.called, "zap.Fatal not called") })
	return hook
}

func Test_PostSupervisor_Start_FailStartSession(t *testing.T) {
	log := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WithFatalHook(calledFatal(t))))

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	mgr.EXPECT().PrepareInitializer(postOpts).Return(nil)
	mgr.EXPECT().StartSession(gomock.Any()).Return(errors.New("failed start session"))

	sync := NewMocksyncer(ctrl)
	sync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan)

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, mgr, sync)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Start(postOpts))
	require.EqualError(t, ps.eg.Wait(), "failed start session")
}

func Test_PostSupervisor_StartsServiceCmd(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	mgr.EXPECT().PrepareInitializer(postOpts).Return(nil)
	mgr.EXPECT().StartSession(gomock.Any()).Return(nil)

	sync := NewMocksyncer(ctrl)
	sync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan)

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, mgr, sync)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Start(postOpts))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })

	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	pid := int(ps.pid.Load())
	process, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.NotNil(t, process)

	if runtime.GOOS != "windows" {
		require.NoError(t, process.Signal(syscall.Signal(0))) // check if process is running
	}

	require.NoError(t, ps.Stop(false))

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

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	mgr.EXPECT().PrepareInitializer(postOpts).Return(nil)
	mgr.EXPECT().StartSession(gomock.Any()).Return(nil)

	sync := NewMocksyncer(ctrl)
	sync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan).AnyTimes()

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, mgr, sync)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Start(postOpts))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })
	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	require.NoError(t, ps.Stop(false))
	require.Eventually(t, func() bool { return (ps.pid.Load() == 0) }, 5*time.Second, 100*time.Millisecond)

	mgr.EXPECT().PrepareInitializer(postOpts).Return(nil)
	mgr.EXPECT().StartSession(gomock.Any()).Return(nil)
	require.NoError(t, ps.Start(postOpts))
	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	require.NoError(t, ps.Stop(false))
	require.Eventually(t, func() bool { return (ps.pid.Load() == 0) }, 5*time.Second, 100*time.Millisecond)
}

func Test_PostSupervisor_RestartsOnCrash(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	mgr.EXPECT().PrepareInitializer(postOpts).Return(nil)
	mgr.EXPECT().StartSession(gomock.Any()).Return(nil)

	sync := NewMocksyncer(ctrl)
	sync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan)

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, mgr, sync)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Start(postOpts))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })

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
	require.NoError(t, ps.Stop(false))
}

func Test_PostSupervisor_StopOnError(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	mgr.EXPECT().PrepareInitializer(postOpts).Return(nil)
	mgr.EXPECT().StartSession(gomock.Any()).Return(nil)

	sync := NewMocksyncer(ctrl)
	sync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan)

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, mgr, sync)
	require.NoError(t, err)
	require.NotNil(t, ps)

	require.NoError(t, ps.Start(postOpts))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })
	require.Eventually(t, func() bool { return (ps.pid.Load() != 0) }, 5*time.Second, 100*time.Millisecond)

	testErr := errors.New("couldn't delete files")
	mgr.EXPECT().Reset().Return(testErr)
	require.ErrorIs(t, ps.Stop(true), testErr)
}

func Test_PostSupervisor_Providers_includesCPU(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	provingOpts := DefaultPostProvingOpts()

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	sync := NewMocksyncer(ctrl)

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, mgr, sync)
	require.NoError(t, err)

	providers, err := ps.Providers()
	require.NoError(t, err)

	for _, p := range providers {
		if p.ID == initialization.CPUProviderID() {
			return
		}
	}
	require.Fail(t, "no CPU provider found")
}

func Test_PostSupervisor_Benchmark(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	provingOpts := DefaultPostProvingOpts()

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	sync := NewMocksyncer(ctrl)

	ps, err := NewPostSupervisor(log.Named("supervisor"), cmdCfg, postCfg, provingOpts, mgr, sync)
	require.NoError(t, err)

	providers, err := ps.Providers()
	require.NoError(t, err)

	for _, p := range providers {
		score, err := ps.Benchmark(p)
		require.NoError(t, err)
		require.NotZero(t, score)
	}
}
