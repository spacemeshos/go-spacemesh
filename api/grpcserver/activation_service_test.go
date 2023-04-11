package grpcserver_test

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	"github.com/golang/mock/gomock"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/api/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestGet_RejectInvalidAtxID(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := mocks.NewMockAtxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider)

	_, err := activationService.Get(context.Background(), &pb.GetRequest{})
	require.Error(t, err)
	require.Equal(t, status.Code(err), codes.InvalidArgument)
}

func TestGet_AtxNotPresent(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := mocks.NewMockAtxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider)

	id := types.RandomATXID()
	atxProvider.EXPECT().GetFullAtx(id).Return(nil, nil)

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.Error(t, err)
	require.Equal(t, status.Code(err), codes.NotFound)
}

func TestGet_AtxProviderReturnsFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := mocks.NewMockAtxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider)

	id := types.RandomATXID()
	atxProvider.EXPECT().GetFullAtx(id).Return(&types.VerifiedActivationTx{}, errors.New(""))

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.Error(t, err)
	require.Equal(t, status.Code(err), codes.NotFound)
}

func TestGet_HappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := mocks.NewMockAtxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider)

	id := types.RandomATXID()
	atx := types.VerifiedActivationTx{
		ActivationTx: &types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					Sequence:           rand.Uint64(),
					PrevATXID:          types.RandomATXID(),
					PublishEpoch:       0,
					PositioningATX:     types.RandomATXID(),
					InitialPostIndices: types.RandomBytes(7),
				},
				Coinbase: types.GenerateAddress(types.RandomBytes(32)),
				NumUnits: rand.Uint32(),
			},
		},
	}
	atx.SetID(id)
	atxProvider.EXPECT().GetFullAtx(id).Return(&atx, nil)

	response, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.NoError(t, err)

	require.Equal(t, atx.ID().Bytes(), response.Atx.Id.Id)
	require.Equal(t, atx.PublishEpoch.Uint32(), response.Atx.Layer.Number)
	require.Equal(t, atx.SmesherID.Bytes(), response.Atx.SmesherId.Id)
	require.Equal(t, atx.Coinbase.String(), response.Atx.Coinbase.Address)
	require.Equal(t, atx.PrevATXID.Bytes(), response.Atx.PrevAtx.Id)
	require.Equal(t, atx.NumUnits, response.Atx.NumUnits)
	require.Equal(t, atx.Sequence, response.Atx.Sequence)
}
