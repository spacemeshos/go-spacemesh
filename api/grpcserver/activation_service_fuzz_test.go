package grpcserver

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	fuzz "github.com/google/gofuzz"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func fuzzValidAtxId(request *pb.GetRequest, c fuzz.Continue) {
	var id types.ATXID
	c.Fuzz(&id)
	request.Id = id.Bytes()
}

func Fuzz_ActivationService_InvalidAtxId(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzer := fuzz.NewFromGoFuzz(data)

		request := new(pb.GetRequest)
		fuzzer.Fuzz(request)

		ctrl := gomock.NewController(t)
		atxProvider := NewMockatxProvider(ctrl)
		activationService := NewActivationService(atxProvider)

		resp, err := activationService.Get(context.Background(), request)
		code, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, code.Code())
		require.Nil(t, resp)
	})
}

func Fuzz_ActivationService(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte, atxNotFound bool) {
		fuzzer := fuzz.NewFromGoFuzz(data).Funcs(fuzzValidAtxId)

		request := new(pb.GetRequest)
		fuzzer.Fuzz(request)
		id := types.ATXID(types.BytesToHash(request.Id))

		ctrl := gomock.NewController(t)
		atxProvider := NewMockatxProvider(ctrl)
		activationService := NewActivationService(atxProvider)

		if atxNotFound {
			atxProvider.EXPECT().GetFullAtx(id).Return(nil, errors.New("error"))
		} else {
			atx := &types.VerifiedActivationTx{
				ActivationTx: &types.ActivationTx{},
			}
			atx.SetID(id)
			atxProvider.EXPECT().GetFullAtx(id).Return(atx, nil)
		}

		resp, err := activationService.Get(context.Background(), request)

		if atxNotFound {
			code, ok := status.FromError(err)
			require.True(t, ok)
			require.Equal(t, codes.NotFound, code.Code())
			require.Nil(t, resp)
			return
		}

		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}
