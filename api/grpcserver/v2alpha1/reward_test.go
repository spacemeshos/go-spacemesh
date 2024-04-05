package v2alpha1

import (
	"context"
	"errors"
	"io"
	"testing"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
)

func TestRewardService_List(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	gen := fixture.NewRewardsGenerator().WithAddresses(100).WithUniqueCoinbase()
	rwds := make([]types.Reward, 100)
	for i := range rwds {
		rwd := gen.Next()
		require.NoError(t, rewards.Add(db, rwd))
		rwds[i] = *rwd
	}

	svc := NewRewardService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewRewardServiceClient(conn)

	t.Run("limit set too high", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.RewardRequest{Limit: 200})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit is capped at 100", s.Message())
	})

	t.Run("no limit set", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.RewardRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit must be set to <= 100", s.Message())
	})

	t.Run("limit and offset", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.RewardRequest{
			Limit:  25,
			Offset: 50,
		})
		require.NoError(t, err)
		require.Len(t, list.Rewards, 25)
	})

	t.Run("all", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.RewardRequest{Limit: 100})
		require.NoError(t, err)
		require.Len(t, rwds, len(list.Rewards))
	})

	t.Run("coinbase", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.RewardRequest{
			Limit:      1,
			StartLayer: rwds[3].Layer.Uint32(),
			EndLayer:   rwds[3].Layer.Uint32(),
			FilterBy:   &spacemeshv2alpha1.RewardRequest_Coinbase{Coinbase: rwds[3].Coinbase.String()},
		})
		require.NoError(t, err)
		require.Equal(t, rwds[3].Layer.Uint32(), list.GetRewards()[0].GetV1().Layer)
		require.Equal(t, rwds[3].LayerReward, list.GetRewards()[0].GetV1().LayerReward)
		require.Equal(t, rwds[3].TotalReward, list.GetRewards()[0].GetV1().Total)
		require.Equal(t, rwds[3].Coinbase.String(), list.GetRewards()[0].GetV1().Coinbase)
	})

	t.Run("smesher", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.RewardRequest{
			Limit:      1,
			StartLayer: rwds[4].Layer.Uint32(),
			EndLayer:   rwds[4].Layer.Uint32(),
			FilterBy:   &spacemeshv2alpha1.RewardRequest_Smesher{Smesher: rwds[4].SmesherID.Bytes()},
		})
		require.NoError(t, err)
		require.Equal(t, rwds[4].Layer.Uint32(), list.GetRewards()[0].GetV1().Layer)
		require.Equal(t, rwds[4].LayerReward, list.GetRewards()[0].GetV1().LayerReward)
		require.Equal(t, rwds[4].TotalReward, list.GetRewards()[0].GetV1().Total)
		require.Equal(t, rwds[4].Coinbase.String(), list.GetRewards()[0].GetV1().Coinbase)
	})
}

func TestRewardStreamService_Stream(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	gen := fixture.NewRewardsGenerator()
	rwds := make([]types.Reward, 100)
	for i := range rwds {
		rwd := gen.Next()
		require.NoError(t, rewards.Add(db, rwd))
		rwds[i] = *rwd
	}

	svc := NewRewardStreamService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewRewardStreamServiceClient(conn)

	t.Run("all", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		stream, err := client.Stream(ctx, &spacemeshv2alpha1.RewardStreamRequest{})
		require.NoError(t, err)

		var i int
		for {
			_, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			i++
		}
		require.Len(t, rwds, i)
	})

	t.Run("watch", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		const (
			start = 100
			n     = 10
		)

		gen = fixture.NewRewardsGenerator().WithLayers(start, 10)
		var streamed []types.Reward
		for i := 0; i < n; i++ {
			rwd := gen.Next()
			streamed = append(streamed, *rwd)
		}

		for _, tc := range []struct {
			desc    string
			request *spacemeshv2alpha1.RewardStreamRequest
		}{
			{
				desc: "Smesher",
				request: &spacemeshv2alpha1.RewardStreamRequest{
					FilterBy: &spacemeshv2alpha1.RewardStreamRequest_Smesher{
						Smesher: streamed[3].SmesherID.Bytes(),
					},
					StartLayer: start,
					EndLayer:   start,
					Watch:      true,
				},
			},
			{
				desc: "Coinbase",
				request: &spacemeshv2alpha1.RewardStreamRequest{
					FilterBy: &spacemeshv2alpha1.RewardStreamRequest_Coinbase{
						Coinbase: streamed[3].Coinbase.String(),
					},
					StartLayer: start,
					EndLayer:   start,
					Watch:      true,
				},
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {
				stream, err := client.Stream(ctx, tc.request)
				require.NoError(t, err)
				_, err = stream.Header()
				require.NoError(t, err)

				var expect []*types.Reward
				for _, rst := range streamed {
					events.ReportRewardReceived(rst)
					matcher := rewardsMatcher{tc.request, ctx}
					if matcher.match(&rst) {
						expect = append(expect, &rst)
					}
				}

				for _, rst := range expect {
					received, err := stream.Recv()
					require.NoError(t, err)
					require.Equal(t, toReward(rst).String(), received.GetV1().String())
				}
			})
		}
	})
}
