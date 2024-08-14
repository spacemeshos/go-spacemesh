package activation

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func testSetupOpts(t *testing.T) PostSetupOpts {
	t.Helper()
	opts := DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2
	return opts
}

func newPostManager(t *testing.T, cfg PostConfig) *PostSetupManager {
	t.Helper()
	ctrl := gomock.NewController(t)
	validator := NewMocknipostValidator(ctrl)
	validator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes()

	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	})
	db := sql.InMemory()
	atxsdata := atxsdata.New()
	mgr, err := NewPostSetupManager(cfg, zaptest.NewLogger(t), db, atxsdata, types.RandomATXID(), syncer, validator)
	require.NoError(t, err)
	return mgr
}

func Test_PostSupervisor_ErrorOnMissingBinary(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	cmdCfg.PostServiceCmd = "missing"
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	provingOpts := DefaultPostProvingOpts()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, nil, nil)
	err = ps.Start(cmdCfg, postOpts, sig)
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.ErrorContains(t, err, "stat post service binary missing")
}

func Test_PostSupervisor_StopWithoutStart(t *testing.T) {
	log := zaptest.NewLogger(t)

	postCfg := DefaultPostConfig()
	provingOpts := DefaultPostProvingOpts()

	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, nil, nil)
	require.NoError(t, ps.Stop(false))
}

func Test_PostSupervisor_Start_FailPrepare(t *testing.T) {
	log := zaptest.NewLogger(t).WithOptions(zap.WithFatalHook(calledFatal(t)))

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := DefaultPostSetupOpts()
	postOpts.DataDir = t.TempDir()
	provingOpts := DefaultPostProvingOpts()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	testErr := errors.New("test error")
	mgr.EXPECT().PrepareInitializer(gomock.Any(), postOpts, sig.NodeID()).Return(testErr)
	builder := NewMockAtxBuilder(ctrl)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

	require.NoError(t, ps.Start(cmdCfg, postOpts, sig))
	require.ErrorIs(t, ps.Stop(false), testErr)
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
	postOpts.DataDir = t.TempDir()
	provingOpts := DefaultPostProvingOpts()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	mgr.EXPECT().PrepareInitializer(gomock.Any(), postOpts, sig.NodeID()).Return(nil)
	mgr.EXPECT().StartSession(gomock.Any(), sig.NodeID()).Return(errors.New("failed start session"))
	builder := NewMockAtxBuilder(ctrl)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

	require.NoError(t, ps.Start(cmdCfg, postOpts, sig))
	require.EqualError(t, ps.eg.Wait(), "failed start session")
}

func Test_PostSupervisor_StartsServiceCmd(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := testSetupOpts(t)
	provingOpts := DefaultPostProvingOpts()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mgr := newPostManager(t, postCfg)
	builder := NewMockAtxBuilder(ctrl)
	builder.EXPECT().Register(sig)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

	require.NoError(t, ps.Start(cmdCfg, postOpts, sig))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })

	require.Eventually(t, func() bool { return ps.pid.Load() != 0 }, 5*time.Second, 100*time.Millisecond)

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
	postOpts := testSetupOpts(t)
	provingOpts := DefaultPostProvingOpts()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mgr := newPostManager(t, postCfg)
	builder := NewMockAtxBuilder(ctrl)
	builder.EXPECT().Register(sig)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

	require.NoError(t, ps.Start(cmdCfg, postOpts, sig))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })
	require.Eventually(t, func() bool { return ps.pid.Load() != 0 }, 5*time.Second, 100*time.Millisecond)

	require.NoError(t, ps.Stop(false))
	require.Eventually(t, func() bool { return ps.pid.Load() == 0 }, 5*time.Second, 100*time.Millisecond)

	builder.EXPECT().Register(sig)
	require.NoError(t, ps.Start(cmdCfg, postOpts, sig))
	require.Eventually(t, func() bool { return ps.pid.Load() != 0 }, 5*time.Second, 100*time.Millisecond)

	require.NoError(t, ps.Stop(false))
	require.Eventually(t, func() bool { return ps.pid.Load() == 0 }, 5*time.Second, 100*time.Millisecond)
}

func Test_PostSupervisor_LogFatalOnCrash(t *testing.T) {
	log := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WithFatalHook(calledFatal(t))))

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := testSetupOpts(t)
	provingOpts := DefaultPostProvingOpts()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mgr := newPostManager(t, postCfg)
	builder := NewMockAtxBuilder(ctrl)
	builder.EXPECT().Register(sig)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

	require.NoError(t, ps.Start(cmdCfg, postOpts, sig))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })

	require.Eventually(t, func() bool { return ps.pid.Load() != 0 }, 5*time.Second, 100*time.Millisecond)

	pid := int(ps.pid.Load())
	process, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.NotNil(t, process)
	require.NoError(t, process.Kill())

	// log asserts that zap.Fatal was called
	require.NoError(t, ps.eg.Wait())
}

func Test_PostSupervisor_LogFatalOnInvalidConfig(t *testing.T) {
	log := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WithFatalHook(calledFatal(t))))

	cmdCfg := DefaultTestPostServiceConfig()
	cmdCfg.NodeAddress = "http://127.0.0.1:9099" // wrong port
	cmdCfg.MaxRetries = 1                        // speedup test, will fail on 2nd retry (~ 5s)
	postCfg := DefaultPostConfig()
	postOpts := testSetupOpts(t)
	provingOpts := DefaultPostProvingOpts()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mgr := newPostManager(t, postCfg)
	builder := NewMockAtxBuilder(ctrl)
	builder.EXPECT().Register(sig)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

	require.NoError(t, ps.Start(cmdCfg, postOpts, sig))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })

	require.Eventually(t, func() bool { return ps.pid.Load() != 0 }, 5*time.Second, 100*time.Millisecond)

	pid := int(ps.pid.Load())
	process, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.NotNil(t, process)

	// log asserts that zap.Fatal was called
	require.NoError(t, ps.eg.Wait())
}

func Test_PostSupervisor_StopOnError(t *testing.T) {
	log := zaptest.NewLogger(t)

	cmdCfg := DefaultTestPostServiceConfig()
	postCfg := DefaultPostConfig()
	postOpts := testSetupOpts(t)
	provingOpts := DefaultPostProvingOpts()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	mgr.EXPECT().PrepareInitializer(gomock.Any(), postOpts, sig.NodeID()).Return(nil)
	mgr.EXPECT().StartSession(gomock.Any(), sig.NodeID()).DoAndReturn(func(_ context.Context, id types.NodeID) error {
		// The service requires metadata JSON file to exist or it will fail to boot.
		meta := shared.PostMetadata{NodeId: id.Bytes(), CommitmentAtxId: types.RandomATXID().Bytes(), NumUnits: 1}
		metaBytes, err := json.Marshal(meta)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(postOpts.DataDir, "postdata_metadata.json"), metaBytes, 0o644)
		require.NoError(t, err)
		return nil
	})
	builder := NewMockAtxBuilder(ctrl)
	builder.EXPECT().Register(sig)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

	require.NoError(t, ps.Start(cmdCfg, postOpts, sig))
	t.Cleanup(func() { assert.NoError(t, ps.Stop(false)) })
	require.Eventually(t, func() bool { return ps.pid.Load() != 0 }, 5*time.Second, 100*time.Millisecond)

	testErr := errors.New("couldn't delete files")
	mgr.EXPECT().Reset().Return(testErr)
	require.ErrorIs(t, ps.Stop(true), testErr)
}

func Test_PostSupervisor_Providers_includesCPU(t *testing.T) {
	log := zaptest.NewLogger(t)

	postCfg := DefaultPostConfig()
	provingOpts := DefaultPostProvingOpts()

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	builder := NewMockAtxBuilder(ctrl)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

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

	postCfg := DefaultPostConfig()
	provingOpts := DefaultPostProvingOpts()

	ctrl := gomock.NewController(t)
	mgr := NewMockpostSetupProvider(ctrl)
	builder := NewMockAtxBuilder(ctrl)
	ps := NewPostSupervisor(log.Named("supervisor"), postCfg, provingOpts, mgr, builder)

	providers, err := ps.Providers()
	require.NoError(t, err)

	for _, p := range providers {
		score, err := ps.Benchmark(p)
		require.NoError(t, err)
		require.NotZero(t, score)
	}
}
