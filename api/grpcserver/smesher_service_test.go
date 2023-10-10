package grpcserver_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestPostConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	postSetupProvider := grpcserver.NewMockpostSetupProvider(ctrl)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	postSupervisor := grpcserver.NewMockpostSupervisor(ctrl)
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, postSupervisor, time.Second, activation.DefaultPostSetupOpts())

	postConfig := activation.PostConfig{
		MinNumUnits:   rand.Uint32(),
		MaxNumUnits:   rand.Uint32(),
		LabelsPerUnit: rand.Uint64(),
		K1:            rand.Uint32(),
		K2:            rand.Uint32(),
	}
	postSetupProvider.EXPECT().Config().Return(postConfig)

	response, err := svc.PostConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.EqualValues(t, config.BitsPerLabel, response.BitsPerLabel)
	require.Equal(t, postConfig.MinNumUnits, response.MinNumUnits)
	require.Equal(t, postConfig.MaxNumUnits, response.MaxNumUnits)
	require.Equal(t, postConfig.LabelsPerUnit, response.LabelsPerUnit)
	require.Equal(t, postConfig.K1, response.K1)
	require.EqualValues(t, postConfig.K2, response.K2)
}

func TestStartSmeshingPassesCorrectSmeshingOpts(t *testing.T) {
	ctrl := gomock.NewController(t)
	postSetupProvider := grpcserver.NewMockpostSetupProvider(ctrl)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	postSupervisor := grpcserver.NewMockpostSupervisor(ctrl)
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, postSupervisor, time.Second, activation.DefaultPostSetupOpts())

	types.SetNetworkHRP("stest")
	addr, err := types.StringToAddress("stest1qqqqqqrs60l66w5uksxzmaznwq6xnhqfv56c28qlkm4a5")
	require.NoError(t, err)
	providerID := uint32(7)
	opts := activation.PostSetupOpts{
		DataDir:          "data-dir",
		NumUnits:         1,
		MaxFileSize:      1024,
		Throttle:         true,
		Scrypt:           config.DefaultLabelParams(),
		ComputeBatchSize: config.DefaultComputeBatchSize,
	}
	opts.ProviderID.SetInt64(int64(providerID))
	postSupervisor.EXPECT().Start().Return(nil)
	smeshingProvider.EXPECT().StartSmeshing(addr, opts).Return(nil)

	_, err = svc.StartSmeshing(context.Background(), &pb.StartSmeshingRequest{
		Coinbase: &pb.AccountId{Address: "stest1qqqqqqrs60l66w5uksxzmaznwq6xnhqfv56c28qlkm4a5"},
		Opts: &pb.PostSetupOpts{
			DataDir:     "data-dir",
			NumUnits:    1,
			MaxFileSize: 1024,
			ProviderId:  &providerID,
			Throttle:    true,
		},
	})
	require.NoError(t, err)
}

func TestSmesherService_PostSetupProviders(t *testing.T) {
	ctrl := gomock.NewController(t)
	postSetupProvider := grpcserver.NewMockpostSetupProvider(ctrl)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	postSupervisor := grpcserver.NewMockpostSupervisor(ctrl)
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, postSupervisor, time.Second, activation.DefaultPostSetupOpts())

	providers := []activation.PostSetupProvider{
		{
			ID:    0,
			Model: "cpu",
		},
		{
			ID:    1,
			Model: "gpu",
		},
	}
	postSetupProvider.EXPECT().Providers().Return(providers, nil).AnyTimes()

	resp, err := svc.PostSetupProviders(context.Background(), &pb.PostSetupProvidersRequest{
		Benchmark: false,
	})
	require.NoError(t, err)
	require.Len(t, resp.Providers, 2)
	require.EqualValues(t, providers[0].ID, resp.Providers[0].Id)
	require.EqualValues(t, providers[1].ID, resp.Providers[1].Id)

	postSetupProvider.EXPECT().Benchmark(providers[0]).Return(1_000, nil)
	postSetupProvider.EXPECT().Benchmark(providers[1]).Return(100_000, nil)

	resp, err = svc.PostSetupProviders(context.Background(), &pb.PostSetupProvidersRequest{
		Benchmark: true,
	})
	require.NoError(t, err)
	require.Len(t, resp.Providers, 2)
	require.EqualValues(t, providers[0].ID, resp.Providers[0].Id)
	require.Equal(t, uint64(1_000), resp.Providers[0].Performance)
	require.EqualValues(t, providers[1].ID, resp.Providers[1].Id)
	require.Equal(t, uint64(100_000), resp.Providers[1].Performance)
}

func TestSmesherService_PostSetupStatus(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		postSetupProvider := grpcserver.NewMockpostSetupProvider(ctrl)
		smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
		postSupervisor := grpcserver.NewMockpostSupervisor(ctrl)
		svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, postSupervisor, time.Second, activation.DefaultPostSetupOpts())

		postSetupProvider.EXPECT().Status().Return(&activation.PostSetupStatus{
			State:            activation.PostSetupStateComplete,
			NumLabelsWritten: 1_000,
			LastOpts:         nil,
		})
		resp, err := svc.PostSetupStatus(context.Background(), &emptypb.Empty{})
		require.NoError(t, err)
		require.Equal(t, pb.PostSetupStatus_STATE_COMPLETE, resp.Status.State)
		require.EqualValues(t, 1_000, resp.Status.NumLabelsWritten)
		require.Nil(t, resp.Status.Opts)
	})

	t.Run("completed with last Opts", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		postSetupProvider := grpcserver.NewMockpostSetupProvider(ctrl)
		smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
		postSupervisor := grpcserver.NewMockpostSupervisor(ctrl)
		svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, postSupervisor, time.Second, activation.DefaultPostSetupOpts())

		id := activation.PostProviderID{}
		id.SetInt64(1)
		opts := activation.PostSetupOpts{
			DataDir:     "data-dir",
			NumUnits:    4,
			MaxFileSize: 1024,
			ProviderID:  id,
			Throttle:    true,
		}
		postSetupProvider.EXPECT().Status().Return(&activation.PostSetupStatus{
			State:            activation.PostSetupStateComplete,
			NumLabelsWritten: 1_000,
			LastOpts:         &opts,
		})
		resp, err := svc.PostSetupStatus(context.Background(), &emptypb.Empty{})
		require.NoError(t, err)
		require.Equal(t, pb.PostSetupStatus_STATE_COMPLETE, resp.Status.State)
		require.EqualValues(t, 1_000, resp.Status.NumLabelsWritten)
		require.Equal(t, "data-dir", resp.Status.Opts.DataDir)
		require.EqualValues(t, 4, resp.Status.Opts.NumUnits)
		require.EqualValues(t, 1024, resp.Status.Opts.MaxFileSize)
		require.EqualValues(t, 1, *resp.Status.Opts.ProviderId)
		require.True(t, resp.Status.Opts.Throttle)
	})

	t.Run("in progress", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		postSetupProvider := grpcserver.NewMockpostSetupProvider(ctrl)
		smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
		postSupervisor := grpcserver.NewMockpostSupervisor(ctrl)
		svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, postSupervisor, time.Second, activation.DefaultPostSetupOpts())

		id := activation.PostProviderID{}
		id.SetInt64(100)
		opts := activation.PostSetupOpts{
			DataDir:     "data-dir",
			NumUnits:    4,
			MaxFileSize: 1024,
			ProviderID:  id,
			Throttle:    false,
		}
		postSetupProvider.EXPECT().Status().Return(&activation.PostSetupStatus{
			State:            activation.PostSetupStateInProgress,
			NumLabelsWritten: 1_000,
			LastOpts:         &opts,
		})
		resp, err := svc.PostSetupStatus(context.Background(), &emptypb.Empty{})
		require.NoError(t, err)
		require.Equal(t, pb.PostSetupStatus_STATE_IN_PROGRESS, resp.Status.State)
		require.EqualValues(t, 1_000, resp.Status.NumLabelsWritten)
		require.Equal(t, "data-dir", resp.Status.Opts.DataDir)
		require.EqualValues(t, 4, resp.Status.Opts.NumUnits)
		require.EqualValues(t, 1024, resp.Status.Opts.MaxFileSize)
		require.EqualValues(t, 100, *resp.Status.Opts.ProviderId)
		require.False(t, resp.Status.Opts.Throttle)
	})
}
