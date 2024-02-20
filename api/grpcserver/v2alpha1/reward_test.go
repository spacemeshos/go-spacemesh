package v2alpha1

import (
	"context"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestRewardService_List(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	gen := fixture.NewRewardsGenerator()
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
		require.Equal(t, s.Message(), "limit is capped at 100")
	})

	t.Run("no limit set", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.RewardRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, s.Message(), "limit must be set to <= 100")
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
