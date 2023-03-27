package grpcserver_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
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
	require.Equal(t, postConfig.MinNumUnits, response.MinNumUnits)
	require.Equal(t, postConfig.MaxNumUnits, response.MaxNumUnits)
	require.Equal(t, postConfig.LabelsPerUnit, response.LabelsPerUnit)
	require.Equal(t, postConfig.K1, response.K1)
	require.EqualValues(t, postConfig.K2, response.K2)
}
