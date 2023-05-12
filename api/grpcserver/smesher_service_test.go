package grpcserver_test

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestPostConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	postSetupProvider := activation.NewMockpostSetupProvider(ctrl)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	mockFunc := func() grpcserver.CheckpointRunner {
		return grpcserver.NewMockCheckpointRunner(ctrl)
	}
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, mockFunc, time.Second, activation.DefaultPostSetupOpts())

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
	postSetupProvider := activation.NewMockpostSetupProvider(ctrl)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	mockFunc := func() grpcserver.CheckpointRunner {
		return grpcserver.NewMockCheckpointRunner(ctrl)
	}
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, mockFunc, time.Second, activation.DefaultPostSetupOpts())

	types.DefaultTestAddressConfig()
	addr, err := types.StringToAddress("stest1qqqqqqrs60l66w5uksxzmaznwq6xnhqfv56c28qlkm4a5")
	require.NoError(t, err)
	smeshingProvider.EXPECT().StartSmeshing(addr, activation.PostSetupOpts{
		DataDir:          "data-dir",
		NumUnits:         1,
		MaxFileSize:      1024,
		ProviderID:       7,
		Throttle:         true,
		Scrypt:           config.DefaultLabelParams(),
		ComputeBatchSize: config.DefaultComputeBatchSize,
	}).Return(nil)

	_, err = svc.StartSmeshing(context.Background(), &pb.StartSmeshingRequest{
		Coinbase: &pb.AccountId{Address: "stest1qqqqqqrs60l66w5uksxzmaznwq6xnhqfv56c28qlkm4a5"},
		Opts: &pb.PostSetupOpts{
			DataDir:     "data-dir",
			NumUnits:    1,
			MaxFileSize: 1024,
			ProviderId:  7,
			Throttle:    true,
		},
	})
	require.NoError(t, err)
}

func TestSmesherService_PostSetupProviders(t *testing.T) {
	ctrl := gomock.NewController(t)
	postSetupProvider := activation.NewMockpostSetupProvider(ctrl)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, time.Second, activation.DefaultPostSetupOpts())

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

func TestSmesherService_Checkpoint(t *testing.T) {
	ctrl := gomock.NewController(t)
	postSetupProvider := activation.NewMockpostSetupProvider(ctrl)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	runner := grpcserver.NewMockCheckpointRunner(ctrl)
	mockFunc := func() grpcserver.CheckpointRunner {
		return runner
	}
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, mockFunc, time.Second, activation.DefaultPostSetupOpts())

	const (
		snapshot uint32 = 11
		restore  uint32 = 13
		data            = "checkpoint data"
	)
	runner.EXPECT().Generate(gomock.Any(), types.LayerID(snapshot), types.LayerID(restore)).Return([]byte(data), nil)
	resp, err := svc.Checkpoint(context.Background(), &pb.CheckpointRequest{
		SnapshotLayer: snapshot,
		RestoreLayer:  restore,
	})
	require.NoError(t, err)
	require.Equal(t, data, string(resp.Data))

	errStr := "whatever"
	runner.EXPECT().Generate(gomock.Any(), types.LayerID(snapshot), types.LayerID(restore)).Return(nil, errors.New(errStr))
	_, err = svc.Checkpoint(context.Background(), &pb.CheckpointRequest{
		SnapshotLayer: snapshot,
		RestoreLayer:  restore,
	})
	require.ErrorContains(t, err, errStr)
}
