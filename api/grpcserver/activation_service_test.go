package grpcserver_test

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Test_Highest_ReturnsGoldenAtxOnError(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	goldenAtx := types.ATXID{2, 3, 4}
	activationService := grpcserver.NewActivationService(atxProvider, goldenAtx)

	atxProvider.EXPECT().MaxHeightAtx().Return(types.EmptyATXID, errors.New("blah"))
	response, err := activationService.Highest(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Equal(t, goldenAtx.Bytes(), response.Atx.Id.Id)
	require.Nil(t, response.Atx.Layer)
	require.Nil(t, response.Atx.SmesherId)
	require.Nil(t, response.Atx.Coinbase)
	require.Nil(t, response.Atx.PrevAtx)
	require.EqualValues(t, 0, response.Atx.NumUnits)
	require.EqualValues(t, 0, response.Atx.Sequence)
}

func Test_Highest_ReturnsMaxTickHeight(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	goldenAtx := types.ATXID{2, 3, 4}
	activationService := grpcserver.NewActivationService(atxProvider, goldenAtx)

	atx := types.VerifiedActivationTx{
		ActivationTx: &types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					Sequence:       rand.Uint64(),
					PrevATXID:      types.RandomATXID(),
					PublishEpoch:   0,
					PositioningATX: types.RandomATXID(),
				},
				Coinbase: types.GenerateAddress(types.RandomBytes(32)),
				NumUnits: rand.Uint32(),
			},
		},
	}
	id := types.RandomATXID()
	atx.SetID(id)
	atxProvider.EXPECT().MaxHeightAtx().Return(id, nil)
	atxProvider.EXPECT().GetFullAtx(id).Return(&atx, nil)

	response, err := activationService.Highest(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Equal(t, atx.ID().Bytes(), response.Atx.Id.Id)
	require.Equal(t, atx.PublishEpoch.Uint32(), response.Atx.Layer.Number)
	require.Equal(t, atx.SmesherID.Bytes(), response.Atx.SmesherId.Id)
	require.Equal(t, atx.Coinbase.String(), response.Atx.Coinbase.Address)
	require.Equal(t, atx.PrevATXID.Bytes(), response.Atx.PrevAtx.Id)
	require.Equal(t, atx.NumUnits, response.Atx.NumUnits)
	require.Equal(t, atx.Sequence, response.Atx.Sequence)
}

func TestGet_RejectInvalidAtxID(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: []byte{1, 2, 3}})
	require.Error(t, err)
	require.Equal(t, status.Code(err), codes.InvalidArgument)
}

func TestGet_AtxNotPresent(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	id := types.RandomATXID()
	atxProvider.EXPECT().GetFullAtx(id).Return(nil, nil)

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.Error(t, err)
	require.Equal(t, status.Code(err), codes.NotFound)
}

func TestGet_AtxProviderReturnsFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	id := types.RandomATXID()
	atxProvider.EXPECT().GetFullAtx(id).Return(&types.VerifiedActivationTx{}, errors.New(""))

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.Error(t, err)
	require.Equal(t, status.Code(err), codes.NotFound)
}

func TestGet_HappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	id := types.RandomATXID()
	atx := types.VerifiedActivationTx{
		ActivationTx: &types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					Sequence:       rand.Uint64(),
					PrevATXID:      types.RandomATXID(),
					PublishEpoch:   0,
					PositioningATX: types.RandomATXID(),
				},
				Coinbase: types.GenerateAddress(types.RandomBytes(32)),
				NumUnits: rand.Uint32(),
			},
		},
	}
	atx.SetID(id)
	atxProvider.EXPECT().GetFullAtx(id).Return(&atx, nil)
	atxProvider.EXPECT().GetMalfeasanceProof(gomock.Any()).Return(nil, sql.ErrNotFound)

	response, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.NoError(t, err)

	require.Equal(t, atx.ID().Bytes(), response.Atx.Id.Id)
	require.Equal(t, atx.PublishEpoch.Uint32(), response.Atx.Layer.Number)
	require.Equal(t, atx.SmesherID.Bytes(), response.Atx.SmesherId.Id)
	require.Equal(t, atx.Coinbase.String(), response.Atx.Coinbase.Address)
	require.Equal(t, atx.PrevATXID.Bytes(), response.Atx.PrevAtx.Id)
	require.Equal(t, atx.NumUnits, response.Atx.NumUnits)
	require.Equal(t, atx.Sequence, response.Atx.Sequence)
	require.Nil(t, response.MalfeasanceProof)
}

func TestGet_IdentityCanceled(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	smesher, proof := grpcserver.BallotMalfeasance(t, sql.InMemory())
	id := types.RandomATXID()
	atx := types.VerifiedActivationTx{
		ActivationTx: &types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					Sequence:       rand.Uint64(),
					PrevATXID:      types.RandomATXID(),
					PublishEpoch:   0,
					PositioningATX: types.RandomATXID(),
				},
				Coinbase: types.GenerateAddress(types.RandomBytes(32)),
				NumUnits: rand.Uint32(),
			},
			SmesherID: smesher,
		},
	}
	atx.SetID(id)
	atxProvider.EXPECT().GetFullAtx(id).Return(&atx, nil)
	atxProvider.EXPECT().GetMalfeasanceProof(smesher).Return(proof, nil)

	response, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.NoError(t, err)

	require.Equal(t, atx.ID().Bytes(), response.Atx.Id.Id)
	require.Equal(t, atx.PublishEpoch.Uint32(), response.Atx.Layer.Number)
	require.Equal(t, atx.SmesherID.Bytes(), response.Atx.SmesherId.Id)
	require.Equal(t, atx.Coinbase.String(), response.Atx.Coinbase.Address)
	require.Equal(t, atx.PrevATXID.Bytes(), response.Atx.PrevAtx.Id)
	require.Equal(t, atx.NumUnits, response.Atx.NumUnits)
	require.Equal(t, atx.Sequence, response.Atx.Sequence)
	require.Equal(t, events.ToMalfeasancePB(smesher, proof, false), response.MalfeasanceProof)
}
