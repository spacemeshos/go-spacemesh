package grpcserver_test

import (
	"context"
	"errors"
	"math/rand/v2"
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
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
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
	require.Nil(t, response.Atx.PrevAtx) // nolint:staticcheck // SA1019 (deprecated)
	require.EqualValues(t, 0, response.Atx.NumUnits)
	require.EqualValues(t, 0, response.Atx.Sequence)
}

func Test_Highest_ReturnsMaxTickHeight(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	goldenAtx := types.ATXID{2, 3, 4}
	activationService := grpcserver.NewActivationService(atxProvider, goldenAtx)

	previous := types.RandomATXID()
	atx := types.ActivationTx{
		Sequence:     rand.Uint64(),
		PublishEpoch: 0,
		Coinbase:     types.GenerateAddress(types.RandomBytes(32)),
		NumUnits:     rand.Uint32(),
	}
	id := types.RandomATXID()
	atx.SetID(id)
	atxProvider.EXPECT().MaxHeightAtx().Return(id, nil)
	atxProvider.EXPECT().GetAtx(id).Return(&atx, nil)
	atxProvider.EXPECT().Previous(id).Return([]types.ATXID{previous}, nil)

	response, err := activationService.Highest(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Equal(t, atx.ID().Bytes(), response.Atx.Id.Id)
	require.Equal(t, atx.PublishEpoch.Uint32(), response.Atx.Layer.Number)
	require.Equal(t, atx.SmesherID.Bytes(), response.Atx.SmesherId.Id)
	require.Equal(t, atx.Coinbase.String(), response.Atx.Coinbase.Address)
	require.Equal(t, previous.Bytes(), response.Atx.PrevAtx.Id) // nolint:staticcheck // SA1019 (deprecated)
	require.Equal(t, atx.NumUnits, response.Atx.NumUnits)
	require.Equal(t, atx.Sequence, response.Atx.Sequence)
}

func TestGet_RejectInvalidAtxID(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: []byte{1, 2, 3}})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGet_AtxNotPresent(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	id := types.RandomATXID()
	atxProvider.EXPECT().GetAtx(id).Return(nil, nil)

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestGet_AtxProviderReturnsFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	id := types.RandomATXID()
	atxProvider.EXPECT().GetAtx(id).Return(&types.ActivationTx{}, errors.New(""))

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestGet_AtxProviderFailsObtainPreviousAtxs(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	id := types.RandomATXID()
	atxProvider.EXPECT().GetAtx(id).Return(&types.ActivationTx{}, nil)
	atxProvider.EXPECT().Previous(id).Return(nil, errors.New(""))

	_, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGet_HappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	previous := []types.ATXID{types.RandomATXID(), types.RandomATXID()}
	id := types.RandomATXID()
	atx := types.ActivationTx{
		Sequence:     rand.Uint64(),
		PublishEpoch: 0,
		Coinbase:     types.GenerateAddress(types.RandomBytes(32)),
		NumUnits:     rand.Uint32(),
	}
	atx.SetID(id)
	atxProvider.EXPECT().GetAtx(id).Return(&atx, nil)
	atxProvider.EXPECT().MalfeasanceProof(gomock.Any()).Return(nil, sql.ErrNotFound)
	atxProvider.EXPECT().Previous(id).Return(previous, nil)

	response, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.NoError(t, err)

	require.Equal(t, atx.ID().Bytes(), response.Atx.Id.Id)
	require.Equal(t, atx.PublishEpoch.Uint32(), response.Atx.Layer.Number)
	require.Equal(t, atx.SmesherID.Bytes(), response.Atx.SmesherId.Id)
	require.Equal(t, atx.Coinbase.String(), response.Atx.Coinbase.Address)
	require.Equal(t, previous[0].Bytes(), response.Atx.PrevAtx.Id) // nolint:staticcheck // SA1019 (deprecated)
	require.Len(t, response.Atx.PreviousAtxs, 2)
	require.Equal(t, previous[0].Bytes(), response.Atx.PreviousAtxs[0].Id)
	require.Equal(t, previous[1].Bytes(), response.Atx.PreviousAtxs[1].Id)
	require.Equal(t, atx.NumUnits, response.Atx.NumUnits)
	require.Equal(t, atx.Sequence, response.Atx.Sequence)
	require.Nil(t, response.MalfeasanceProof)
}

func TestGet_IdentityCanceled(t *testing.T) {
	ctrl := gomock.NewController(t)
	atxProvider := grpcserver.NewMockatxProvider(ctrl)
	activationService := grpcserver.NewActivationService(atxProvider, types.ATXID{1})

	smesher, proof := grpcserver.BallotMalfeasance(t, statesql.InMemory())
	previous := types.RandomATXID()
	id := types.RandomATXID()
	atx := types.ActivationTx{
		Sequence:     rand.Uint64(),
		PublishEpoch: 0,
		Coinbase:     types.GenerateAddress(types.RandomBytes(32)),
		NumUnits:     rand.Uint32(),
		SmesherID:    smesher,
	}
	atx.SetID(id)
	atxProvider.EXPECT().GetAtx(id).Return(&atx, nil)
	atxProvider.EXPECT().MalfeasanceProof(smesher).Return(proof, nil)
	atxProvider.EXPECT().Previous(id).Return([]types.ATXID{previous}, nil)

	response, err := activationService.Get(context.Background(), &pb.GetRequest{Id: id.Bytes()})
	require.NoError(t, err)

	require.Equal(t, atx.ID().Bytes(), response.Atx.Id.Id)
	require.Equal(t, atx.PublishEpoch.Uint32(), response.Atx.Layer.Number)
	require.Equal(t, atx.SmesherID.Bytes(), response.Atx.SmesherId.Id)
	require.Equal(t, atx.Coinbase.String(), response.Atx.Coinbase.Address)
	require.Equal(t, previous.Bytes(), response.Atx.PrevAtx.Id) // nolint:staticcheck // SA1019 (deprecated)
	require.Len(t, response.Atx.PreviousAtxs, 1)
	require.Equal(t, previous.Bytes(), response.Atx.PreviousAtxs[0].Id)
	require.Equal(t, atx.NumUnits, response.Atx.NumUnits)
	require.Equal(t, atx.Sequence, response.Atx.Sequence)
	require.Equal(t, events.ToMalfeasancePB(smesher, proof, false), response.MalfeasanceProof)
}
