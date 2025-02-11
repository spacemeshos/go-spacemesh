package node

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
)

func getSmeshingServiceTestConfig(tb testing.TB) *config.Config {
	cfg := config.MainnetSmeshingServiceConfig()

	tmp := tb.TempDir()
	cfg.DataDirParent = tmp
	cfg.FileLock = filepath.Join(tmp, "LOCK")
	cfg.LayerDuration = 20 * time.Second

	cfg.API.PublicListener = "127.0.0.1:0"
	cfg.API.PrivateListener = "127.0.0.1:0"
	cfg.API.JSONListener = "127.0.0.1:0"

	cfg.POST = activation.DefaultPostConfig()
	cfg.POST.MinNumUnits = 2
	cfg.POST.MaxNumUnits = 4
	cfg.POST.LabelsPerUnit = 32
	cfg.POST.K2 = 4

	cfg.BaseConfig.PoetServers = nil

	cfg.SMESHING = config.DefaultSmeshingConfig()
	cfg.SMESHING.Start = false
	cfg.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte{1}).StringWithHRP(cfg.NetworkHRP)
	cfg.SMESHING.Opts.DataDir = filepath.Join(tmp, "post")
	cfg.SMESHING.Opts.NumUnits = cfg.POST.MinNumUnits + 1
	cfg.SMESHING.Opts.Scrypt.N = 2
	cfg.SMESHING.Opts.ProviderID.SetUint32(initialization.CPUProviderID())

	cfg.HARE3.RoundDuration = 2
	cfg.HARE3.PreroundDelay = 1

	cfg.HARE4.RoundDuration = 2
	cfg.HARE4.PreroundDelay = 1

	cfg.LayerAvgSize = 5
	cfg.LayersPerEpoch = 3
	cfg.TxsPerProposal = 100
	cfg.Tortoise.Zdist = 5

	cfg.HareEligibility.ConfidenceParam = 1

	cfg.Genesis = config.DefaultTestGenesisConfig()
	cfg.POSTService = activation.DefaultTestPostServiceConfig()

	return &cfg
}

func TestNewSmeshingService(t *testing.T) {
	t.Run("creates new service with valid config", func(t *testing.T) {
		cfg := getSmeshingServiceTestConfig(t)
		service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
		require.NoError(t, err)
		require.NotNil(t, service)

		// Verify DataDir was created
		dataDir := cfg.DataDir()
		_, err = os.Stat(dataDir)
		require.NoError(t, err, "DataDir should exist")
	})

	t.Run("creates identity if not exists", func(t *testing.T) {
		cfg := getSmeshingServiceTestConfig(t)
		service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
		require.NoError(t, err)
		require.NotNil(t, service)
		require.Len(t, service.signers, 1)

		// Verify identity file was created
		_, err = os.Stat(filepath.Join(cfg.DataDir(), "identities", "local.key"))
		require.NoError(t, err)
	})
}

func TestSmeshingService_Start(t *testing.T) {
	t.Run("starts API services", func(t *testing.T) {
		cfg := getSmeshingServiceTestConfig(t)
		cfg.API.PublicServices = []grpcserver.Service{grpcserver.Debug}

		service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		errCh := make(chan error)
		go func() {
			errCh <- service.Start(ctx)
		}()
		select {
		case <-service.started:
		case <-ctx.Done():
			t.Fatal("not started on time")
		}

		conn, err := grpc.NewClient(
			service.grpcPublicServer.BoundAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, conn.Close()) })

		client := pb.NewDebugServiceClient(conn)
		resp, err := client.ChangeLogLevel(ctx, &pb.ChangeLogLevelRequest{
			Module: "poet",
			Level:  "DEBUG",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

func TestSmeshingService_StartSmeshing(t *testing.T) {
	t.Run("starts smeshing with valid coinbase", func(t *testing.T) {
		cfg := getSmeshingServiceTestConfig(t)
		service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err = service.startServices(ctx)
		require.NoError(t, err)
	})

	t.Run("fails with invalid coinbase", func(t *testing.T) {
		cfg := getSmeshingServiceTestConfig(t)
		cfg.SMESHING.CoinbaseAccount = "invalid-address"

		service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err = service.startServices(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse CoinbaseAccount address")
	})
}

func TestSmeshingService_GRPCServices(t *testing.T) {
	t.Run("initializes all grpc services", func(t *testing.T) {
		cfg := getSmeshingServiceTestConfig(t)
		service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
		require.NoError(t, err)

		services := []grpcserver.Service{
			grpcserver.Debug,
			grpcserver.Smesher,
			grpcserver.Post,
			grpcserver.PostInfo,
		}

		for _, svc := range services {
			api, err := service.grpcService(svc, zaptest.NewLogger(t))
			require.NoError(t, err)
			require.NotNil(t, api)
		}
	})

	t.Run("fails with unknown service", func(t *testing.T) {
		cfg := getSmeshingServiceTestConfig(t)
		service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
		require.NoError(t, err)

		api, err := service.grpcService("unknown", zaptest.NewLogger(t))
		require.Error(t, err)
		require.Nil(t, api)
	})
}

func TestSmeshingService_StopServices(t *testing.T) {
	t.Run("stops all services", func(t *testing.T) {
		cfg := getSmeshingServiceTestConfig(t)
		service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err = service.start(ctx)
		require.NoError(t, err)

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()

		service.stopServices(stopCtx)
	})
}

func TestSmeshingService_PprofServer(t *testing.T) {
	cfg := getSmeshingServiceTestConfig(t)
	cfg.PprofHTTPServer = true
	cfg.PprofHTTPServerListener = ":0"
	cfg.PprofBlockProfile = true
	cfg.PprofMutexProfile = true
	service, err := NewSmeshingService(cfg, zaptest.NewLogger(t))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	require.NoError(t, service.start(ctx))
	defer func() {
		cancel()
		service.stopServices(context.Background())
	}()

	url := fmt.Sprintf("http://%s/debug/pprof/", service.pprofService.Addr)
	require.Eventually(t, func() bool {
		resp, err := http.Get(url)
		require.NoError(t, err, url)
		resp.Body.Close()
		return http.StatusOK == resp.StatusCode
	}, time.Second*10, time.Millisecond*100)
}
