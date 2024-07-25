package grpcserver

import (
	"context"
	"math"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type globalStateServiceConn struct {
	pb.GlobalStateServiceClient

	meshAPI     *MockmeshAPI
	conStateAPI *MockconservativeState
}

func setupGlobalStateService(t *testing.T) (*globalStateServiceConn, context.Context) {
	ctrl, mockCtx := gomock.WithContext(context.Background(), t)
	meshAPI := NewMockmeshAPI(ctrl)
	conStateAPI := NewMockconservativeState(ctrl)
	svc := NewGlobalStateService(meshAPI, conStateAPI)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := pb.NewGlobalStateServiceClient(conn)

	return &globalStateServiceConn{
		GlobalStateServiceClient: client,

		meshAPI:     meshAPI,
		conStateAPI: conStateAPI,
	}, mockCtx
}

func TestGlobalStateService(t *testing.T) {
	t.Run("GlobalStateHash", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		layerVerified := types.LayerID(8)
		c.meshAPI.EXPECT().LatestLayerInState().Return(layerVerified)
		stateRoot := types.HexToHash32("11111")
		c.conStateAPI.EXPECT().GetStateRoot().Return(stateRoot, nil)

		res, err := c.GlobalStateHash(ctx, &pb.GlobalStateHashRequest{})
		require.NoError(t, err)
		require.Equal(t, layerVerified.Uint32(), res.Response.Layer.Number)
		require.Equal(t, stateRoot.Bytes(), res.Response.RootHash)
	})
	t.Run("Account", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		c.conStateAPI.EXPECT().GetBalance(addr1).Return(accountBalance, nil)
		c.conStateAPI.EXPECT().GetNonce(addr1).Return(accountCounter, nil)
		c.conStateAPI.EXPECT().GetProjection(addr1).Return(accountCounter+1, accountBalance+1)

		res, err := c.Account(ctx, &pb.AccountRequest{
			AccountId: &pb.AccountId{Address: addr1.String()},
		})
		require.NoError(t, err)
		require.Equal(t, addr1.String(), res.AccountWrapper.AccountId.Address)
		require.Equal(t, uint64(accountBalance), res.AccountWrapper.StateCurrent.Balance.Value)
		require.Equal(t, uint64(accountCounter), res.AccountWrapper.StateCurrent.Counter)
		require.Equal(t, uint64(accountBalance+1), res.AccountWrapper.StateProjected.Balance.Value)
		require.Equal(t, uint64(accountCounter+1), res.AccountWrapper.StateProjected.Counter)
	})
	t.Run("AccountDataQuery_MissingFilter", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		_, err := c.AccountDataQuery(ctx, &pb.AccountDataQueryRequest{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "`Filter` must be provided")
	})
	t.Run("AccountDataQuery_MissingFlags", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		_, err := c.AccountDataQuery(ctx, &pb.AccountDataQueryRequest{
			Filter: &pb.AccountDataFilter{
				AccountId: &pb.AccountId{Address: addr1.String()},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "`Filter.AccountMeshDataFlags` must set at least one")
	})
	t.Run("AccountDataQuery_BadOffset", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		c.meshAPI.EXPECT().GetRewardsByCoinbase(addr1).Return([]*types.Reward{
			{
				Layer:       layerFirst,
				TotalReward: rewardAmount,
				LayerReward: rewardAmount,
				Coinbase:    addr1,
			},
		}, nil)
		c.conStateAPI.EXPECT().GetBalance(addr1).Return(accountBalance, nil)
		c.conStateAPI.EXPECT().GetNonce(addr1).Return(accountCounter, nil)
		c.conStateAPI.EXPECT().GetProjection(addr1).Return(accountCounter+1, accountBalance+1)

		res, err := c.AccountDataQuery(ctx, &pb.AccountDataQueryRequest{
			MaxResults: uint32(1),
			Offset:     math.MaxUint32,
			Filter: &pb.AccountDataFilter{
				AccountId: &pb.AccountId{Address: addr1.String()},
				AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
			},
		})
		// huge offset is not an error, we just expect no results
		require.NoError(t, err)
		require.Equal(t, uint32(0), res.TotalResults)
		require.Empty(t, res.AccountItem)
	})
	t.Run("AccountDataQuery_ZeroMaxResults", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		c.meshAPI.EXPECT().GetRewardsByCoinbase(addr1).Return([]*types.Reward{
			{
				Layer:       layerFirst,
				TotalReward: rewardAmount,
				LayerReward: rewardAmount,
				Coinbase:    addr1,
			},
		}, nil)
		c.conStateAPI.EXPECT().GetBalance(addr1).Return(accountBalance, nil)
		c.conStateAPI.EXPECT().GetNonce(addr1).Return(accountCounter, nil)
		c.conStateAPI.EXPECT().GetProjection(addr1).Return(accountCounter+1, accountBalance+1)

		res, err := c.AccountDataQuery(ctx, &pb.AccountDataQueryRequest{
			MaxResults: uint32(0),
			Filter: &pb.AccountDataFilter{
				AccountId: &pb.AccountId{Address: addr1.String()},
				AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
			},
		})
		// zero maxresults means return everything
		require.NoError(t, err)
		require.Equal(t, uint32(2), res.TotalResults)
		require.Len(t, res.AccountItem, 2)
	})
	t.Run("AccountDataQuery_OneResult", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		c.meshAPI.EXPECT().GetRewardsByCoinbase(addr1).Return([]*types.Reward{
			{
				Layer:       layerFirst,
				TotalReward: rewardAmount,
				LayerReward: rewardAmount,
				Coinbase:    addr1,
				SmesherID:   rewardSmesherID,
			},
		}, nil)
		c.conStateAPI.EXPECT().GetBalance(addr1).Return(accountBalance, nil)
		c.conStateAPI.EXPECT().GetNonce(addr1).Return(accountCounter, nil)
		c.conStateAPI.EXPECT().GetProjection(addr1).Return(accountCounter+1, accountBalance+1)

		res, err := c.AccountDataQuery(ctx, &pb.AccountDataQueryRequest{
			MaxResults: uint32(1),
			Filter: &pb.AccountDataFilter{
				AccountId: &pb.AccountId{Address: addr1.String()},
				AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
			},
		})
		require.NoError(t, err)
		require.Equal(t, uint32(2), res.TotalResults)
		require.Len(t, res.AccountItem, 1)
		checkAccountDataQueryItemReward(t, res.AccountItem[0].Datum)
	})
	t.Run("AccountDataQuery", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		c.meshAPI.EXPECT().GetRewardsByCoinbase(addr1).Return([]*types.Reward{
			{
				Layer:       layerFirst,
				TotalReward: rewardAmount,
				LayerReward: rewardAmount,
				Coinbase:    addr1,
				SmesherID:   rewardSmesherID,
			},
		}, nil)
		c.conStateAPI.EXPECT().GetBalance(addr1).Return(accountBalance, nil)
		c.conStateAPI.EXPECT().GetNonce(addr1).Return(accountCounter, nil)
		c.conStateAPI.EXPECT().GetProjection(addr1).Return(accountCounter+1, accountBalance+1)

		res, err := c.AccountDataQuery(ctx, &pb.AccountDataQueryRequest{
			Filter: &pb.AccountDataFilter{
				AccountId: &pb.AccountId{Address: addr1.String()},
				AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
			},
		})
		require.NoError(t, err)
		require.Equal(t, uint32(2), res.TotalResults)
		require.Len(t, res.AccountItem, 2)

		checkAccountDataQueryItemReward(t, res.AccountItem[0].Datum)
		checkAccountDataQueryItemAccount(t, res.AccountItem[1].Datum)
	})
	t.Run("AppEventStream", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		stream, err := c.AppEventStream(ctx, &pb.AppEventStreamRequest{})
		// We expect to be able to open the stream but for it to fail upon the first request
		require.NoError(t, err)
		_, err = stream.Recv()
		statusCode := status.Code(err)
		require.Equal(t, codes.Unimplemented, statusCode)
	})
	t.Run("AccountDataStream_emptyAddress", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		stream, err := c.AccountDataStream(ctx, &pb.AccountDataStreamRequest{
			Filter: &pb.AccountDataFilter{
				AccountId: &pb.AccountId{},
				AccountDataFlags: uint32(
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
						pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT),
			},
		})
		require.NoError(t, err, "unexpected error opening stream")

		_, err = stream.Recv()
		statusCode := status.Code(err)
		require.Equal(t, codes.Unknown, statusCode)
	})
	t.Run("AccountDataStream_invalidAddress", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupGlobalStateService(t)

		// Just try opening and immediately closing the stream
		ctx, cancel := context.WithCancel(ctx)
		stream, err := c.AccountDataStream(ctx, &pb.AccountDataStreamRequest{
			Filter: &pb.AccountDataFilter{
				AccountId: &pb.AccountId{Address: types.GenerateAddress([]byte{'A'}).String()},
				AccountDataFlags: uint32(
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
						pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT),
			},
		})
		require.NoError(t, err, "unexpected error opening stream")

		cancel()
		_, err = stream.Recv()
		statusCode := status.Code(err)
		require.Equal(t, codes.Canceled, statusCode)
	})
	t.Run("AccountDataStream", func(t *testing.T) {
		t.Parallel()

		tt := []struct {
			name string
			req  *pb.AccountDataStreamRequest
			err  string
		}{
			{
				name: "missing filter",
				req:  &pb.AccountDataStreamRequest{},
				err:  "`Filter` must be provided",
			},
			{
				name: "empty filter",
				req: &pb.AccountDataStreamRequest{
					Filter: &pb.AccountDataFilter{},
				},
				err: "`Filter.AccountId` must be provided",
			},
			{
				name: "missing address",
				req: &pb.AccountDataStreamRequest{
					Filter: &pb.AccountDataFilter{
						AccountDataFlags: uint32(
							pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
								pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT),
					},
				},
				err: "`Filter.AccountId` must be provided",
			},
			{
				name: "filter with zero flags",
				req: &pb.AccountDataStreamRequest{
					Filter: &pb.AccountDataFilter{
						AccountId:        &pb.AccountId{Address: addr1.String()},
						AccountDataFlags: uint32(0),
					},
				},
				err: "`Filter.AccountDataFlags` must set at least one bitfield",
			},
		}

		for _, tc := range tt {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				c, ctx := setupGlobalStateService(t)

				// there should be no error opening the stream
				stream, err := c.AccountDataStream(ctx, tc.req)
				require.NoError(t, err, "unexpected error opening stream")

				// sending a request should generate an error
				_, err = stream.Recv()
				require.Error(t, err, "expected an error")
				require.ErrorContains(t, err, tc.err, "received unexpected error")
				statusCode := status.Code(err)
				require.Equal(t, codes.InvalidArgument, statusCode, "expected InvalidArgument error")
			})
		}
	})
	t.Run("GlobalStateStream", func(t *testing.T) {
		t.Parallel()

		t.Run("nonzero flags", func(t *testing.T) {
			t.Parallel()
			c, ctx := setupGlobalStateService(t)

			// Just try opening and immediately closing the stream
			ctx, cancel := context.WithCancel(ctx)
			stream, err := c.GlobalStateStream(ctx, &pb.GlobalStateStreamRequest{
				GlobalStateDataFlags: uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_ACCOUNT),
			})
			require.NoError(t, err, "unexpected error opening stream")

			cancel()
			_, err = stream.Recv()
			statusCode := status.Code(err)
			require.Equal(t, codes.Canceled, statusCode)
		})
		t.Run("zero flags", func(t *testing.T) {
			t.Parallel()
			c, ctx := setupGlobalStateService(t)

			// there should be no error opening the stream
			stream, err := c.GlobalStateStream(ctx, &pb.GlobalStateStreamRequest{GlobalStateDataFlags: uint32(0)})
			require.NoError(t, err, "unexpected error opening stream")

			// sending a request should generate an error
			_, err = stream.Recv()
			require.Error(t, err, "expected an error")
			require.ErrorContains(
				t,
				err,
				"`GlobalStateDataFlags` must set at least one bitfield",
				"received unexpected error",
			)
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode, "expected InvalidArgument error")
		})
	})
}
