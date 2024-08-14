package grpcserver

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func launchPostSupervisor(
	tb testing.TB,
	log *zap.Logger,
	serviceCfg activation.PostSupervisorConfig,
	postOpts activation.PostSetupOpts,
) (types.NodeID, func()) {
	postCfg := activation.DefaultPostConfig()
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = tb.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	goldenATXID := types.RandomATXID()

	ctrl := gomock.NewController(tb)
	validator := activation.NewMocknipostValidator(ctrl)
	validator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes()

	syncer := activation.NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	})
	db := sql.InMemory()
	logger := log.Named("post manager")
	mgr, err := activation.NewPostSetupManager(postCfg, logger, db, atxsdata.New(), goldenATXID, syncer, validator)
	require.NoError(tb, err)

	// start post supervisor
	builder := activation.NewMockAtxBuilder(ctrl)
	builder.EXPECT().Register(sig)
	ps := activation.NewPostSupervisor(log, postCfg, provingOpts, mgr, builder)
	require.NoError(tb, ps.Start(serviceCfg, postOpts, sig))
	return sig.NodeID(), func() { assert.NoError(tb, ps.Stop(false)) }
}

func launchPostSupervisorTLS(
	tb testing.TB,
	log *zap.Logger,
	serviceCfg activation.PostSupervisorConfig,
	postOpts activation.PostSetupOpts,
) (types.NodeID, func()) {
	postCfg := activation.DefaultPostConfig()
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = tb.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	goldenATXID := types.RandomATXID()

	ctrl := gomock.NewController(tb)
	validator := activation.NewMocknipostValidator(ctrl)
	validator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes()
	syncer := activation.NewMocksyncer(ctrl)
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	})
	db := sql.InMemoryTest(tb)
	logger := log.Named("post supervisor")
	mgr, err := activation.NewPostSetupManager(postCfg, logger, db, atxsdata.New(), goldenATXID, syncer, validator)
	require.NoError(tb, err)

	// start post supervisor
	builder := activation.NewMockAtxBuilder(ctrl)
	builder.EXPECT().Register(sig)
	ps := activation.NewPostSupervisor(log, postCfg, provingOpts, mgr, builder)
	require.NoError(tb, ps.Start(serviceCfg, postOpts, sig))
	return sig.NodeID(), func() { assert.NoError(tb, ps.Stop(false)) }
}

func Test_GenerateProof(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	svc.AllowConnections(true)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	serviceCfg := activation.DefaultTestPostServiceConfig()
	serviceCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)

	id, postCleanup := launchPostSupervisor(t, log.Named("supervisor"), serviceCfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	proof, meta, err := client.Proof(context.Background(), challenge)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.NotNil(t, meta)

	// drop connection
	postCleanup()
	require.Eventually(t, func() bool {
		proof, meta, err = client.Proof(context.Background(), challenge)
		return err != nil
	}, 5*time.Second, 100*time.Millisecond)

	require.ErrorContains(t, err, "post client closed")
	require.Nil(t, proof)
	require.Nil(t, meta)
}

func Test_GenerateProof_TLS(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	svc.AllowConnections(true)
	certDir := genKeys(t)
	cfg, cleanup := launchTLSServer(t, certDir, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	serviceCfg := activation.DefaultTestPostServiceConfig()
	serviceCfg.NodeAddress = fmt.Sprintf("https://%s", cfg.TLSListener)
	serviceCfg.CACert = filepath.Join(certDir, caCertName)
	serviceCfg.Cert = filepath.Join(certDir, clientCertName)
	serviceCfg.Key = filepath.Join(certDir, clientKeyName)

	id, postCleanup := launchPostSupervisorTLS(t, log.Named("supervisor"), serviceCfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	proof, meta, err := client.Proof(context.Background(), challenge)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.NotNil(t, meta)

	// drop connection
	postCleanup()
	require.Eventually(t, func() bool {
		proof, meta, err = client.Proof(context.Background(), challenge)
		return err != nil
	}, 5*time.Second, 100*time.Millisecond)

	require.ErrorContains(t, err, "post client closed")
	require.Nil(t, proof)
	require.Nil(t, meta)
}

func Test_GenerateProof_Cancel(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	svc.AllowConnections(true)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	serviceCfg := activation.DefaultTestPostServiceConfig()
	serviceCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)

	id, postCleanup := launchPostSupervisor(t, log.Named("supervisor"), serviceCfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	// cancel on sending
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	proof, meta, err := client.Proof(ctx, challenge)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, proof)
	require.Nil(t, meta)
}

func Test_Metadata(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	svc.AllowConnections(true)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	serviceCfg := activation.DefaultTestPostServiceConfig()
	serviceCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)

	id, postCleanup := launchPostSupervisor(t, log.Named("supervisor"), serviceCfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	meta, err := client.Info(context.Background())
	require.NoError(t, err)
	require.NotNil(t, meta)
	require.Equal(t, id, meta.NodeID)
	require.NotEmpty(t, meta.CommitmentATX)
	require.NotNil(t, meta.Nonce)
	require.Equal(t, opts.NumUnits, meta.NumUnits)

	// drop connection
	postCleanup()
	require.Eventually(t, func() bool {
		meta, err = client.Info(context.Background())
		return err != nil
	}, 5*time.Second, 100*time.Millisecond)

	require.ErrorContains(t, err, "post client closed")
	require.Nil(t, meta)
}

func Test_GenerateProof_MultipleServices(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	svc.AllowConnections(true)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	serviceCfg := activation.DefaultTestPostServiceConfig()
	serviceCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)

	// all but one should not be able to register to the node (i.e. open a stream to it).
	id, postCleanup := launchPostSupervisor(t, log.Named("supervisor1"), serviceCfg, opts)
	t.Cleanup(postCleanup)

	opts.DataDir = t.TempDir()
	_, postCleanup = launchPostSupervisor(t, log.Named("supervisor2"), serviceCfg, opts)
	t.Cleanup(postCleanup)

	opts.DataDir = t.TempDir()
	_, postCleanup = launchPostSupervisor(t, log.Named("supervisor3"), serviceCfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	proof, meta, err := client.Proof(context.Background(), challenge)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.NotNil(t, meta)
}

func Test_PostService_Connection_NotAllowed(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	svc.AllowConnections(false)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn, err := grpc.NewClient(
		cfg.PublicListener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := pb.NewPostServiceClient(conn)

	stream, err := client.Register(context.Background())
	require.NoError(t, err)

	_, err = stream.Recv()
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.ErrorContains(t, err, "connection not allowed")
}
