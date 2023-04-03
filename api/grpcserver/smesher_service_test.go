package grpcserver_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	v1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/post/config"
)

func TestPostConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	postSetupProvider := activation.NewMockpostSetupProvider(ctrl)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, time.Second)

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
	svc := grpcserver.NewSmesherService(postSetupProvider, smeshingProvider, time.Second)

	addr, err := types.StringToAddress("stest1qqqqqqrs60l66w5uksxzmaznwq6xnhqfv56c28qlkm4a5")
	require.NoError(t, err)
	smeshingProvider.EXPECT().StartSmeshing(addr, activation.PostSetupOpts{
		DataDir:           "data-dir",
		NumUnits:          1,
		MaxFileSize:       1024,
		ComputeProviderID: 7,
		Throttle:          true,
		Scrypt:            config.DefaultLabelParams(),
	}).Return(nil)

	_, err = svc.StartSmeshing(context.Background(), &v1.StartSmeshingRequest{
		Coinbase: &v1.AccountId{Address: "stest1qqqqqqrs60l66w5uksxzmaznwq6xnhqfv56c28qlkm4a5"},
		Opts: &v1.PostSetupOpts{
			DataDir:           "data-dir",
			NumUnits:          1,
			MaxFileSize:       1024,
			ComputeProviderId: 7,
			Throttle:          true,
		},
	})
	require.NoError(t, err)
}
